package template

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type egernForwardValue struct {
	Match string `yaml:"match"`
	Value string `yaml:"value"`
}

type egernForwardEntry struct {
	Domain         *egernForwardValue `yaml:"domain"`
	DomainSuffix   *egernForwardValue `yaml:"domain_suffix"`
	DomainWildcard *egernForwardValue `yaml:"domain_wildcard"`
}

func TestEgernInjectorInjectsUpstreamsAndPrependsForwardRules(t *testing.T) {
	content := []byte(`dns:
  bootstrap:
    - system
  upstreams:
    china:
      - https://dns.example/dns-query
  forward:
    - domain_wildcard:
        match: "*"
        value: china
policy_groups: []
`)
	policyContent := []byte(`common-domains:
  - github.com
proxy-server-nameserver-policy:
  # alpha / beta
  "+.alpha.example,jp-test.github.com":
    - 192.0.2.53#DIRECT
    - 198.51.100.53#DIRECT
  # huanyu
  "+.huanyu.example":
    - https://dns-one.example/dns-query#DIRECT&skip-cert-verify=true
    - https://dns-two.example/dns-query#DIRECT&skip-cert-verify=true
  # xmtz
  "+.xmtz.example":
    - https://dns-three.example/dns-query#DIRECT&skip-cert-verify=true
`)

	loadCalls := 0
	got, err := Inject(Context{
		File: "egern_app.yaml",
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
			Bootstrap []string            `yaml:"bootstrap"`
			Upstreams map[string][]string `yaml:"upstreams"`
			Forward   []egernForwardEntry `yaml:"forward"`
		} `yaml:"dns"`
	}
	if err := yaml.Unmarshal(got, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.CommonDomains) != 0 {
		t.Fatalf("common-domains metadata was injected: %#v", document.CommonDomains)
	}
	if !reflect.DeepEqual(document.DNS.Bootstrap, []string{"system"}) {
		t.Fatalf("existing DNS configuration was not preserved: %#v", document.DNS.Bootstrap)
	}
	wantUpstreams := map[string][]string{
		"china":       {"https://dns.example/dns-query"},
		"proxy-dns-1": {"192.0.2.53", "198.51.100.53"},
		"huanyu":      {"https://dns-one.example/dns-query", "https://dns-two.example/dns-query"},
		"xmtz":        {"https://dns-three.example/dns-query"},
	}
	if !reflect.DeepEqual(document.DNS.Upstreams, wantUpstreams) {
		t.Fatalf("Egern upstreams = %#v, want %#v", document.DNS.Upstreams, wantUpstreams)
	}
	if len(document.DNS.Forward) != 5 {
		t.Fatalf("forward rule count = %d, want 5", len(document.DNS.Forward))
	}
	assertEgernForwardValue(t, document.DNS.Forward[0].DomainSuffix, "alpha.example", "proxy-dns-1")
	assertEgernForwardValue(t, document.DNS.Forward[1].Domain, "jp-test.github.com", "proxy-dns-1")
	assertEgernForwardValue(t, document.DNS.Forward[2].DomainSuffix, "huanyu.example", "huanyu")
	assertEgernForwardValue(t, document.DNS.Forward[3].DomainSuffix, "xmtz.example", "xmtz")
	assertEgernForwardValue(t, document.DNS.Forward[4].DomainWildcard, "*", "china")

	output := string(got)
	for _, unwanted := range []string{"#DIRECT", "skip-cert-verify", "+.alpha.example"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("Egern output contains %q:\n%s", unwanted, output)
		}
	}
	for _, comment := range []string{"alpha / beta", "huanyu", "xmtz"} {
		if !strings.Contains(output, "    # "+comment+"\n    - ") {
			t.Fatalf("provider comment %q is missing or not directly before its first rule:\n%s", comment, output)
		}
		if strings.Contains(output, "# "+comment+"\n\n") {
			t.Fatalf("provider comment %q is followed by a blank line:\n%s", comment, output)
		}
	}
}

func TestNextEgernUpstreamNameAvoidsExistingNames(t *testing.T) {
	used := map[string]struct{}{
		"proxy-dns-1": {},
		"huanyu":      {},
	}
	genericIndex := 1
	if got := nextEgernUpstreamName("alpha / beta", used, &genericIndex); got != "proxy-dns-2" {
		t.Fatalf("generic upstream name = %q, want proxy-dns-2", got)
	}
	if got := nextEgernUpstreamName("huanyu", used, &genericIndex); got != "huanyu-2" {
		t.Fatalf("provider upstream name = %q, want huanyu-2", got)
	}
}

func assertEgernForwardValue(t *testing.T, got *egernForwardValue, match string, value string) {
	t.Helper()
	if got == nil || got.Match != match || got.Value != value {
		t.Fatalf("forward value = %#v, want match=%q value=%q", got, match, value)
	}
}
