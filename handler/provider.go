package handler

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
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// ProviderConfig represents a provider YAML configuration file
type ProviderConfig struct {
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

// ProviderHandler handles GET /provider/:provider
func ProviderHandler(c *gin.Context) {
	provider := c.Param("provider")
	ua := c.Query("ua")

	// 1. Load provider config
	config, err := loadProviderConfig(provider)
	if err != nil {
		zap.S().Errorw("failed to load provider config", "provider", provider, "error", err)
		c.String(404, "Not found")
		return
	}

	// 2. Build auth headers (Step 1 & 2: only User-Agent from config if present)
	authHeaders := make(map[string]string)
	if configUA, ok := config.Headers["User-Agent"]; ok {
		authHeaders["User-Agent"] = configUA
	}

	// 3. Build content headers (Step 3: all config headers + ua merged into User-Agent)
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

	// 4. Try Step 3 with cached token first
	if config.SubscribeUrl != "" && config.Token != "" {
		contentUrl := buildContentUrl(config)
		contentResp, err := client.R().
			SetHeaders(contentHeaders).
			Get(contentUrl)
		if err == nil && contentResp.StatusCode() == 200 && len(contentResp.Body()) > 0 {
			body, err := decodeSubscriptionBody(contentResp.Body(), config.Decrypt)
			if err != nil {
				zap.S().Errorw("decode content failed", "provider", provider, "error", err)
				c.String(502, "Bad Gateway")
				return
			}
			if subInfo := contentResp.Header().Get("subscription-userinfo"); subInfo != "" {
				c.Header("subscription-userinfo", subInfo)
			}
			c.Data(200, "text/plain; charset=UTF-8", body)
			return
		}
		zap.S().Infow("cached token expired, re-login", "provider", provider)
	}

	// 5. Step 1: Login
	loginResp, err := client.R().
		SetHeaders(authHeaders).
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]string{
			"email":    config.Username,
			"password": config.Password,
		}).
		Post(config.BaseUrl + "/passport/auth/login")
	if err != nil {
		zap.S().Errorw("login request failed", "provider", provider, "error", err)
		c.String(403, "Forbidden")
		return
	}

	var lr loginResponse
	if err := json.Unmarshal(loginResp.Body(), &lr); err != nil || lr.Data.AuthData == "" {
		zap.S().Errorw("failed to parse login response", "provider", provider, "error", err)
		c.String(404, "Not found")
		return
	}

	// 6. Step 2: Get Subscribe
	subResp, err := client.R().
		SetHeaders(authHeaders).
		SetHeader("Authorization", lr.Data.AuthData).
		Get(config.BaseUrl + "/user/getSubscribe")
	if err != nil {
		zap.S().Errorw("getSubscribe request failed", "provider", provider, "error", err)
		c.String(403, "Forbidden")
		return
	}

	var sr subscribeResponse
	if err := json.Unmarshal(subResp.Body(), &sr); err != nil {
		zap.S().Errorw("failed to parse subscribe response", "provider", provider, "error", err)
		c.String(404, "Not found")
		return
	}

	// 7. Update cached token and persist
	config.Token = sr.Data.Token
	if !config.ForceSubscribeUrl && sr.Data.SubscribeUrl != "" {
		config.SubscribeUrl = sr.Data.SubscribeUrl
	}
	if err := saveProviderConfig(provider, config); err != nil {
		zap.S().Errorw("failed to save provider config", "provider", provider, "error", err)
	}

	// 8. Step 3: Get subscription content
	contentUrl := buildContentUrl(config)
	contentResp, err := client.R().
		SetHeaders(contentHeaders).
		Get(contentUrl)
	if err != nil {
		zap.S().Errorw("get content request failed", "provider", provider, "error", err)
		c.String(403, "Forbidden")
		return
	}

	body, err := decodeSubscriptionBody(contentResp.Body(), config.Decrypt)
	if err != nil {
		zap.S().Errorw("decode content failed", "provider", provider, "error", err)
		c.String(502, "Bad Gateway")
		return
	}

	if subInfo := contentResp.Header().Get("subscription-userinfo"); subInfo != "" {
		c.Header("subscription-userinfo", subInfo)
	}
	c.Data(200, "text/plain; charset=UTF-8", body)
}

// buildContentUrl constructs the Step 3 URL based on forceSubscribeUrl mode
func buildContentUrl(config *ProviderConfig) string {
	if config.ForceSubscribeUrl {
		// forceSubscribeUrl: always use subscribeUrl as base, append token
		return fmt.Sprintf("%s?token=%s", config.SubscribeUrl, config.Token)
	}
	// non-force: subscribeUrl may already contain token (cached from API response)
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

// loadProviderConfig reads and parses a provider YAML config file
func loadProviderConfig(provider string) (*ProviderConfig, error) {
	configPath := filepath.Join(providerDir, provider+".yml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config ProviderConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// saveProviderConfig writes the provider config back to its YAML file
func saveProviderConfig(provider string, config *ProviderConfig) error {
	configPath := filepath.Join(providerDir, provider+".yml")
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}
