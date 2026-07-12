package template

import (
	"strings"
	"testing"

	"github.com/alecthw/sub-server/handler/subscription"
)

func TestSurgeInjectorSupportsSurfboardTemplates(t *testing.T) {
	content := []byte(`[General]

[Proxy Group]
Proxy = select, include-other-group="DIRECT"

[Rule]
FINAL,Proxy
`)
	ctx := Context{
		File:       "surfboard_app.conf",
		ManagedURL: "https://sub.example/dlcfg/id/surfboard_app.conf",
		Entries: []subscription.Entry{
			{Name: "SubStore", URL: "https://subs.example/getsub/collection/demo"},
		},
	}

	if !IsSubscribable(ctx.File) {
		t.Fatalf("expected %s to be subscribable", ctx.File)
	}

	gotBytes, err := Inject(ctx, content)
	if err != nil {
		t.Fatal(err)
	}
	got := string(gotBytes)

	wants := []string{
		"#!MANAGED-CONFIG https://sub.example/dlcfg/id/surfboard_app.conf\n",
		`Proxy = select, include-other-group="DIRECT, SubStore"`,
		"SubStore = select, hidden=0, policy-path=https://subs.example/getsub/collection/demo, update-interval=86400, icon-url=https://raw.githubusercontent.com/Koolson/Qure/master/IconSet/Color/Airport.png",
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("generated content missing %q:\n%s", want, got)
		}
	}
}
