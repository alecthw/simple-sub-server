package provider

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/go-resty/resty/v2"
	"gopkg.in/yaml.v3"
)

func TestProxyServerDomainRulePreservesCommonDomains(t *testing.T) {
	commonDomains := normalizeCommonDomains([]string{
		"github.com",
		"*.Cloudflare.com.",
		"github.com",
		"invalid",
	})
	if want := []string{"github.com", "cloudflare.com"}; !reflect.DeepEqual(commonDomains, want) {
		t.Fatalf("normalized common domains = %#v, want %#v", commonDomains, want)
	}

	tests := []struct {
		name   string
		server string
		want   string
	}{
		{name: "common root", server: "github.com", want: "github.com"},
		{name: "common subdomain", server: "api.github.com", want: "api.github.com"},
		{name: "manually added common domain", server: "cdn.cloudflare.com", want: "cdn.cloudflare.com"},
		{name: "domain boundary", server: "github.com.evil.example", want: "+.evil.example"},
		{name: "ordinary provider domain", server: "node.airport.example", want: "+.airport.example"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := proxyServerDomainRule(test.server, commonDomains); got != test.want {
				t.Fatalf("proxyServerDomainRule(%q) = %q, want %q", test.server, got, test.want)
			}
		})
	}
}

func TestParseProxyDNSCommonDomainsUsesDefaultWhenMissing(t *testing.T) {
	got, configured, err := parseProxyDNSCommonDomains([]byte(`proxy-server-nameserver-policy:
  "+.*": []
`))
	if err != nil {
		t.Fatal(err)
	}
	if configured {
		t.Fatal("missing common-domains was reported as configured")
	}
	if !reflect.DeepEqual(got, defaultProxyDNSCommonDomains) {
		t.Fatalf("common domains = %#v, want %#v", got, defaultProxyDNSCommonDomains)
	}
}

func TestLoadOrGenerateProxyDNSPolicyMigratesMissingCommonDomains(t *testing.T) {
	providerDir := t.TempDir()
	path := filepath.Join(providerDir, proxyDNSFileName)
	if err := os.WriteFile(path, []byte(`proxy-server-nameserver-policy:
  "+.*":
    - 192.0.2.1#DIRECT
`), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadOrGenerateProxyDNSPolicy(providerDir, resty.New())
	if err != nil {
		t.Fatal(err)
	}

	var generated proxyDNSFileConfig
	if err := yaml.Unmarshal(got, &generated); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(generated.CommonDomains, defaultProxyDNSCommonDomains) {
		t.Fatalf("common domains = %#v, want %#v", generated.CommonDomains, defaultProxyDNSCommonDomains)
	}
	if _, exists := generated.Policy["+.*"]; exists {
		t.Fatal("legacy catch-all rule was not removed during migration")
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(written, got) {
		t.Fatal("migrated proxy-dns.yml differs from returned content")
	}
}
