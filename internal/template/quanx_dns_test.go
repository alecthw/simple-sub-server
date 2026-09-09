package template

import (
	"strings"
	"testing"
)

func TestConvertProxyDNSPolicyToQuanxDNS(t *testing.T) {
	content := []byte(`proxy-server-nameserver-policy:
  # udp provider
  "+.example2.com,+.edge.example":
    - 192.0.2.53#DIRECT
    - 198.51.100.53#DIRECT
  # doh provider
  "+.example3.com,api.github.com":
    - https://doh.example/dns-query#DIRECT&skip-cert-verify=true
    - https://backup.example/dns-query#DIRECT&skip-cert-verify=true
  # doq provider
  "+.example4.com":
    - quic://dns.example#DIRECT
    - quic://backup.example#DIRECT
  "+.*":
    - 203.0.113.53#DIRECT
`)

	got, err := convertProxyDNSPolicyToQuanxDNS(content)
	if err != nil {
		t.Fatal(err)
	}
	want := `# udp provider
server = /*.example2.com/192.0.2.53
server = /*.edge.example/192.0.2.53

# doh provider
doh-server = /*.example3.com/https://doh.example/dns-query
doh-server = /api.github.com/https://doh.example/dns-query

# doq provider
doq-server = /*.example4.com/quic://dns.example`
	if got != want {
		t.Fatalf("converted QuanX DNS block:\n%s\nwant:\n%s", got, want)
	}
	for _, unwanted := range []string{"198.51.100.53", "backup.example", "#DIRECT", "skip-cert-verify", "*.*"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("QuanX DNS output contains %q:\n%s", unwanted, got)
		}
	}
}

func TestQuanxInjectorInjectsProxyDNSPolicyWithoutServerRemoteSection(t *testing.T) {
	content := []byte("[dns]\nserver = 223.5.5.5\n\n[policy]\nstatic=direct\n")
	policyContent := []byte("proxy-server-nameserver-policy:\n  \"+.airport.example\":\n    - 192.0.2.53#DIRECT\n")

	loadCalls := 0
	got, err := (QuanxInjector{}).Inject(Context{
		File: "quanx.conf",
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

	dnsSection, _, _, ok := findSection(string(got), "[dns]")
	if !ok {
		t.Fatal("generated QuanX config is missing [dns]")
	}
	if !strings.Contains(dnsSection, "server = 223.5.5.5") {
		t.Fatalf("existing QuanX DNS configuration was not preserved:\n%s", dnsSection)
	}
	if !strings.Contains(dnsSection, "server = /*.airport.example/192.0.2.53") {
		t.Fatalf("generated QuanX DNS section is missing proxy policy:\n%s", dnsSection)
	}
	if strings.Index(string(got), "*.airport.example") > strings.Index(string(got), "[policy]") {
		t.Fatal("QuanX DNS policy was inserted outside [dns]")
	}
}

func TestQuanxInjectorSkipsProxyDNSWithoutDNSSection(t *testing.T) {
	loadCalls := 0
	content := []byte("[server_remote]\n\n[policy]\nstatic=direct\n")
	got, err := (QuanxInjector{}).Inject(Context{
		File: "quanx.conf",
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
		t.Fatalf("QuanX config without [dns] changed:\n%s", got)
	}
}
