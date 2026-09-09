package template

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

func injectQuanxProxyDNSPolicy(ctx Context, result string) (string, error) {
	if ctx.LoadProxyDNSPolicy == nil {
		return result, nil
	}

	section, sectionStart, sectionEnd, ok := findSection(result, "[dns]")
	if !ok {
		return result, nil
	}

	content, err := ctx.LoadProxyDNSPolicy()
	if err != nil {
		return "", fmt.Errorf("load proxy dns policy: %w", err)
	}
	block, err := convertProxyDNSPolicyToQuanxDNS(content)
	if err != nil {
		return "", err
	}
	if block == "" {
		return result, nil
	}

	section = appendQuanxDNSBlock(section, block)
	return result[:sectionStart] + section + result[sectionEnd:], nil
}

func convertProxyDNSPolicyToQuanxDNS(content []byte) (string, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		return "", fmt.Errorf("parse proxy dns policy: %w", err)
	}
	root := rootMappingNode(&document)
	if root == nil {
		return "", fmt.Errorf("proxy dns policy root is not a mapping")
	}
	policy := getMappingValue(root, "proxy-server-nameserver-policy")
	if policy == nil || policy.Kind != yaml.MappingNode {
		return "", fmt.Errorf("proxy-server-nameserver-policy is missing or invalid")
	}

	groups := make([]string, 0, len(policy.Content)/2)
	for index := 0; index+1 < len(policy.Content); index += 2 {
		key := policy.Content[index]
		value := policy.Content[index+1]
		if key.Kind != yaml.ScalarNode || value.Kind != yaml.SequenceNode {
			return "", fmt.Errorf("proxy-server-nameserver-policy entry is invalid")
		}

		nameservers := surgeNameservers(value)
		if len(nameservers) == 0 {
			continue
		}
		directive := quanxDNSDirective(nameservers[0])

		lines := make([]string, 0, len(strings.Split(key.Value, ","))+1)
		if comment := surgePolicyComment(key.HeadComment); comment != "" {
			lines = append(lines, comment)
		}
		for _, domain := range strings.Split(key.Value, ",") {
			pattern := surgeHostPattern(domain)
			if pattern == "" {
				continue
			}
			lines = append(lines, directive+" = /"+pattern+"/"+nameservers[0])
		}
		if len(lines) == 0 || (len(lines) == 1 && strings.HasPrefix(lines[0], "#")) {
			continue
		}
		groups = append(groups, strings.Join(lines, "\n"))
	}

	return strings.Join(groups, "\n\n"), nil
}

func quanxDNSDirective(nameserver string) string {
	lower := strings.ToLower(strings.TrimSpace(nameserver))
	switch {
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
		return "doh-server"
	case strings.HasPrefix(lower, "quic://"), strings.HasPrefix(lower, "doq://"):
		return "doq-server"
	default:
		return "server"
	}
}

func appendQuanxDNSBlock(section string, block string) string {
	section = strings.TrimRight(section, "\n")
	if strings.TrimSpace(section) == "" {
		return "\n" + block + "\n"
	}
	return section + "\n\n" + block + "\n"
}
