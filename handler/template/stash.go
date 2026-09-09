package template

import (
	"fmt"
	"strings"

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

	if err := injectStashProxyDNSPolicy(ctx, root); err != nil {
		return nil, err
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

func injectStashProxyDNSPolicy(ctx Context, root *yaml.Node) error {
	if dns := getMappingValue(root, "dns"); dns != nil && dns.Kind == yaml.MappingNode {
		deleteMappingValue(dns, "proxy-server-nameserver-policy")
	}

	policy, err := loadExpandedProxyDNSPolicy(ctx)
	if err != nil {
		return err
	}
	if policy == nil {
		return nil
	}
	nameservers, err := stripStashDNSOptions(policy)
	if err != nil {
		return err
	}

	dns := getOrCreateMappingNode(root, "dns")
	setMappingValue(dns, "nameserver-policy", policy)
	proxyNameservers := getOrCreateSequenceNode(dns, "proxy-server-nameserver")
	for _, nameserver := range nameservers {
		appendUniqueString(proxyNameservers, nameserver)
	}
	return nil
}

func stripStashDNSOptions(policy *yaml.Node) ([]string, error) {
	nameservers := make([]string, 0)
	allSeen := make(map[string]struct{})
	for index := 1; index < len(policy.Content); index += 2 {
		sequence := policy.Content[index]
		if sequence.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("proxy-server-nameserver-policy entry is invalid")
		}

		cleanedNodes := make([]*yaml.Node, 0, len(sequence.Content))
		ruleSeen := make(map[string]struct{})
		for _, item := range sequence.Content {
			if item.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("proxy-server-nameserver-policy nameserver is invalid")
			}
			nameserver, _, _ := strings.Cut(strings.TrimSpace(item.Value), "#")
			if nameserver == "" {
				continue
			}
			if _, exists := ruleSeen[nameserver]; !exists {
				cleaned := cloneYAMLNode(item)
				cleaned.Value = nameserver
				cleanedNodes = append(cleanedNodes, cleaned)
				ruleSeen[nameserver] = struct{}{}
			}
			if _, exists := allSeen[nameserver]; !exists {
				nameservers = append(nameservers, nameserver)
				allSeen[nameserver] = struct{}{}
			}
		}
		sequence.Content = cleanedNodes
	}
	return nameservers, nil
}

func newStashProxyProviderNode(entry subscription.Entry) *yaml.Node {
	return newMapNode(
		"url", newStringNode(entry.URL),
		"interval", newIntNode("86400"),
	)
}
