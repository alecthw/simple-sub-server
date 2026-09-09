package template

import (
	"fmt"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

type egernDNSGroup struct {
	comment     string
	nameservers []string
	rules       []egernDNSRule
}

type egernDNSRule struct {
	kind  string
	match string
}

func injectEgernProxyDNSPolicy(ctx Context, root *yaml.Node) error {
	if ctx.LoadProxyDNSPolicy == nil {
		return nil
	}

	content, err := ctx.LoadProxyDNSPolicy()
	if err != nil {
		return fmt.Errorf("load proxy dns policy: %w", err)
	}
	groups, err := parseEgernDNSGroups(content)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		return nil
	}

	dns := getOrCreateMappingNode(root, "dns")
	upstreams := getOrCreateMappingNode(dns, "upstreams")
	forward := getOrCreateSequenceNode(dns, "forward")
	usedNames := egernUpstreamNames(upstreams)
	genericIndex := 1
	generatedForward := make([]*yaml.Node, 0)

	for _, group := range groups {
		upstreamName := nextEgernUpstreamName(group.comment, usedNames, &genericIndex)
		nameservers := make([]*yaml.Node, 0, len(group.nameservers))
		for _, nameserver := range group.nameservers {
			nameservers = append(nameservers, newStringNode(nameserver))
		}
		setMappingValue(upstreams, upstreamName, newSequenceNode(nameservers...))

		for index, rule := range group.rules {
			entry := newMapNode(rule.kind, newMapNode(
				"match", newStringNode(rule.match),
				"value", newStringNode(upstreamName),
			))
			if index == 0 {
				entry.HeadComment = cleanEgernComment(group.comment)
			}
			generatedForward = append(generatedForward, entry)
		}
	}

	forward.Content = append(generatedForward, forward.Content...)
	return nil
}

func parseEgernDNSGroups(content []byte) ([]egernDNSGroup, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		return nil, fmt.Errorf("parse proxy dns policy: %w", err)
	}
	root := rootMappingNode(&document)
	if root == nil {
		return nil, fmt.Errorf("proxy dns policy root is not a mapping")
	}
	policy := getMappingValue(root, "proxy-server-nameserver-policy")
	if policy == nil || policy.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("proxy-server-nameserver-policy is missing or invalid")
	}

	groups := make([]egernDNSGroup, 0, len(policy.Content)/2)
	for index := 0; index+1 < len(policy.Content); index += 2 {
		key := policy.Content[index]
		value := policy.Content[index+1]
		if key.Kind != yaml.ScalarNode || value.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("proxy-server-nameserver-policy entry is invalid")
		}

		nameservers := surgeNameservers(value)
		if len(nameservers) == 0 {
			continue
		}
		group := egernDNSGroup{comment: key.HeadComment, nameservers: nameservers}
		for _, domain := range strings.Split(key.Value, ",") {
			rule, ok := egernDNSRuleForDomain(domain)
			if ok {
				group.rules = append(group.rules, rule)
			}
		}
		if len(group.rules) != 0 {
			groups = append(groups, group)
		}
	}
	return groups, nil
}

func egernDNSRuleForDomain(domain string) (egernDNSRule, bool) {
	domain = strings.TrimSpace(domain)
	if domain == "" || domain == "+.*" || domain == "*.*" {
		return egernDNSRule{}, false
	}
	for _, prefix := range []string{"+.", "*."} {
		if strings.HasPrefix(domain, prefix) {
			match := strings.TrimSpace(strings.TrimPrefix(domain, prefix))
			if match == "" {
				return egernDNSRule{}, false
			}
			return egernDNSRule{kind: "domain_suffix", match: match}, true
		}
	}
	return egernDNSRule{kind: "domain", match: domain}, true
}

func egernUpstreamNames(upstreams *yaml.Node) map[string]struct{} {
	used := make(map[string]struct{}, len(upstreams.Content)/2)
	for index := 0; index+1 < len(upstreams.Content); index += 2 {
		used[strings.ToLower(upstreams.Content[index].Value)] = struct{}{}
	}
	return used
}

func nextEgernUpstreamName(comment string, used map[string]struct{}, genericIndex *int) string {
	base := egernProviderUpstreamName(comment)
	if base == "" {
		for {
			candidate := fmt.Sprintf("proxy-dns-%d", *genericIndex)
			*genericIndex = *genericIndex + 1
			if _, exists := used[strings.ToLower(candidate)]; !exists {
				used[strings.ToLower(candidate)] = struct{}{}
				return candidate
			}
		}
	}

	for suffix := 1; ; suffix++ {
		candidate := base
		if suffix > 1 {
			candidate = fmt.Sprintf("%s-%d", base, suffix)
		}
		if _, exists := used[strings.ToLower(candidate)]; !exists {
			used[strings.ToLower(candidate)] = struct{}{}
			return candidate
		}
	}
}

func egernProviderUpstreamName(comment string) string {
	comment = cleanEgernComment(comment)
	if comment == "" || strings.Contains(comment, "\n") || strings.Contains(comment, " / ") {
		return ""
	}

	var name strings.Builder
	lastDash := false
	for _, character := range strings.ToLower(comment) {
		switch {
		case unicode.IsLetter(character), unicode.IsDigit(character), character == '_':
			name.WriteRune(character)
			lastDash = false
		case character == '-':
			if name.Len() > 0 && !lastDash {
				name.WriteRune(character)
				lastDash = true
			}
		default:
			if name.Len() > 0 && !lastDash {
				name.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(name.String(), "-")
}

func cleanEgernComment(comment string) string {
	lines := make([]string, 0)
	for _, line := range strings.Split(comment, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}
