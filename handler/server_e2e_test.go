package handler_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthw/sub-server/handler"
	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"gopkg.in/yaml.v3"
)

const testUID = "00000000-0000-0000-0000-000000000001"

func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	filename := filepath.Join(dir, "sub", name)
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func startServer(t *testing.T, dir, converter, managed string) *httptest.Server {
	t.Helper()
	app := handler.New(handler.Config{WorkDir: dir, SubconverterURL: converter, ManagedConfigPrefix: managed}, resty.New())
	router := gin.New()
	app.RegisterRoutes(router)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	server.Client().CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return server
}

func request(t *testing.T, server *httptest.Server, route string, status int) (http.Header, string) {
	t.Helper()
	resp, err := server.Client().Get(server.URL + route)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != status {
		t.Fatalf("GET %s: status = %d, want %d; body = %s", route, resp.StatusCode, status, body)
	}
	return resp.Header, string(body)
}

func TestSubscriptionAppsE2E(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, testUID+"/subscribe.txt", "# subscriptions\nDemo=https://subscription.example/demo\nBackup=https://subscription.example/backup\n")
	writeFixture(t, dir, "provider/proxy-dns.yml", "common-domains: [github.com]\nproxy-server-nameserver-policy:\n  '+.node.example': [192.0.2.53#DIRECT]\n")
	tests := []struct {
		file, input string
		contains    []string
	}{
		{"Clash.yaml", "# keep comment\ndns: {enable: true}\nrules: [MATCH,DIRECT]\n", []string{"proxy-providers:", "proxy-server-nameserver-policy:", "192.0.2.53#DIRECT", "# keep comment"}},
		{"Stash.yml", "dns: {enable: true}\nrules: [MATCH,DIRECT]\n", []string{"proxy-providers:", "nameserver-policy:", "192.0.2.53"}},
		{"Egern.yaml", "policy_groups:\n  - select:\n      name: Main\n      flatten: true\n      policies: [DIRECT]\n", []string{"external:", "auto_update:", "upstreams:", "match: node.example"}},
		{"Surge.conf", "[Proxy Group]\nMain = select, include-other-group=\"Existing\"\n[Host]\n[Rule]\nFINAL,DIRECT\n", []string{"include-other-group=\"Existing, Demo, Backup\"", "policy-path=https://subscription.example/demo", "*.node.example = server:192.0.2.53", "FINAL,DIRECT"}},
		{"Surfboard.conf", "[Proxy Group]\n[Host]\n", []string{"policy-path=https://subscription.example/demo", "*.node.example = server:192.0.2.53"}},
		{"Loon.conf", "[Remote Proxy]\n[Host]\n[Rule]\nFINAL,DIRECT\n", []string{"Demo = https://subscription.example/demo", "*.node.example = server:192.0.2.53", "FINAL,DIRECT"}},
		{"quanx.conf", "[server_remote]\n[dns]\n[filter_local]\nfinal, direct\n", []string{"https://subscription.example/demo, tag=Demo", "server = /*.node.example/192.0.2.53", "final, direct"}},
	}
	for _, tt := range tests {
		writeFixture(t, dir, "template/"+tt.file, tt.input)
	}
	server := startServer(t, dir, "", "https://config.example/dl")
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			headers, body := request(t, server, "/"+testUID+"/"+tt.file, http.StatusOK)
			if headers.Get("Content-Type") != "text/plain; charset=UTF-8" {
				t.Fatalf("content type = %s", headers.Get("Content-Type"))
			}
			for _, want := range append(tt.contains, "https://subscription.example/backup") {
				if !strings.Contains(body, want) {
					t.Errorf("missing %q in:\n%s", want, body)
				}
			}
			if strings.HasSuffix(tt.file, ".yaml") || strings.HasSuffix(tt.file, ".yml") {
				var document map[string]any
				if err := yaml.Unmarshal([]byte(body), &document); err != nil {
					t.Fatal(err)
				}
			}
			if tt.file == "Stash.yml" && strings.Contains(body, "#DIRECT") {
				t.Error("Stash DNS retained Mihomo options")
			}
			if tt.file == "Surge.conf" || tt.file == "Surfboard.conf" {
				if !strings.HasPrefix(body, "#!MANAGED-CONFIG https://config.example/dl/"+testUID+"/"+tt.file+"\n") {
					t.Error("missing managed header")
				}
			}
		})
	}
}

func TestSubscriptionAccessE2E(t *testing.T) {
	tests := []struct {
		name, route string
		files       map[string]string
		status      int
		body        string
	}{
		{"local file wins", "/" + testUID + "/Clash.yaml", map[string]string{testUID + "/Clash.yaml": "local\n", "template/Clash.yaml": "invalid: ["}, 200, "local\n"},
		{"invalid uuid", "/invalid/Clash.yaml", nil, 403, "Forbidden"},
		{"unknown user", "/" + testUID + "/Clash.yaml", nil, 404, "Not found"},
		{"private subscriptions", "/" + testUID + "/subscribe.txt", map[string]string{testUID + "/subscribe.txt": "secret"}, 403, "Forbidden"},
		{"private whitelist", "/" + testUID + "/whitelist.txt", map[string]string{testUID + "/whitelist.txt": "secret"}, 403, "Forbidden"},
		{"parent traversal", "/" + testUID + "/..%5Csecret.yaml", nil, 403, "Forbidden"},
		{"whitelist denial", "/" + testUID + "/Clash.yaml", map[string]string{testUID + "/whitelist.txt": "Stash.yaml\n", testUID + "/Clash.yaml": "local"}, 403, "Forbidden"},
		{"empty whitelist", "/" + testUID + "/Clash.yaml", map[string]string{testUID + "/whitelist.txt": "# deny all\n", testUID + "/Clash.yaml": "local"}, 403, "Forbidden"},
		{"whitelist allows", "/" + testUID + "/custom.json", map[string]string{testUID + "/whitelist.txt": "# allow\n custom.json \n", testUID + "/custom.json": "{}"}, 200, "{}"},
		{"template needs subscriptions", "/" + testUID + "/Clash.yaml", map[string]string{testUID + "/other.json": "{}", "template/Clash.yaml": "{}"}, 404, "Not found"},
		{"unsupported fallback", "/" + testUID + "/custom.json", map[string]string{testUID + "/subscribe.txt": "Demo=https://example.com", "template/custom.json": "{}"}, 404, "Not found"},
		{"malformed template", "/" + testUID + "/Clash.yaml", map[string]string{testUID + "/subscribe.txt": "Demo=https://example.com", "template/Clash.yaml": "invalid: ["}, 500, "Internal server error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tt.files {
				writeFixture(t, dir, name, content)
			}
			server := startServer(t, dir, "", "")
			_, body := request(t, server, tt.route, tt.status)
			if body != tt.body {
				t.Fatalf("body = %q, want %q", body, tt.body)
			}
		})
	}
}

func TestSubconverterAndRedirectE2E(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sub" || r.URL.Query().Get("target") != "surge" || r.URL.Query().Get("url") != "https://subscription.example/demo|https://subscription.example/backup" {
			t.Errorf("unexpected converter request: %s", r.URL)
		}
		_, _ = w.Write([]byte("[General]\n"))
	}))
	defer upstream.Close()
	dir := t.TempDir()
	writeFixture(t, dir, testUID+"/subscribe.txt", "Demo=https://subscription.example/demo\nBackup=https://subscription.example/backup\n")
	writeFixture(t, dir, "subconv/surge.ini", "[Profile]\ntarget=surge\n")
	writeFixture(t, dir, "subconv/redirect.ini", "[Redirect]\nfile=Surge.conf\n")
	writeFixture(t, dir, "subconv/unsafe.ini", "[Redirect]\nfile=../secret.conf\n")
	server := startServer(t, dir, upstream.URL, "https://config.example/dl")
	_, body := request(t, server, "/"+testUID+"/surge.ini", 200)
	want := "#!MANAGED-CONFIG https://config.example/dl/" + testUID + "/surge.ini interval=43200 strict=true\n[General]\n"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
	headers, _ := request(t, server, "/"+testUID+"/redirect.ini", 302)
	if headers.Get("Location") != "https://config.example/dl/"+testUID+"/Surge.conf" {
		t.Fatal(headers)
	}
	request(t, server, "/"+testUID+"/unsafe.ini", 404)
}

func TestProviderE2E(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "clash-verge" {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		w.Header().Set("subscription-userinfo", "upload=1; download=2; total=100")
		_, _ = w.Write([]byte("proxies: []\n"))
	}))
	defer upstream.Close()
	dir := t.TempDir()
	writeFixture(t, dir, "provider/demo.yml", "subscribeUrl: "+upstream.URL+"\n")
	server := startServer(t, dir, "", "")
	headers, body := request(t, server, "/provider/demo", 200)
	if body != "proxies: []\n" || headers.Get("subscription-userinfo") != "upload=1; download=2; total=100" {
		t.Fatalf("provider response: %v %s", headers, body)
	}
	request(t, server, "/provider/missing", 404)
}

func TestServerIsolationE2E(t *testing.T) {
	servers := make([]*httptest.Server, 2)
	for i, name := range []string{"First", "Second"} {
		dir := t.TempDir()
		writeFixture(t, dir, testUID+"/subscribe.txt", name+"=https://subscription.example/"+name+"\n")
		writeFixture(t, dir, "template/Surge.conf", "[Proxy Group]\n")
		servers[i] = startServer(t, dir, "", "https://config.example/"+name+"/")
	}
	for i, name := range []string{"First", "Second"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for range 10 {
				_, body := request(t, servers[i], "/"+testUID+"/Surge.conf", 200)
				if !strings.Contains(body, "policy-path=https://subscription.example/"+name) || !strings.HasPrefix(body, "#!MANAGED-CONFIG https://config.example/"+name+"/"+testUID+"/Surge.conf\n") {
					t.Fatalf("server configuration leaked across instances: %s", body)
				}
			}
		})
	}
}

func TestSubconverterFailureAndManagedURLsE2E(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("target") == "fail" {
			http.Error(w, "private upstream details", 503)
			return
		}
		if r.URL.Query().Get("url") != "https://subscription.example/custom?token=demo" {
			t.Errorf("explicit profile URL lost: %s", r.URL)
		}
		_, _ = w.Write([]byte("converted\n"))
	}))
	defer upstream.Close()
	dir := t.TempDir()
	writeFixture(t, dir, testUID+"/subscribe.txt", "Demo=https://subscription.example/demo\n")
	writeFixture(t, dir, "subconv/surge.ini", "[Profile]\ntarget=surge\nurl=https://subscription.example/custom?token=demo\n")
	writeFixture(t, dir, "subconv/fail.ini", "[Profile]\ntarget=fail\n")
	writeFixture(t, dir, "subconv/redirect.ini", "[Redirect]\nfile=Surge.conf\n")
	for _, prefix := range []string{"", "https://config.example/dl/"} {
		t.Run(prefix, func(t *testing.T) {
			server := startServer(t, dir, upstream.URL, prefix)
			_, body := request(t, server, "/"+testUID+"/surge.ini?download=1", 200)
			want := "converted\n"
			if prefix != "" {
				want = "#!MANAGED-CONFIG https://config.example/dl/" + testUID + "/surge.ini?download=1 interval=43200 strict=true\n" + want
			}
			if body != want {
				t.Fatalf("body = %q, want %q", body, want)
			}
			_, body = request(t, server, "/"+testUID+"/fail.ini", 502)
			if body != "Bad gateway" {
				t.Fatalf("unexpected error response: %q", body)
			}
			headers, _ := request(t, server, "/"+testUID+"/redirect.ini", 302)
			location := "/" + testUID + "/Surge.conf"
			if prefix != "" {
				location = strings.TrimRight(prefix, "/") + location
			}
			if headers.Get("Location") != location {
				t.Fatalf("location = %q, want %q", headers.Get("Location"), location)
			}
		})
	}
	unavailable := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	unavailable.Close()
	server := startServer(t, dir, unavailable.URL, "")
	request(t, server, "/"+testUID+"/surge.ini", 502)
}

func TestFileBoundariesE2E(t *testing.T) {
	t.Run("symlink outside user directory", func(t *testing.T) {
		dir := t.TempDir()
		writeFixture(t, dir, testUID+"/subscribe.txt", "Demo=https://example.com\n")
		writeFixture(t, dir, "private/secret.yaml", "secret content")
		writeFixture(t, dir, "template/Clash.yaml", "{}")
		if err := os.Symlink(filepath.Join(dir, "sub/private/secret.yaml"), filepath.Join(dir, "sub", testUID, "Clash.yaml")); err != nil {
			t.Fatal(err)
		}
		server := startServer(t, dir, "", "")
		_, body := request(t, server, "/"+testUID+"/Clash.yaml", 500)
		if body != "Internal server error" {
			t.Fatalf("unexpected body: %s", body)
		}
	})
	t.Run("directory cannot trigger fallback", func(t *testing.T) {
		dir := t.TempDir()
		writeFixture(t, dir, testUID+"/Clash.yaml/child", "not a file")
		writeFixture(t, dir, "template/Clash.yaml", "{}")
		server := startServer(t, dir, "", "")
		request(t, server, "/"+testUID+"/Clash.yaml", 500)
	})
	t.Run("whitelist must be fully readable", func(t *testing.T) {
		dir := t.TempDir()
		writeFixture(t, dir, testUID+"/Clash.yaml", "local")
		writeFixture(t, dir, testUID+"/whitelist.txt", "Clash.yaml\n"+strings.Repeat("x", 70*1024))
		server := startServer(t, dir, "", "")
		request(t, server, "/"+testUID+"/Clash.yaml", 403)
	})
	t.Run("provider path traversal", func(t *testing.T) {
		server := startServer(t, t.TempDir(), "", "")
		request(t, server, "/provider/..%5Csecret", 403)
	})
}
