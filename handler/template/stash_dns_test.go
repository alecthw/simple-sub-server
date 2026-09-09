package template

import (
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestStashInjectorInjectsSupportedDNSFields(t *testing.T) {
	content := []byte(`dns:
  enable: true
  nameserver-policy:
    "+.old.example":
      - 192.0.2.1
  proxy-server-nameserver:
    - 203.0.113.53
  proxy-server-nameserver-policy:
    "+.unsupported.example":
      - 203.0.113.54
rules:
  - MATCH,DIRECT
`)
	policyContent := []byte(`common-domains:
  - github.com
proxy-server-nameserver-policy:
  "+.node.example,+.edge.example":
    - https://private.example/dns-query#DIRECT&skip-cert-verify=true
    - 192.0.2.53#DIRECT
  "+.udp.example":
    - 192.0.2.53#DIRECT
    - 198.51.100.53#DIRECT
`)

	loadCalls := 0
	got, err := Inject(Context{
		File: "stash_app.yaml",
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
			Enable            bool                `yaml:"enable"`
			NameserverPolicy  map[string][]string `yaml:"nameserver-policy"`
			ProxyNameservers  []string            `yaml:"proxy-server-nameserver"`
			UnsupportedPolicy map[string][]string `yaml:"proxy-server-nameserver-policy"`
		} `yaml:"dns"`
	}
	if err := yaml.Unmarshal(got, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.CommonDomains) != 0 {
		t.Fatalf("common-domains metadata was injected into Stash output: %#v", document.CommonDomains)
	}
	if !document.DNS.Enable {
		t.Fatal("existing dns configuration was not preserved")
	}
	want := map[string][]string{
		"+.node.example": {"https://private.example/dns-query", "192.0.2.53"},
		"+.edge.example": {"https://private.example/dns-query", "192.0.2.53"},
		"+.udp.example":  {"192.0.2.53", "198.51.100.53"},
	}
	if !reflect.DeepEqual(document.DNS.NameserverPolicy, want) {
		t.Fatalf("injected nameserver policy = %#v, want %#v", document.DNS.NameserverPolicy, want)
	}
	wantNameservers := []string{"203.0.113.53", "https://private.example/dns-query", "192.0.2.53", "198.51.100.53"}
	if !reflect.DeepEqual(document.DNS.ProxyNameservers, wantNameservers) {
		t.Fatalf("proxy nameservers = %#v, want %#v", document.DNS.ProxyNameservers, wantNameservers)
	}
	if len(document.DNS.UnsupportedPolicy) != 0 {
		t.Fatalf("unsupported proxy-server-nameserver-policy remained: %#v", document.DNS.UnsupportedPolicy)
	}
}
