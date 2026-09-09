package template

import "github.com/alecthw/sub-server/internal/subscription"

type LoonInjector struct{}

func (LoonInjector) Match(file string) bool {
	return isNamedConfFile(file, "loon")
}

func (LoonInjector) Inject(ctx Context, content []byte) ([]byte, error) {
	return (pipeline[string]{codec: textCodec{}, steps: []transform[string]{
		injectLoonSubscriptions, injectHostProxyDNSPolicy,
	}}).Inject(ctx, content)
}

func injectLoonSubscriptions(ctx Context, content string) (string, error) {
	return injectSection(ctx, content, "[Remote Proxy]", func(section string, entry subscription.Entry) string {
		return appendLine(section, entry.Name+" = "+entry.URL+", udp=true, fast-open=true")
	}), nil
}
