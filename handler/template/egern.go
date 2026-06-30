package template

import (
	"github.com/alecthw/sub-server/handler/subscription"
	"gopkg.in/yaml.v3"
)

type EgernInjector struct{}

func (EgernInjector) Match(file string) bool {
	return isNamedYamlFile(file, "egern")
}

func (EgernInjector) Inject(ctx Context, content []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, err
	}

	root := rootMappingNode(&doc)
	if root == nil {
		return content, nil
	}
	if ctx.ManagedURL != "" {
		setEgernAutoUpdateURL(root, ctx.ManagedURL)
	}

	policyGroups := getOrCreateSequenceNode(root, "policy_groups")
	for _, entry := range ctx.Entries {
		if entry.Name == "" || entry.URL == "" {
			continue
		}
		appendPolicyName(policyGroups, entry.Name)
		policyGroups.Content = append(policyGroups.Content, newEgernExternalPolicyGroupNode(entry))
	}

	return marshalYAML(&doc)
}

func appendPolicyName(policyGroups *yaml.Node, name string) {
	for _, group := range policyGroups.Content {
		if group.Kind != yaml.MappingNode {
			continue
		}
		for i := 1; i < len(group.Content); i += 2 {
			body := group.Content[i]
			if body.Kind != yaml.MappingNode {
				continue
			}
			policies := getMappingValue(body, "policies")
			if policies == nil {
				continue
			}
			if !isTrueMappingValue(body, "flatten") {
				continue
			}
			if policies.Kind != yaml.SequenceNode {
				policies.Kind = yaml.SequenceNode
				policies.Tag = "!!seq"
				policies.Content = nil
			}
			appendUniqueString(policies, name)
		}
	}
}

func isTrueMappingValue(root *yaml.Node, key string) bool {
	value := getMappingValue(root, key)
	return value != nil && value.Kind == yaml.ScalarNode && value.Tag == "!!bool" && value.Value == "true"
}

func setEgernAutoUpdateURL(root *yaml.Node, managedURL string) {
	autoUpdate := getOrCreateMappingNode(root, "auto_update")
	setMappingValue(autoUpdate, "url", newStringNode(managedURL))
	if getMappingValue(autoUpdate, "interval") == nil {
		setMappingValue(autoUpdate, "interval", newIntNode("86400"))
	}
}

func newEgernExternalPolicyGroupNode(entry subscription.Entry) *yaml.Node {
	return newMapNode(
		"external", newMapNode(
			"name", newStringNode(entry.Name),
			"type", newStringNode("select"),
			"urls", newSequenceNode(newStringNode(entry.URL)),
			"update_interval", newIntNode("86400"),
			"icon", newStringNode("https://raw.githubusercontent.com/Koolson/Qure/master/IconSet/Color/Airport.png"),
		),
	)
}
