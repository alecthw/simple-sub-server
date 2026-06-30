package template

import (
	"github.com/alecthw/sub-server/handler/subscription"
	"gopkg.in/yaml.v3"
)

type StashInjector struct{}

func (StashInjector) Match(file string) bool {
	return isNamedYamlFile(file, "stash")
}

func (StashInjector) Inject(ctx Context, content []byte) ([]byte, error) {
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
		setMappingValue(providers, entry.Name, newStashProxyProviderNode(entry))
	}

	return marshalYAML(&doc)
}

func newStashProxyProviderNode(entry subscription.Entry) *yaml.Node {
	return newMapNode(
		"url", newStringNode(entry.URL),
		"interval", newIntNode("86400"),
	)
}
