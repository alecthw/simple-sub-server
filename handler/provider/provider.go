package provider

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

const cfgUserAgent = "Mozilla/5.0 (dart:io) SuperAccelerator"

var errSubscriptionUnavailable = errors.New("subscription unavailable")

// Config represents a provider YAML configuration file.
type Config struct {
	CfgUrls      []string          `yaml:"cfgUrls"`
	Username     string            `yaml:"username"`
	Password     string            `yaml:"password"`
	Headers      map[string]string `yaml:"headers"`
	Decrypt      *DecryptConfig    `yaml:"decrypt"`
	SubscribeUrl string            `yaml:"subscribeUrl"`
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

type cfgResponse struct {
	HostSource string   `json:"host_source"`
	Hosts      []string `json:"hosts"`
}

type contentResult struct {
	body                 []byte
	subscriptionUserinfo string
}

// Handler handles GET /provider/:provider.
func Handler(providerDir string, client *resty.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		handle(c, providerDir, client)
	}
}

func handle(c *gin.Context, providerDir string, client *resty.Client) {
	providerName := c.Param("provider")

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

	if config.SubscribeUrl != "" {
		result, err := fetchSubscriptionContent(client, config.SubscribeUrl, contentHeaders, config.Decrypt)
		if err == nil {
			writeSubscription(c, result)
			return
		}
		zap.S().Infow("cached subscribe url unavailable, refreshing", "provider", providerName, "error", err)
	}

	baseURLs, err := fetchBaseURLs(client, config.CfgUrls)
	if err != nil {
		zap.S().Errorw("failed to fetch provider base urls", "provider", providerName, "error", err)
		c.String(404, "Not found")
		return
	}

	result, subscribeURL, err := refreshSubscription(client, config, baseURLs, authHeaders, contentHeaders)
	if err != nil {
		zap.S().Errorw("failed to refresh subscription", "provider", providerName, "error", err)
		c.String(403, "Forbidden")
		return
	}

	config.SubscribeUrl = subscribeURL
	if err := saveConfig(providerDir, providerName, config); err != nil {
		zap.S().Errorw("failed to save provider config", "provider", providerName, "error", err)
	}

	writeSubscription(c, result)
}

func fetchBaseURLs(client *resty.Client, cfgURLs []string) ([]string, error) {
	if len(cfgURLs) == 0 {
		return nil, fmt.Errorf("cfgUrls is empty")
	}

	type cfgResult struct {
		index int
		hosts []string
		err   error
	}

	results := make([]cfgResult, len(cfgURLs))
	ch := make(chan cfgResult, len(cfgURLs))
	var wg sync.WaitGroup
	for i, cfgURL := range cfgURLs {
		wg.Add(1)
		go func(index int, url string) {
			defer wg.Done()
			hosts, err := fetchConfigHosts(client, url)
			ch <- cfgResult{index: index, hosts: hosts, err: err}
		}(i, cfgURL)
	}
	wg.Wait()
	close(ch)

	for result := range ch {
		results[result.index] = result
	}

	seen := make(map[string]struct{})
	baseURLs := make([]string, 0)
	var lastErr error
	for _, result := range results {
		if result.err != nil {
			lastErr = result.err
			continue
		}
		for _, host := range result.hosts {
			for _, baseURL := range baseURLCandidates(host) {
				if _, ok := seen[baseURL]; ok {
					continue
				}
				seen[baseURL] = struct{}{}
				baseURLs = append(baseURLs, baseURL)
			}
		}
	}
	if len(baseURLs) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("no hosts found in cfgUrls")
	}
	return baseURLs, nil
}

func fetchConfigHosts(client *resty.Client, cfgURL string) ([]string, error) {
	resp, err := client.R().
		SetHeader("User-Agent", cfgUserAgent).
		Get(cfgURL)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != 200 || len(resp.Body()) == 0 {
		return nil, fmt.Errorf("cfg url returned status %d", resp.StatusCode())
	}

	decoded, err := decodeBase64String(strings.TrimSpace(string(resp.Body())))
	if err != nil {
		return nil, err
	}

	var cfg cfgResponse
	if err := json.Unmarshal(decoded, &cfg); err != nil {
		return nil, err
	}
	hosts := append([]string{}, cfg.Hosts...)
	if cfg.HostSource != "" {
		hosts = append(hosts, cfg.HostSource)
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("cfg hosts is empty")
	}
	return hosts, nil
}

func refreshSubscription(client *resty.Client, config *Config, baseURLs []string, authHeaders map[string]string, contentHeaders map[string]string) (*contentResult, string, error) {
	var lastErr error
	for _, baseURL := range baseURLs {
		authData, err := login(client, baseURL, config, authHeaders)
		if err != nil {
			lastErr = err
			continue
		}

		subscribe, err := getSubscribe(client, baseURL, authData, authHeaders)
		if err != nil {
			lastErr = err
			continue
		}

		if subscribe.Data.SubscribeUrl != "" {
			result, err := fetchSubscriptionContent(client, subscribe.Data.SubscribeUrl, contentHeaders, config.Decrypt)
			if err == nil {
				return result, subscribe.Data.SubscribeUrl, nil
			}
			lastErr = err
		}

		if subscribe.Data.Token == "" {
			continue
		}
		for _, fallbackBaseURL := range baseURLs {
			fallbackURL := fallbackSubscribeURL(fallbackBaseURL, subscribe.Data.Token)
			result, err := fetchSubscriptionContent(client, fallbackURL, contentHeaders, config.Decrypt)
			if err == nil {
				return result, fallbackURL, nil
			}
			lastErr = err
		}
	}

	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", errSubscriptionUnavailable
}

func login(client *resty.Client, baseURL string, config *Config, headers map[string]string) (string, error) {
	resp, err := client.R().
		SetHeaders(headers).
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]string{
			"email":    config.Username,
			"password": config.Password,
		}).
		Post(baseURL + "/passport/auth/login")
	if err != nil {
		return "", err
	}
	if resp.StatusCode() != 200 || len(resp.Body()) == 0 {
		return "", fmt.Errorf("login returned status %d", resp.StatusCode())
	}

	var lr loginResponse
	if err := json.Unmarshal(resp.Body(), &lr); err != nil {
		return "", err
	}
	if lr.Data.AuthData == "" {
		return "", fmt.Errorf("login auth_data is empty")
	}
	return lr.Data.AuthData, nil
}

func getSubscribe(client *resty.Client, baseURL string, authData string, headers map[string]string) (*subscribeResponse, error) {
	resp, err := client.R().
		SetHeaders(headers).
		SetHeader("Authorization", authData).
		Get(baseURL + "/user/getSubscribe")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != 200 || len(resp.Body()) == 0 {
		return nil, fmt.Errorf("getSubscribe returned status %d", resp.StatusCode())
	}

	var sr subscribeResponse
	if err := json.Unmarshal(resp.Body(), &sr); err != nil {
		return nil, err
	}
	if sr.Data.SubscribeUrl == "" && sr.Data.Token == "" {
		return nil, fmt.Errorf("getSubscribe returned no subscribe_url or token")
	}
	return &sr, nil
}

func fetchSubscriptionContent(client *resty.Client, url string, headers map[string]string, decrypt *DecryptConfig) (*contentResult, error) {
	if strings.TrimSpace(url) == "" {
		return nil, errSubscriptionUnavailable
	}

	resp, err := client.R().
		SetHeaders(headers).
		Get(url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != 200 || len(resp.Body()) == 0 {
		return nil, errSubscriptionUnavailable
	}

	body, err := decodeSubscriptionBody(resp.Body(), decrypt)
	if err != nil {
		return nil, err
	}

	return &contentResult{
		body:                 body,
		subscriptionUserinfo: resp.Header().Get("subscription-userinfo"),
	}, nil
}

func writeSubscription(c *gin.Context, result *contentResult) {
	if result.subscriptionUserinfo != "" {
		c.Header("subscription-userinfo", result.subscriptionUserinfo)
	}
	c.Data(200, "text/plain; charset=UTF-8", result.body)
}

func fallbackSubscribeURL(baseURL string, token string) string {
	return fmt.Sprintf("%s/client/subscribe?token=%s", normalizeBaseURL(baseURL), token)
}

func baseURLCandidates(baseURL string) []string {
	normalized := normalizeBaseURL(baseURL)
	if normalized == "" {
		return nil
	}
	if strings.HasSuffix(normalized, "/api/v1") {
		return []string{normalized}
	}
	if strings.HasSuffix(normalized, "/api") {
		return []string{normalized, normalized + "/v1"}
	}
	return []string{normalized + "/api/v1"}
}

func normalizeBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

func decodeBase64String(value string) ([]byte, error) {
	value = strings.TrimPrefix(value, "\xef\xbb\xbf")
	value = strings.TrimPrefix(value, "\ufeff")
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.URLEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return base64.RawURLEncoding.DecodeString(value)
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
