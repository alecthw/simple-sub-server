package template

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/alecthw/sub-server/internal/subscription"
)

func TestYAMLDocumentsPreserveUnsupportedRoots(t *testing.T) {
	for _, injector := range []Injector{ClashInjector{}, StashInjector{}, EgernInjector{}} {
		for _, content := range []string{"", "# comment\n", "scalar\n", "- list\n"} {
			got, err := injector.Inject(Context{}, []byte(content))
			if err != nil || string(got) != content {
				t.Errorf("%T: content %q: got %q, %v", injector, content, got, err)
			}
		}
		if _, err := injector.Inject(Context{}, []byte("broken: [")); err == nil {
			t.Errorf("%T accepted malformed YAML", injector)
		}
	}
}

func TestAppPipelinesFilterEntriesAndPropagateErrors(t *testing.T) {
	entries := []subscription.Entry{{Name: "", URL: "https://anonymous.example"}, {Name: "Empty"}, {Name: "Demo", URL: "https://subscription.example/demo"}}
	original := append([]subscription.Entry(nil), entries...)
	tests := []struct {
		injector Injector
		content  string
	}{
		{ClashInjector{}, "{}"}, {StashInjector{}, "{}"}, {EgernInjector{}, "{}"},
		{SurgeInjector{}, "[Proxy Group]\n[Host]\n"}, {LoonInjector{}, "[Remote Proxy]\n[Host]\n"}, {QuanxInjector{}, "[server_remote]\n[dns]\n"},
	}
	for _, tt := range tests {
		ctx := Context{Entries: entries}
		got, err := tt.injector.Inject(ctx, []byte(tt.content))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(got), "https://subscription.example/demo") || strings.Contains(string(got), "anonymous.example") || strings.Contains(string(got), "Empty") {
			t.Errorf("%T: invalid entry filtering: %s", tt.injector, got)
		}
		wantErr := errors.New("policy unavailable")
		ctx.LoadProxyDNSPolicy = func() ([]byte, error) { return nil, wantErr }
		if _, err := tt.injector.Inject(ctx, []byte(tt.content)); !errors.Is(err, wantErr) {
			t.Errorf("%T: error = %v", tt.injector, err)
		}
	}
	if !reflect.DeepEqual(entries, original) {
		t.Fatal("pipeline modified caller entries")
	}
}

func TestRegistryMatching(t *testing.T) {
	registry := DefaultRegistry()
	for _, file := range []string{"ClashMeta.YAML", "stash.yml", "egern.yaml", "SURGE.CONF", "surfboard.conf", "loon.conf", "quanx.conf"} {
		if registry.Find(file) == nil {
			t.Errorf("no injector for %s", file)
		}
	}
	for _, file := range []string{"custom.json", "clash.conf", "surge.yaml"} {
		if registry.Find(file) != nil {
			t.Errorf("unexpected injector for %s", file)
		}
	}
}
