package provider

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func loadProxyDNSCommonDomains(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return append([]string(nil), defaultProxyDNSCommonDomains...), nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return append([]string(nil), defaultProxyDNSCommonDomains...), nil
	}

	commonDomains, _, err := parseProxyDNSCommonDomains(data)
	return commonDomains, err
}

func parseProxyDNSCommonDomains(data []byte) ([]string, bool, error) {
	var config proxyDNSFileConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, false, fmt.Errorf("parse proxy dns common domains: %w", err)
	}

	commonDomains := normalizeCommonDomains(config.CommonDomains)
	if len(commonDomains) == 0 {
		return append([]string(nil), defaultProxyDNSCommonDomains...), false, nil
	}
	if _, hasLegacyCatchAll := config.Policy["+.*"]; hasLegacyCatchAll {
		return commonDomains, false, nil
	}
	return commonDomains, true, nil
}

func normalizeCommonDomains(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		domain := normalizedProxyServerDomain(value)
		if domain == "" {
			continue
		}
		if _, exists := seen[domain]; exists {
			continue
		}
		seen[domain] = struct{}{}
		normalized = append(normalized, domain)
	}
	return normalized
}
