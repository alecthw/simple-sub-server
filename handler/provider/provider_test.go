package provider

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"gopkg.in/yaml.v3"
)

func TestHandleUsesCachedSubscribeURLAndIgnoresUAQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var cfgHits atomic.Int32
	var contentUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cfg":
			cfgHits.Add(1)
			t.Errorf("cached subscribeUrl should not fetch cfgUrls")
			http.Error(w, "unexpected cfg fetch", http.StatusInternalServerError)
			return
		case "/cached":
			contentUserAgent = r.UserAgent()
			w.Header().Set("subscription-userinfo", "upload=1; download=2")
			_, _ = w.Write([]byte("cached-content"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	providerDir := t.TempDir()
	writeProviderConfig(t, providerDir, "demo", &Config{
		CfgUrls: []string{server.URL + "/cfg"},
		Headers: map[string]string{
			"User-Agent": "ConfiguredUA",
		},
		SubscribeUrl: server.URL + "/cached",
	})

	resp := performProviderRequest(providerDir, "/provider/demo?ua=InjectedUA", resty.New())
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", resp.Code, resp.Body.String())
	}
	if got := resp.Body.String(); got != "cached-content" {
		t.Fatalf("body = %q", got)
	}
	if contentUserAgent != "ConfiguredUA" {
		t.Fatalf("content User-Agent = %q", contentUserAgent)
	}
	if cfgHits.Load() != 0 {
		t.Fatalf("cfg hits = %d", cfgHits.Load())
	}
	if got := resp.Header().Get("subscription-userinfo"); got != "upload=1; download=2" {
		t.Fatalf("subscription-userinfo = %q", got)
	}
}

func TestHandleRefreshesSubscribeURLFromCfgUrlsAndFallsBackWithToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var serverURL string
	var cfgUserAgentSeen string
	var loginUserAgentSeen string
	var contentUserAgentSeen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cfg":
			cfgUserAgentSeen = r.UserAgent()
			jsonBody := fmt.Sprintf(`{"hosts":["%s/bad","%s/"]}`, serverURL, serverURL)
			_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(jsonBody))))
		case "/cached-expired":
			http.Error(w, "expired", http.StatusGone)
		case "/bad/passport/auth/login":
			http.Error(w, "bad base", http.StatusBadGateway)
		case "/passport/auth/login":
			http.Error(w, "missing api prefix", http.StatusNotFound)
		case "/api/v1/passport/auth/login":
			loginUserAgentSeen = r.UserAgent()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"auth_data":"Bearer auth"}}`))
		case "/api/v1/user/getSubscribe":
			if got := r.Header.Get("Authorization"); got != "Bearer auth" {
				t.Errorf("Authorization = %q", got)
				http.Error(w, "bad authorization", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fmt.Sprintf(`{"data":{"token":"new-token","subscribe_url":"%s/dead-sub"}}`, serverURL)))
		case "/dead-sub", "/bad/client/subscribe":
			http.NotFound(w, r)
		case "/api/v1/client/subscribe":
			if got := r.URL.Query().Get("token"); got != "new-token" {
				t.Errorf("fallback token = %q", got)
				http.Error(w, "bad token", http.StatusUnauthorized)
				return
			}
			contentUserAgentSeen = r.UserAgent()
			_, _ = w.Write([]byte("fallback-content"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	providerDir := t.TempDir()
	writeProviderConfig(t, providerDir, "demo", &Config{
		CfgUrls:  []string{server.URL + "/cfg"},
		Username: "user@example.com",
		Password: "secret",
		Headers: map[string]string{
			"User-Agent": "ProviderUA",
		},
		SubscribeUrl: server.URL + "/cached-expired",
	})

	resp := performProviderRequest(providerDir, "/provider/demo", resty.New())
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", resp.Code, resp.Body.String())
	}
	if got := resp.Body.String(); got != "fallback-content" {
		t.Fatalf("body = %q", got)
	}
	if cfgUserAgentSeen != cfgUserAgent {
		t.Fatalf("cfg User-Agent = %q", cfgUserAgentSeen)
	}
	if loginUserAgentSeen != "ProviderUA" {
		t.Fatalf("login User-Agent = %q", loginUserAgentSeen)
	}
	if contentUserAgentSeen != "ProviderUA" {
		t.Fatalf("content User-Agent = %q", contentUserAgentSeen)
	}

	saved := readProviderConfigMap(t, filepath.Join(providerDir, "demo.yml"))
	wantSubscribeURL := server.URL + "/api/v1/client/subscribe?token=new-token"
	if saved["subscribeUrl"] != wantSubscribeURL {
		t.Fatalf("saved subscribeUrl = %v, want %q", saved["subscribeUrl"], wantSubscribeURL)
	}
	for _, removedKey := range []string{"baseUrl", "forceSubscribeUrl", "token"} {
		if _, ok := saved[removedKey]; ok {
			t.Fatalf("saved config still has removed key %q", removedKey)
		}
	}
}

func performProviderRequest(providerDir string, target string, client *resty.Client) *httptest.ResponseRecorder {
	router := gin.New()
	router.GET("/provider/:provider", Handler(providerDir, client))

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	router.ServeHTTP(resp, req)
	return resp
}

func writeProviderConfig(t *testing.T, providerDir string, provider string, config *Config) {
	t.Helper()
	data, err := yaml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, provider+".yml"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func readProviderConfigMap(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) == "" {
		t.Fatal("saved config is empty")
	}
	var config map[string]any
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	return config
}

func TestBaseURLCandidatesNormalizeAPIBase(t *testing.T) {
	tests := []struct {
		name string
		host string
		want []string
	}{
		{
			name: "root host",
			host: "https://example.com/",
			want: []string{"https://example.com/api/v1"},
		},
		{
			name: "api host",
			host: "https://example.com/api",
			want: []string{"https://example.com/api", "https://example.com/api/v1"},
		},
		{
			name: "api v1 host",
			host: "https://example.com/api/v1",
			want: []string{"https://example.com/api/v1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := baseURLCandidates(tt.host)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("baseURLCandidates(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}
