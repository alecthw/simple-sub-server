package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/go-resty/resty/v2"
	"go.uber.org/zap"
)

const defaultUserAgent = "clash-verge"

var errSubscriptionUnavailable = errors.New("subscription unavailable")

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

type contentResult struct {
	body                 []byte
	subscriptionUserinfo string
	profileTitle         string
}

func fetchProviderResult(providerDir string, providerName string, client *resty.Client) (*contentResult, int, error) {
	config, err := loadConfig(providerDir, providerName)
	if err != nil {
		return nil, 404, err
	}

	authHeaders := make(map[string]string)
	if configUA, ok := config.Headers["User-Agent"]; ok {
		authHeaders["User-Agent"] = configUA
	}

	contentHeaders := map[string]string{
		"User-Agent": defaultUserAgent,
	}
	for k, v := range config.Headers {
		contentHeaders[k] = v
	}

	if config.SubscribeUrl != "" {
		result, err := fetchSubscriptionContent(client, config.SubscribeUrl, contentHeaders, config.Decrypt)
		if err == nil {
			return result, 200, nil
		}
		zap.S().Infow("cached subscribe url unavailable, refreshing", "provider", providerName)
	}

	baseURLs, err := fetchBaseURLs(client, config.CfgUrls)
	if err != nil {
		return nil, 404, err
	}

	result, subscribeURL, err := refreshSubscription(client, config, baseURLs, authHeaders, contentHeaders)
	if err != nil {
		return nil, 403, err
	}

	config.SubscribeUrl = subscribeURL
	if err := saveConfig(providerDir, providerName, config); err != nil {
		zap.S().Errorw("failed to save provider config", "provider", providerName, "error", err)
	}

	return result, 200, nil
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
		profileTitle:         resp.Header().Get("profile-title"),
	}, nil
}

func fallbackSubscribeURL(baseURL string, token string) string {
	return fmt.Sprintf("%s/client/subscribe?token=%s", normalizeBaseURL(baseURL), token)
}
