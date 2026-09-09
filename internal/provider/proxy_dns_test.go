package provider

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"gopkg.in/yaml.v3"
)

func TestExtractProviderDNSPolicy(t *testing.T) {
	result := &contentResult{
		body: []byte(`dns:
  proxy-server-nameserver:
    - https://private.example/dns-query
    - 178.94.14.101
    - https://dns.alidns.com/dns-query
    - 223.5.5.5
proxy-server-nameserver:
  - https://private.example/dns-query
proxies:
  - name: domain one
    server: edge-a.nianyun.pw
  - name: domain two
    server: cdn.flydocking.xyz
  - name: duplicate suffix
    server: edge-b.nianyun.pw
  - name: IP server
    server: 192.0.2.1
`),
		profileTitle: "base64:" + base64.StdEncoding.EncodeToString([]byte("寰宇")),
	}

	policy, ok := extractProviderDNSPolicy("huanyu", result, defaultProxyDNSCommonDomains)
	if !ok {
		t.Fatal("expected a private DNS policy")
	}
	if policy.name != "寰宇" {
		t.Fatalf("name = %q", policy.name)
	}
	if policy.domainRule != "+.flydocking.xyz,+.nianyun.pw" {
		t.Fatalf("domain rule = %q", policy.domainRule)
	}
	wantProbeDomains := []string{"cdn.flydocking.xyz", "edge-a.nianyun.pw", "edge-b.nianyun.pw"}
	if !reflect.DeepEqual(policy.probeDomains, wantProbeDomains) {
		t.Fatalf("probe domains = %#v, want %#v", policy.probeDomains, wantProbeDomains)
	}
	wantNameservers := []string{
		"https://private.example/dns-query#DIRECT&skip-cert-verify=true",
		"178.94.14.101#DIRECT",
		"https://dns.alidns.com/dns-query#DIRECT&skip-cert-verify=true",
		"223.5.5.5#DIRECT",
	}
	if !reflect.DeepEqual(policy.nameservers, wantNameservers) {
		t.Fatalf("nameservers = %#v, want %#v", policy.nameservers, wantNameservers)
	}
}

func TestExtractProviderDNSPolicySkipsPublicDNSOnly(t *testing.T) {
	result := &contentResult{body: []byte(`dns:
  proxy-server-nameserver:
    - https://dns.google/dns-query
    - tls://1.1.1.1
proxies:
  - server: node.example.com
`)}

	if _, ok := extractProviderDNSPolicy("public", result, defaultProxyDNSCommonDomains); ok {
		t.Fatal("public DNS must not produce a provider policy")
	}
}

func TestMergeProviderDNSPoliciesWhenAnyNameserverOverlaps(t *testing.T) {
	policies := []providerDNSPolicy{
		{
			name:        "first",
			domainRule:  "+.iz2ze58f9krop9tgbc.org,+.only-first.example",
			nameservers: []string{"14.137.229.81#DIRECT", "1.0.0.1#DIRECT"},
		},
		{
			name:        "second",
			domainRule:  "+.iz2ze58f9krop9tgbc.org,+.only-second.example",
			nameservers: []string{"178.94.14.101#DIRECT", "14.137.229.81#DIRECT"},
		},
	}

	got := mergeProviderDNSPolicies(policies)
	if len(got) != 1 {
		t.Fatalf("merged policy count = %d, want 1: %#v", len(got), got)
	}
	if got[0].domainRule != "+.iz2ze58f9krop9tgbc.org,+.only-first.example,+.only-second.example" {
		t.Fatalf("merged domains = %q", got[0].domainRule)
	}
	wantNameservers := []string{
		"178.94.14.101#DIRECT",
		"14.137.229.81#DIRECT",
		"1.0.0.1#DIRECT",
	}
	if !reflect.DeepEqual(got[0].nameservers, wantNameservers) {
		t.Fatalf("merged nameservers = %#v, want %#v", got[0].nameservers, wantNameservers)
	}
	if got[0].name != "first / second" {
		t.Fatalf("merged policy name = %q", got[0].name)
	}
}

func TestMergeProviderDNSPoliciesRegroupsDomainsWithSameDNS(t *testing.T) {
	policies := []providerDNSPolicy{
		{
			name:        "edgenova",
			domainRule:  "+.a.example,+.b.example",
			nameservers: []string{"192.0.2.1#DIRECT"},
		},
		{
			name:        "kuaili",
			domainRule:  "+.b.example,+.a.example",
			nameservers: []string{"192.0.2.2#DIRECT"},
		},
	}

	got := mergeProviderDNSPolicies(policies)
	if len(got) != 1 {
		t.Fatalf("merged policy count = %d, want 1: %#v", len(got), got)
	}
	if got[0].domainRule != "+.a.example,+.b.example" {
		t.Fatalf("regrouped domains = %q", got[0].domainRule)
	}
	wantNameservers := []string{
		"192.0.2.2#DIRECT",
		"192.0.2.1#DIRECT",
	}
	if !reflect.DeepEqual(got[0].nameservers, wantNameservers) {
		t.Fatalf("regrouped nameservers = %#v, want %#v", got[0].nameservers, wantNameservers)
	}
}

func TestMarshalProxyDNSPolicyKeepsLongMergedKeyInline(t *testing.T) {
	longRule := strings.Join([]string{
		"+.bilibili-data.com",
		"+.iz2ze58f9krop9tgbc.org",
		"+.kklsedaf.cc",
		"+.github.com",
		"+.hdkla.com",
		"+.fweifeng.com",
		"+.feixiang988x.com",
		"+.drect90889.org",
	}, ",")

	got, err := marshalProxyDNSPolicy([]providerDNSPolicy{{
		name:        "merged providers",
		domainRule:  longRule,
		nameservers: []string{"178.94.14.101#DIRECT", "14.137.229.81#DIRECT"},
	}}, defaultProxyDNSCommonDomains)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "\n  ? ") || strings.Contains(string(got), "\n  : ") {
		t.Fatalf("generated policy uses explicit YAML key syntax:\n%s", got)
	}
	want := "  \"" + longRule + "\":\n    - 178.94.14.101#DIRECT"
	if !strings.Contains(string(got), want) {
		t.Fatalf("long merged key is malformed:\n%s", got)
	}
}

func TestLoadOrGenerateProxyDNSPolicyUsesExistingFile(t *testing.T) {
	providerDir := t.TempDir()
	want := []byte(`common-domains:
  - github.com
proxy-server-nameserver-policy:
  "+.saved.example":
    - 192.0.2.53#DIRECT
`)
	if err := os.WriteFile(filepath.Join(providerDir, proxyDNSFileName), want, 0644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadOrGenerateProxyDNSPolicy(providerDir, resty.New())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded policy = %q, want %q", got, want)
	}
}

func TestLoadOrGenerateProxyDNSPolicyGeneratesMissingOrEmptyFile(t *testing.T) {
	tests := []struct {
		name       string
		createFile bool
	}{
		{name: "missing"},
		{name: "empty", createFile: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stubDNSAvailability(t, true)

			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				_, _ = w.Write([]byte(`dns:
  proxy-server-nameserver:
    - https://private.example/dns-query
proxies:
  - server: node.airport.example
`))
			}))
			defer server.Close()

			providerDir := t.TempDir()
			writeProviderConfig(t, providerDir, "airport", &Config{SubscribeUrl: server.URL})
			if test.createFile {
				if err := os.WriteFile(filepath.Join(providerDir, proxyDNSFileName), []byte(" \n\t"), 0644); err != nil {
					t.Fatal(err)
				}
			}

			got, err := LoadOrGenerateProxyDNSPolicy(providerDir, resty.New())
			if err != nil {
				t.Fatal(err)
			}
			if requests.Load() != 1 {
				t.Fatalf("provider requests = %d, want 1", requests.Load())
			}

			var generated struct {
				CommonDomains []string            `yaml:"common-domains"`
				Policy        map[string][]string `yaml:"proxy-server-nameserver-policy"`
			}
			if err := yaml.Unmarshal(got, &generated); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(generated.CommonDomains, defaultProxyDNSCommonDomains) {
				t.Fatalf("common domains = %#v, want %#v", generated.CommonDomains, defaultProxyDNSCommonDomains)
			}
			if _, exists := generated.Policy["+.airport.example"]; !exists {
				t.Fatalf("generated policy is missing provider domain: %#v", generated.Policy)
			}
			if _, exists := generated.Policy["+.*"]; exists {
				t.Fatal("generated policy contains the removed catch-all rule")
			}

			written, err := os.ReadFile(filepath.Join(providerDir, proxyDNSFileName))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(written, got) {
				t.Fatal("generated policy was not written to proxy-dns.yml")
			}
		})
	}
}

func TestHandleProxyDNSRefreshesFileAndReturnsIt(t *testing.T) {
	stubDNSAvailability(t, true)

	var userAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/private":
			userAgent = r.UserAgent()
			w.Header().Set("profile-title", "base64:"+base64.StdEncoding.EncodeToString([]byte("寰宇")))
			_, _ = w.Write([]byte(`dns:
  proxy-server-nameserver:
    - https://private.example/dns-query
    - 178.94.14.101
proxies:
  - server: edge.nianyun.pw
  - server: cdn.flydocking.xyz
  - server: api.github.com
  - server: gateway.cloudflare.com
`))
		case "/public":
			_, _ = w.Write([]byte(`dns:
  proxy-server-nameserver:
    - https://dns.alidns.com/dns-query
proxies:
  - server: node.public.example
`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	providerDir := t.TempDir()
	writeProviderConfig(t, providerDir, "huanyu", &Config{SubscribeUrl: server.URL + "/private"})
	writeProviderConfig(t, providerDir, "public", &Config{SubscribeUrl: server.URL + "/public"})
	if err := os.WriteFile(filepath.Join(providerDir, proxyDNSFileName), []byte(`common-domains:
  - github.com
  - cloudflare.com
proxy-server-nameserver-policy:
  "+.*": []
`), 0644); err != nil {
		t.Fatal(err)
	}

	response := performProviderRequest(providerDir, "/provider/proxy-dns", resty.New())
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
	if userAgent != defaultUserAgent {
		t.Fatalf("User-Agent = %q, want %q", userAgent, defaultUserAgent)
	}
	if got := response.Header().Get("Content-Type"); got != "text/plain; charset=UTF-8" {
		t.Fatalf("Content-Type = %q", got)
	}

	written, err := os.ReadFile(filepath.Join(providerDir, proxyDNSFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(written, response.Body.Bytes()) {
		t.Fatal("returned policy differs from proxy-dns.yml")
	}
	if !strings.Contains(string(written), "# 寰宇") {
		t.Fatalf("generated YAML does not contain decoded provider title:\n%s", written)
	}

	var generated struct {
		CommonDomains []string            `yaml:"common-domains"`
		Policy        map[string][]string `yaml:"proxy-server-nameserver-policy"`
	}
	if err := yaml.Unmarshal(written, &generated); err != nil {
		t.Fatal(err)
	}
	wantCommonDomains := []string{"github.com", "cloudflare.com"}
	if !reflect.DeepEqual(generated.CommonDomains, wantCommonDomains) {
		t.Fatalf("common domains = %#v, want %#v", generated.CommonDomains, wantCommonDomains)
	}
	wantPrivate := []string{
		"https://private.example/dns-query#DIRECT&skip-cert-verify=true",
		"178.94.14.101#DIRECT",
	}
	if got := generated.Policy["+.flydocking.xyz,+.nianyun.pw,api.github.com,gateway.cloudflare.com"]; !reflect.DeepEqual(got, wantPrivate) {
		t.Fatalf("private policy = %#v, want %#v", got, wantPrivate)
	}
	if _, exists := generated.Policy["+.*"]; exists {
		t.Fatal("generated policy contains the removed catch-all rule")
	}
	if len(generated.Policy) != 1 {
		t.Fatalf("policy count = %d, want 1: %#v", len(generated.Policy), generated.Policy)
	}
}

func TestWithDirectDNSOptions(t *testing.T) {
	tests := []struct {
		name       string
		nameserver string
		want       string
	}{
		{
			name:       "doh",
			nameserver: "https://private.example/dns-query",
			want:       "https://private.example/dns-query#DIRECT&skip-cert-verify=true",
		},
		{
			name:       "udp",
			nameserver: "178.94.14.101",
			want:       "178.94.14.101#DIRECT",
		},
		{
			name:       "preserve extra options",
			nameserver: "https://private.example/dns-query#proxy&h3=true&skip-cert-verify=false",
			want:       "https://private.example/dns-query#DIRECT&proxy&h3=true&skip-cert-verify=true",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := withDirectDNSOptions(test.nameserver); got != test.want {
				t.Fatalf("withDirectDNSOptions(%q) = %q, want %q", test.nameserver, got, test.want)
			}
		})
	}
}

func stubDNSAvailability(t *testing.T, available bool) {
	t.Helper()
	previous := dnsAvailabilityProbe
	dnsAvailabilityProbe = func(string, []string) bool { return available }
	t.Cleanup(func() {
		dnsAvailabilityProbe = previous
	})
}

func TestNextProxyDNSRefresh(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "before three",
			now:  time.Date(2026, time.September, 8, 2, 59, 59, 0, location),
			want: time.Date(2026, time.September, 8, 3, 0, 0, 0, location),
		},
		{
			name: "at three",
			now:  time.Date(2026, time.September, 8, 3, 0, 0, 0, location),
			want: time.Date(2026, time.September, 9, 3, 0, 0, 0, location),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := nextProxyDNSRefresh(test.now); !got.Equal(test.want) {
				t.Fatalf("next refresh = %s, want %s", got, test.want)
			}
		})
	}
}
