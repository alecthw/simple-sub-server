package template

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

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
