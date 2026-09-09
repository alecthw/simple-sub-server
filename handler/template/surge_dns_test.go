package template

import (
	"errors"
	"strings"
	"testing"
)

func TestConvertProxyDNSPolicyToSurgeHost(t *testing.T) {
	content := []byte(`common-domains:
  - github.com
proxy-server-nameserver-policy:
  # merged providers
  "+.alpha.example,+.beta.test,jp-test.github.com":
    - 192.0.2.53#DIRECT
    - 198.51.100.53#DIRECT
  # doh provider
  "+.private.example":
    - https://dns-one.example/dns-query#DIRECT&skip-cert-verify=true
    - https://dns-two.example/dns-query#DIRECT&skip-cert-verify=true
  "+.*":
    - 203.0.113.53#DIRECT
`)

	got, err := convertProxyDNSPolicyToSurgeHost(content)
	if err != nil {
		t.Fatal(err)
	}
	want := `# merged providers
*.alpha.example = server:192.0.2.53,198.51.100.53
*.beta.test = server:192.0.2.53,198.51.100.53
jp-test.github.com = server:192.0.2.53,198.51.100.53

# doh provider
*.private.example = server:https://dns-one.example/dns-query,https://dns-two.example/dns-query`
	if got != want {
		t.Fatalf("converted Surge Host block:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, "#DIRECT") || strings.Contains(got, "skip-cert-verify") {
		t.Fatalf("Mihomo DNS options remained in Surge output:\n%s", got)
	}
	if strings.Contains(got, "*.*") {
		t.Fatalf("catch-all rule was converted into Surge output:\n%s", got)
	}
}

func TestSurgeInjectorInjectsProxyDNSPolicyIntoHost(t *testing.T) {
	templateContent := []byte(`[General]
loglevel = notify

[Proxy Group]
Proxy = select, DIRECT

[Host]
localhost = 127.0.0.1

[Rule]
FINAL,Proxy
`)
	policyContent := []byte(`common-domains:
  - github.com
proxy-server-nameserver-policy:
  # provider
  "+.airport.example,node.github.com":
    - 192.0.2.53#DIRECT
`)

	loadCalls := 0
	got, err := (SurgeInjector{}).Inject(Context{
		File: "surge_app.conf",
		LoadProxyDNSPolicy: func() ([]byte, error) {
			loadCalls++
			return policyContent, nil
		},
	}, templateContent)
	if err != nil {
		t.Fatal(err)
	}
	if loadCalls != 1 {
		t.Fatalf("policy loader calls = %d, want 1", loadCalls)
	}

	hostSection, _, _, ok := findSection(string(got), "[Host]")
	if !ok {
		t.Fatal("generated Surge config is missing [Host]")
	}
	want := `localhost = 127.0.0.1

# provider
*.airport.example = server:192.0.2.53
node.github.com = server:192.0.2.53`
	if !strings.Contains(hostSection, want) {
		t.Fatalf("generated [Host] section does not contain converted policy:\n%s", hostSection)
	}
	if strings.Index(string(got), "*.airport.example") > strings.Index(string(got), "[Rule]") {
		t.Fatal("DNS policy was inserted outside [Host]")
	}
}

func TestSurgeInjectorSkipsProxyDNSWithoutHost(t *testing.T) {
	loadCalls := 0
	content := []byte("[Proxy Group]\nProxy = select, DIRECT\n\n[Rule]\nFINAL,Proxy\n")
	got, err := (SurgeInjector{}).Inject(Context{
		File: "surge.conf",
		LoadProxyDNSPolicy: func() ([]byte, error) {
			loadCalls++
			return nil, nil
		},
	}, content)
	if err != nil {
		t.Fatal(err)
	}
	if loadCalls != 0 {
		t.Fatalf("policy loader calls = %d, want 0", loadCalls)
	}
	if string(got) != string(content) {
		t.Fatalf("Surge config without [Host] changed:\n%s", got)
	}
}

func TestSurfboardInjectorInjectsProxyDNSPolicy(t *testing.T) {
	loadCalls := 0
	content := []byte("[Proxy Group]\nProxy = select, DIRECT\n\n[Host]\nlocalhost = 127.0.0.1\n")
	got, err := (SurgeInjector{}).Inject(Context{
		File: "surfboard.conf",
		LoadProxyDNSPolicy: func() ([]byte, error) {
			loadCalls++
			return []byte("proxy-server-nameserver-policy:\n  \"+.airport.example\":\n    - 192.0.2.53#DIRECT\n"), nil
		},
	}, content)
	if err != nil {
		t.Fatal(err)
	}
	if loadCalls != 1 {
		t.Fatalf("policy loader calls = %d, want 1", loadCalls)
	}
	if !strings.Contains(string(got), "*.airport.example = server:192.0.2.53") {
		t.Fatalf("generated Surfboard config is missing proxy DNS policy:\n%s", got)
	}
}

func TestSurgeInjectorReturnsProxyDNSLoadError(t *testing.T) {
	_, err := (SurgeInjector{}).Inject(Context{
		File: "surge.conf",
		LoadProxyDNSPolicy: func() ([]byte, error) {
			return nil, errors.New("load failed")
		},
	}, []byte("[Host]\n"))
	if err == nil || !strings.Contains(err.Error(), "load proxy dns policy") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoonInjectorInjectsProxyDNSWithoutRemoteProxySection(t *testing.T) {
	loadCalls := 0
	content := []byte("[Host]\nlocalhost = 127.0.0.1\n\n[Rule]\nFINAL,DIRECT\n")
	got, err := (LoonInjector{}).Inject(Context{
		File: "loon.conf",
		LoadProxyDNSPolicy: func() ([]byte, error) {
			loadCalls++
			return []byte("proxy-server-nameserver-policy:\n  \"+.airport.example\":\n    - 192.0.2.53#DIRECT\n"), nil
		},
	}, content)
	if err != nil {
		t.Fatal(err)
	}
	if loadCalls != 1 {
		t.Fatalf("policy loader calls = %d, want 1", loadCalls)
	}
	if !strings.Contains(string(got), "*.airport.example = server:192.0.2.53") {
		t.Fatalf("generated Loon config is missing proxy DNS policy:\n%s", got)
	}
}
