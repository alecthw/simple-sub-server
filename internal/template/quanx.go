package template

import "github.com/alecthw/sub-server/internal/subscription"

type QuanxInjector struct{}

func (QuanxInjector) Match(file string) bool {
	return isNamedConfFile(file, "quanx")
}

func (QuanxInjector) Inject(ctx Context, content []byte) ([]byte, error) {
	return (pipeline[string]{codec: textCodec{}, steps: []transform[string]{
		injectQuanxSubscriptions, injectQuanxProxyDNSPolicy,
	}}).Inject(ctx, content)
}

func injectQuanxSubscriptions(ctx Context, content string) (string, error) {
	return injectSection(ctx, content, "[server_remote]", func(section string, entry subscription.Entry) string {
		return appendLine(section, entry.URL+", tag="+entry.Name+", update-interval=86400, opt-parser=true")
	}), nil
}
