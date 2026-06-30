package template

import (
	"github.com/alecthw/sub-server/handler/subscription"
	"gopkg.in/yaml.v3"
)

type ClashInjector struct{}

func (ClashInjector) Match(file string) bool {
	return isNamedYamlFile(file, "clash")
}

func (ClashInjector) Inject(ctx Context, content []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, err
	}

	root := rootMappingNode(&doc)
	if root == nil {
		return content, nil
	}

	providers := getOrCreateMappingNode(root, "proxy-providers")
	for _, entry := range ctx.Entries {
		if entry.Name == "" || entry.URL == "" {
			continue
		}
		setMappingValue(providers, entry.Name, newClashProxyProviderNode(entry))
	}

	return marshalYAML(&doc)
}

func newClashProxyProviderNode(entry subscription.Entry) *yaml.Node {
	return newMapNode(
		"type", newStringNode("http"),
		"url", newStringNode(entry.URL),
		"interval", newIntNode("86400"),
		"path", newStringNode("./proxy_provider/"+entry.Name+".yaml"),
		"proxy", newStringNode("DIRECT"),
		"health-check", newMapNode(
			"enable", newBoolNode("true"),
			"interval", newIntNode("1800"),
			"url", newStringNode("http://cp.cloudflare.com/generate_204"),
		),
		"override", newMapNode(
			"skip-cert-verify", newBoolNode("true"),
			"udp", newBoolNode("true"),
		),
	)
}
