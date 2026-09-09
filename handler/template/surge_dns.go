package template

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// injectHostProxyDNSPolicy injects the shared proxy DNS policy into clients
// whose [Host] syntax is compatible with Surge.
func injectHostProxyDNSPolicy(ctx Context, result string) (string, error) {
	if ctx.LoadProxyDNSPolicy == nil {
		return result, nil
	}

	section, sectionStart, sectionEnd, ok := findSection(result, "[Host]")
	if !ok {
		return result, nil
	}

	content, err := ctx.LoadProxyDNSPolicy()
	if err != nil {
		return "", fmt.Errorf("load proxy dns policy: %w", err)
	}
	block, err := convertProxyDNSPolicyToSurgeHost(content)
	if err != nil {
		return "", err
	}
	if block == "" {
		return result, nil
	}

	section = appendSurgeHostBlock(section, block)
	return result[:sectionStart] + section + result[sectionEnd:], nil
}

func convertProxyDNSPolicyToSurgeHost(content []byte) (string, error) {
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

		rules := make([]string, 0)
		for _, domain := range strings.Split(key.Value, ",") {
			pattern := surgeHostPattern(domain)
			if pattern == "" {
				continue
			}
			rules = append(rules, pattern+" = server:"+strings.Join(nameservers, ","))
		}
		if len(rules) == 0 {
			continue
		}

		lines := make([]string, 0, len(rules)+2)
		if comment := surgePolicyComment(key.HeadComment); comment != "" {
			lines = append(lines, comment)
		}
		lines = append(lines, rules...)
		groups = append(groups, strings.Join(lines, "\n"))
	}

	return strings.Join(groups, "\n\n"), nil
}

func surgeNameservers(sequence *yaml.Node) []string {
	nameservers := make([]string, 0, len(sequence.Content))
	seen := make(map[string]struct{}, len(sequence.Content))
	for _, node := range sequence.Content {
		if node.Kind != yaml.ScalarNode {
			continue
		}
		nameserver, _, _ := strings.Cut(strings.TrimSpace(node.Value), "#")
		if nameserver == "" {
			continue
		}
		identity := strings.ToLower(nameserver)
		if _, exists := seen[identity]; exists {
			continue
		}
		seen[identity] = struct{}{}
		nameservers = append(nameservers, nameserver)
	}
	return nameservers
}

func surgeHostPattern(domain string) string {
	domain = strings.TrimSpace(domain)
	if domain == "+.*" || domain == "*.*" {
		return ""
	}
	if strings.HasPrefix(domain, "+.") {
		return "*." + strings.TrimPrefix(domain, "+.")
	}
	return domain
}

func surgePolicyComment(comment string) string {
	lines := make([]string, 0)
	for _, line := range strings.Split(comment, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
		if line == "" {
			continue
		}
		lines = append(lines, "# "+line)
	}
	return strings.Join(lines, "\n")
}

func appendSurgeHostBlock(section string, block string) string {
	section = strings.TrimRight(section, "\n")
	if strings.TrimSpace(section) == "" {
		return "\n" + block + "\n"
	}
	return section + "\n\n" + block + "\n"
}
