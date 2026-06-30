package provider

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// Config represents a provider YAML configuration file.
type Config struct {
	BaseUrl           string            `yaml:"baseUrl"`
	SubscribeUrl      string            `yaml:"subscribeUrl"`
	ForceSubscribeUrl bool              `yaml:"forceSubscribeUrl"`
	Token             string            `yaml:"token"`
	Username          string            `yaml:"username"`
	Password          string            `yaml:"password"`
	Headers           map[string]string `yaml:"headers"`
	Decrypt           *DecryptConfig    `yaml:"decrypt"`
}

// DecryptConfig enables the xjkp subscription payload decryption.
type DecryptConfig struct {
	Key string `yaml:"key"`
	IV  string `yaml:"iv"`
}

type loginResponse struct {
	Data struct {
		AuthData string `json:"auth_data"`
	} `json:"data"`
}

type subscribeResponse struct {
	Data struct {
		Token        string `json:"token"`
		SubscribeUrl string `json:"subscribe_url"`
	} `json:"data"`
}

// Handler handles GET /provider/:provider.
func Handler(providerDir string, client *resty.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		handle(c, providerDir, client)
	}
}

func handle(c *gin.Context, providerDir string, client *resty.Client) {
	providerName := c.Param("provider")
	ua := c.Query("ua")

	config, err := loadConfig(providerDir, providerName)
	if err != nil {
		zap.S().Errorw("failed to load provider config", "provider", providerName, "error", err)
		c.String(404, "Not found")
		return
	}

	authHeaders := make(map[string]string)
	if configUA, ok := config.Headers["User-Agent"]; ok {
		authHeaders["User-Agent"] = configUA
	}

	contentHeaders := make(map[string]string)
	for k, v := range config.Headers {
		contentHeaders[k] = v
	}
	if ua != "" {
		if existing, ok := contentHeaders["User-Agent"]; ok {
			contentHeaders["User-Agent"] = ua + " " + existing
		} else {
			contentHeaders["User-Agent"] = ua
		}
	}

	if config.SubscribeUrl != "" && config.Token != "" {
		contentUrl := buildContentURL(config)
		contentResp, err := client.R().
			SetHeaders(contentHeaders).
			Get(contentUrl)
		if err == nil && contentResp.StatusCode() == 200 && len(contentResp.Body()) > 0 {
			body, err := decodeSubscriptionBody(contentResp.Body(), config.Decrypt)
			if err != nil {
				zap.S().Errorw("decode content failed", "provider", providerName, "error", err)
				c.String(502, "Bad Gateway")
				return
			}
			if subInfo := contentResp.Header().Get("subscription-userinfo"); subInfo != "" {
				c.Header("subscription-userinfo", subInfo)
			}
			c.Data(200, "text/plain; charset=UTF-8", body)
			return
		}
		zap.S().Infow("cached token expired, re-login", "provider", providerName)
	}

	loginResp, err := client.R().
		SetHeaders(authHeaders).
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]string{
			"email":    config.Username,
			"password": config.Password,
		}).
		Post(config.BaseUrl + "/passport/auth/login")
	if err != nil {
		zap.S().Errorw("login request failed", "provider", providerName, "error", err)
		c.String(403, "Forbidden")
		return
	}

	var lr loginResponse
	if err := json.Unmarshal(loginResp.Body(), &lr); err != nil || lr.Data.AuthData == "" {
		zap.S().Errorw("failed to parse login response", "provider", providerName, "error", err)
		c.String(404, "Not found")
		return
	}

	subResp, err := client.R().
		SetHeaders(authHeaders).
		SetHeader("Authorization", lr.Data.AuthData).
		Get(config.BaseUrl + "/user/getSubscribe")
	if err != nil {
		zap.S().Errorw("getSubscribe request failed", "provider", providerName, "error", err)
		c.String(403, "Forbidden")
		return
	}

	var sr subscribeResponse
	if err := json.Unmarshal(subResp.Body(), &sr); err != nil {
		zap.S().Errorw("failed to parse subscribe response", "provider", providerName, "error", err)
		c.String(404, "Not found")
		return
	}

	config.Token = sr.Data.Token
	if !config.ForceSubscribeUrl && sr.Data.SubscribeUrl != "" {
		config.SubscribeUrl = sr.Data.SubscribeUrl
	}
	if err := saveConfig(providerDir, providerName, config); err != nil {
		zap.S().Errorw("failed to save provider config", "provider", providerName, "error", err)
	}

	contentUrl := buildContentURL(config)
	contentResp, err := client.R().
		SetHeaders(contentHeaders).
		Get(contentUrl)
	if err != nil {
		zap.S().Errorw("get content request failed", "provider", providerName, "error", err)
		c.String(403, "Forbidden")
		return
	}

	body, err := decodeSubscriptionBody(contentResp.Body(), config.Decrypt)
	if err != nil {
		zap.S().Errorw("decode content failed", "provider", providerName, "error", err)
		c.String(502, "Bad Gateway")
		return
	}

	if subInfo := contentResp.Header().Get("subscription-userinfo"); subInfo != "" {
		c.Header("subscription-userinfo", subInfo)
	}
	c.Data(200, "text/plain; charset=UTF-8", body)
}

func buildContentURL(config *Config) string {
	if config.ForceSubscribeUrl {
		return fmt.Sprintf("%s?token=%s", config.SubscribeUrl, config.Token)
	}
	if strings.Contains(config.SubscribeUrl, "token=") {
		return config.SubscribeUrl
	}
	return fmt.Sprintf("%s?token=%s", config.SubscribeUrl, config.Token)
}

func decodeSubscriptionBody(body []byte, decrypt *DecryptConfig) ([]byte, error) {
	if decrypt == nil {
		return body, nil
	}

	body, err := gunzipIfNeeded(body)
	if err != nil {
		return nil, err
	}

	cipherText, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(body)))
	if err != nil {
		return nil, err
	}
	if len(cipherText) == 0 || len(cipherText)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("invalid ciphertext length")
	}

	key := []byte(decrypt.Key)
	iv := []byte(decrypt.IV)
	if len(key) != aes.BlockSize || len(iv) != aes.BlockSize {
		return nil, fmt.Errorf("invalid aes key or iv length")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	plainText := make([]byte, len(cipherText))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plainText, cipherText)

	plainText, err = pkcs7Unpad(plainText)
	if err != nil {
		return nil, err
	}

	return base64.StdEncoding.DecodeString(strings.TrimSpace(string(plainText)))
}

func gunzipIfNeeded(body []byte) ([]byte, error) {
	if len(body) < 2 || body[0] != 0x1f || body[1] != 0x8b {
		return body, nil
	}

	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()
	return io.ReadAll(reader)
}

func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("invalid pkcs7 data length")
	}

	padding := int(data[len(data)-1])
	if padding == 0 || padding > aes.BlockSize || padding > len(data) {
		return nil, fmt.Errorf("invalid pkcs7 padding")
	}
	for i := len(data) - padding; i < len(data); i++ {
		if int(data[i]) != padding {
			return nil, fmt.Errorf("invalid pkcs7 padding")
		}
	}
	return data[:len(data)-padding], nil
}

func loadConfig(providerDir string, provider string) (*Config, error) {
	configPath := filepath.Join(providerDir, provider+".yml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func saveConfig(providerDir string, provider string, config *Config) error {
	configPath := filepath.Join(providerDir, provider+".yml")
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}
