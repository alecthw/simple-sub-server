package template

import (
	"fmt"
	"strings"

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

	if err := injectProxyDNSPolicy(ctx, root); err != nil {
		return nil, err
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

func injectProxyDNSPolicy(ctx Context, root *yaml.Node) error {
	expandedPolicy, err := loadExpandedProxyDNSPolicy(ctx)
	if err != nil {
		return err
	}
	if expandedPolicy == nil {
		return nil
	}

	dns := getOrCreateMappingNode(root, "dns")
	setMappingValue(dns, "proxy-server-nameserver-policy", expandedPolicy)
	return nil
}

func loadExpandedProxyDNSPolicy(ctx Context) (*yaml.Node, error) {
	if ctx.LoadProxyDNSPolicy == nil {
		return nil, nil
	}

	content, err := ctx.LoadProxyDNSPolicy()
	if err != nil {
		return nil, fmt.Errorf("load proxy dns policy: %w", err)
	}

	var policyDocument yaml.Node
	if err := yaml.Unmarshal(content, &policyDocument); err != nil {
		return nil, fmt.Errorf("parse proxy dns policy: %w", err)
	}
	policyRoot := rootMappingNode(&policyDocument)
	if policyRoot == nil {
		return nil, fmt.Errorf("proxy dns policy root is not a mapping")
	}
	policy := getMappingValue(policyRoot, "proxy-server-nameserver-policy")
	if policy == nil || policy.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("proxy-server-nameserver-policy is missing or invalid")
	}
	expandedPolicy, err := expandProxyDNSPolicyRules(policy)
	if err != nil {
		return nil, err
	}
	return expandedPolicy, nil
}

func expandProxyDNSPolicyRules(policy *yaml.Node) (*yaml.Node, error) {
	expanded := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for index := 0; index+1 < len(policy.Content); index += 2 {
		key := policy.Content[index]
		value := policy.Content[index+1]
		if key.Kind != yaml.ScalarNode || value.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("proxy-server-nameserver-policy entry is invalid")
		}

		firstRule := true
		for _, domain := range strings.Split(key.Value, ",") {
			domain = strings.TrimSpace(domain)
			if domain == "" {
				continue
			}
			expandedKey := cloneYAMLNode(key)
			expandedKey.Value = domain
			if !firstRule {
				expandedKey.HeadComment = ""
			}
			expanded.Content = append(expanded.Content, expandedKey, cloneYAMLNode(value))
			firstRule = false
		}
	}
	return expanded, nil
}

func cloneYAMLNode(node *yaml.Node) *yaml.Node {
	cloned := *node
	cloned.Content = make([]*yaml.Node, len(node.Content))
	for index, child := range node.Content {
		cloned.Content[index] = cloneYAMLNode(child)
	}
	return &cloned
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
