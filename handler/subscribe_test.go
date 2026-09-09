package handler

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	templateinject "github.com/alecthw/sub-server/internal/template"
	"github.com/go-resty/resty/v2"
	"gopkg.in/yaml.v3"
)

func TestServerLoadsClashProxyDNSPolicy(t *testing.T) {
	workDir := t.TempDir()
	server := New(Config{WorkDir: workDir}, resty.New())
	subDir := filepath.Join(workDir, "sub")
	providerDir := filepath.Join(subDir, "provider")

	uid := "00000000-0000-0000-0000-000000000001"
	userDir := filepath.Join(subDir, uid)
	templateDir := filepath.Join(subDir, "template")
	for _, directory := range []string{userDir, templateDir, providerDir} {
		if err := os.MkdirAll(directory, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(userDir, "subscribe.txt"), []byte("Demo=https://subscription.example/demo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, "proxy-dns.yml"), []byte(`common-domains:
  - github.com
proxy-server-nameserver-policy:
  "+.node.example":
    - 192.0.2.53#DIRECT
`), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := server.store.LoadEntries(uid)
	if err != nil {
		t.Fatal(err)
	}
	got, err := server.templates.Inject(templateinject.Context{
		UID: uid, File: "clash_meta.yaml", Entries: entries, LoadProxyDNSPolicy: server.loadProxyDNSPolicy,
	}, []byte("dns:\n  enable: true\n"))
	if err != nil {
		t.Fatal(err)
	}

	var document struct {
		DNS struct {
			Policy map[string][]string `yaml:"proxy-server-nameserver-policy"`
		} `yaml:"dns"`
	}
	if err := yaml.Unmarshal(got, &document); err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"+.node.example": {"192.0.2.53#DIRECT"},
	}
	if !reflect.DeepEqual(document.DNS.Policy, want) {
		t.Fatalf("injected policy = %#v, want %#v", document.DNS.Policy, want)
	}
}
