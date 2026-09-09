package template

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestClashInjectorInjectsProxyDNSPolicyUnderDNS(t *testing.T) {
	content := []byte(`dns:
  enable: true
  nameserver-policy:
    "+.existing.example":
      - 192.0.2.2
  proxy-server-nameserver-policy:
    "+.old.example":
      - 192.0.2.1
rules:
  - MATCH,DIRECT
`)
	policyContent := []byte(`common-domains:
  - github.com
proxy-server-nameserver-policy:
  "+.node.example,+.edge.example":
    - https://private.example/dns-query#DIRECT&skip-cert-verify=true
`)

	loadCalls := 0
	got, err := DefaultRegistry().Inject(Context{
		File: "clash_meta.yaml",
		LoadProxyDNSPolicy: func() ([]byte, error) {
			loadCalls++
			return policyContent, nil
		},
	}, content)
	if err != nil {
		t.Fatal(err)
	}
	if loadCalls != 1 {
		t.Fatalf("policy loader calls = %d, want 1", loadCalls)
	}

	var document struct {
		CommonDomains []string `yaml:"common-domains"`
		DNS           struct {
			Enable           bool                `yaml:"enable"`
			NameserverPolicy map[string][]string `yaml:"nameserver-policy"`
			ProxyPolicy      map[string][]string `yaml:"proxy-server-nameserver-policy"`
		} `yaml:"dns"`
	}
	if err := yaml.Unmarshal(got, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.CommonDomains) != 0 {
		t.Fatalf("common-domains metadata was injected into Clash output: %#v", document.CommonDomains)
	}
	if !document.DNS.Enable {
		t.Fatal("existing dns configuration was not preserved")
	}
	want := map[string][]string{
		"+.node.example": {"https://private.example/dns-query#DIRECT&skip-cert-verify=true"},
		"+.edge.example": {"https://private.example/dns-query#DIRECT&skip-cert-verify=true"},
	}
	wantExisting := map[string][]string{
		"+.existing.example": {"192.0.2.2"},
	}
	if !reflect.DeepEqual(document.DNS.NameserverPolicy, wantExisting) {
		t.Fatalf("existing nameserver policy = %#v, want %#v", document.DNS.NameserverPolicy, wantExisting)
	}
	if !reflect.DeepEqual(document.DNS.ProxyPolicy, want) {
		t.Fatalf("injected proxy-server nameserver policy = %#v, want %#v", document.DNS.ProxyPolicy, want)
	}
	if _, exists := document.DNS.ProxyPolicy["+.old.example"]; exists {
		t.Fatal("old template proxy-server policy was not replaced")
	}
}

func TestClashInjectorSplitsCombinedProxyDNSPolicyKey(t *testing.T) {
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
	policyContent := []byte("proxy-server-nameserver-policy:\n  # merged provider\n  \"" + longRule + "\":\n    - 178.94.14.101#DIRECT\n")

	got, err := DefaultRegistry().Inject(Context{
		File: "clash_meta.yaml",
		LoadProxyDNSPolicy: func() ([]byte, error) {
			return policyContent, nil
		},
	}, []byte("dns:\n  enable: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	output := string(got)
	if strings.Contains(output, "\n    ? ") || strings.Contains(output, "\n    : ") {
		t.Fatalf("Clash output uses explicit YAML key syntax:\n%s", got)
	}
	if strings.Contains(output, longRule) {
		t.Fatalf("Clash output still contains combined policy key:\n%s", got)
	}
	if strings.Contains(output, "\n  nameserver-policy:") {
		t.Fatalf("Clash output unexpectedly contains nameserver-policy:\n%s", got)
	}
	if !strings.Contains(output, "  proxy-server-nameserver-policy:") {
		t.Fatalf("Clash output is missing proxy-server-nameserver-policy:\n%s", got)
	}
	if strings.Count(output, "# merged provider") != 1 {
		t.Fatalf("provider comment count is not 1:\n%s", got)
	}
	for _, domain := range strings.Split(longRule, ",") {
		key := "    \"" + domain + "\":"
		if strings.Count(output, key) != 1 {
			t.Fatalf("split policy occurrence count for %q is not 1:\n%s", domain, got)
		}
	}
}
