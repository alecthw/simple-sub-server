package provider

import (
	"bytes"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alecthw/sub-server/handler/yamlutil"
	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

const (
	proxyDNSProviderName     = "proxy-dns"
	proxyDNSFileName         = proxyDNSProviderName + ".yml"
	proxyDNSCommonDomainsKey = "common-domains"
)

var (
	proxyDNSRefreshMu            sync.Mutex
	defaultProxyDNSCommonDomains = []string{"github.com"}
	publicDNSIPs                 = map[string]struct{}{
		"1.0.0.1": {}, "1.1.1.1": {},
		"8.8.4.4": {}, "8.8.8.8": {},
		"9.9.9.9": {}, "149.112.112.112": {},
		"208.67.220.220": {}, "208.67.222.222": {},
		"94.140.14.14": {}, "94.140.15.15": {},
		"76.76.2.0": {}, "76.76.10.0": {},
		"223.5.5.5": {}, "223.6.6.6": {},
		"120.53.53.53": {}, "1.12.12.12": {},
		"119.29.29.29": {}, "182.254.116.116": {},
		"114.114.114.114": {}, "114.114.115.115": {},
		"180.76.76.76": {}, "101.226.4.6": {}, "218.30.118.6": {},
		"2606:4700:4700::1001": {}, "2606:4700:4700::1111": {},
		"2001:4860:4860::8844": {}, "2001:4860:4860::8888": {},
		"2400:3200::1": {}, "2400:3200:baba::1": {},
	}
	publicDNSDomains = []string{
		"dns.alidns.com",
		"dns.pub",
		"doh.pub",
		"dot.pub",
		"dns.google",
		"cloudflare-dns.com",
		"one.one.one.one",
		"quad9.net",
		"opendns.com",
		"adguard-dns.com",
		"doh.360.cn",
		"dns.nextdns.io",
		"dns.mullvad.net",
		"doh.cleanbrowsing.org",
	}
)

type subscriptionDNSConfig struct {
	ProxyServerNameserver []string `yaml:"proxy-server-nameserver"`
}

type subscriptionProxy struct {
	Server string `yaml:"server"`
}

type subscriptionDocument struct {
	DNS                   subscriptionDNSConfig `yaml:"dns"`
	ProxyServerNameserver []string              `yaml:"proxy-server-nameserver"`
	Proxies               []subscriptionProxy   `yaml:"proxies"`
}

type proxyDNSFileConfig struct {
	CommonDomains []string            `yaml:"common-domains"`
	Policy        map[string][]string `yaml:"proxy-server-nameserver-policy"`
}

type providerDNSPolicy struct {
	name         string
	domainRule   string
	nameservers  []string
	probeDomains []string
}

func handleProxyDNS(c *gin.Context, providerDir string, client *resty.Client) {
	data, err := refreshProxyDNSPolicy(providerDir, client)
	if err != nil {
		zap.S().Errorw("failed to refresh proxy dns policy", "error", err)
		c.String(500, "Internal server error")
		return
	}
	writeSubscription(c, &contentResult{body: data})
}

// StartProxyDNSPolicyScheduler refreshes proxy-dns.yml every day at 03:00
// in the server's local timezone.
func StartProxyDNSPolicyScheduler(providerDir string, client *resty.Client) {
	go func() {
		timer := time.NewTimer(time.Until(nextProxyDNSRefresh(time.Now())))
		defer timer.Stop()
		for {
			<-timer.C
			if _, err := refreshProxyDNSPolicy(providerDir, client); err != nil {
				zap.S().Errorw("scheduled proxy dns policy refresh failed", "error", err)
			}
			timer.Reset(time.Until(nextProxyDNSRefresh(time.Now())))
		}
	}()
}

func nextProxyDNSRefresh(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// LoadOrGenerateProxyDNSPolicy reads proxy-dns.yml, generating it first when
// the file is missing, empty, lacks common-domains, or contains a legacy +.* rule.
func LoadOrGenerateProxyDNSPolicy(providerDir string, client *resty.Client) ([]byte, error) {
	path := filepath.Join(providerDir, proxyDNSFileName)
	data, err := os.ReadFile(path)
	if err == nil && len(bytes.TrimSpace(data)) > 0 {
		_, configured, parseErr := parseProxyDNSCommonDomains(data)
		if parseErr != nil {
			return nil, parseErr
		}
		if configured {
			return data, nil
		}
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return refreshProxyDNSPolicy(providerDir, client)
}

func refreshProxyDNSPolicy(providerDir string, client *resty.Client) ([]byte, error) {
	proxyDNSRefreshMu.Lock()
	defer proxyDNSRefreshMu.Unlock()

	if err := os.MkdirAll(providerDir, 0755); err != nil {
		return nil, err
	}
	proxyDNSPath := filepath.Join(providerDir, proxyDNSFileName)
	commonDomains, err := loadProxyDNSCommonDomains(proxyDNSPath)
	if err != nil {
		return nil, err
	}

	providerNames, err := listProviderNames(providerDir)
	if err != nil {
		return nil, err
	}

	policies := make([]providerDNSPolicy, 0, len(providerNames))
	for _, providerName := range providerNames {
		result, _, err := fetchProviderResult(providerDir, providerName, client)
		if err != nil {
			zap.S().Warnw("skipping provider during proxy dns policy refresh", "provider", providerName)
			continue
		}

		policy, ok := extractProviderDNSPolicy(providerName, result, commonDomains)
		if ok {
			policies = append(policies, policy)
		}
	}
	policies = mergeAndPrioritizeDNSPolicies(policies, dnsAvailabilityProbe)

	data, err := marshalProxyDNSPolicy(policies, commonDomains)
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomically(proxyDNSPath, data); err != nil {
		return nil, err
	}
	return data, nil
}

func listProviderNames(providerDir string) ([]string, error) {
	entries, err := os.ReadDir(providerDir)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yml" || entry.Name() == proxyDNSFileName {
			continue
		}
		names = append(names, strings.TrimSuffix(entry.Name(), ".yml"))
	}
	sort.Strings(names)
	return names, nil
}

func extractProviderDNSPolicy(providerName string, result *contentResult, commonDomains []string) (providerDNSPolicy, bool) {
	var document subscriptionDocument
	if err := yaml.Unmarshal(result.body, &document); err != nil {
		return providerDNSPolicy{}, false
	}

	nameserverValues := append([]string{}, document.DNS.ProxyServerNameserver...)
	nameserverValues = append(nameserverValues, document.ProxyServerNameserver...)
	hasPrivateNameserver := false
	for _, nameserver := range nameserverValues {
		if strings.TrimSpace(nameserver) != "" && !isPublicDNS(nameserver) {
			hasPrivateNameserver = true
			break
		}
	}
	if !hasPrivateNameserver {
		return providerDNSPolicy{}, false
	}

	nameservers := make([]string, 0, len(nameserverValues))
	seenNameservers := make(map[string]struct{})
	for _, nameserver := range nameserverValues {
		nameserver = withDirectDNSOptions(nameserver)
		if nameserver == "" {
			continue
		}
		if _, exists := seenNameservers[nameserver]; exists {
			continue
		}
		seenNameservers[nameserver] = struct{}{}
		nameservers = append(nameservers, nameserver)
	}
	if len(nameservers) == 0 {
		return providerDNSPolicy{}, false
	}

	domainSet := make(map[string]struct{})
	probeDomainSet := make(map[string]struct{})
	for _, proxy := range document.Proxies {
		serverDomain := normalizedProxyServerDomain(proxy.Server)
		if serverDomain == "" {
			continue
		}
		probeDomainSet[serverDomain] = struct{}{}
		if domain := proxyServerDomainRule(serverDomain, commonDomains); domain != "" {
			domainSet[domain] = struct{}{}
		}
	}
	if len(domainSet) == 0 {
		return providerDNSPolicy{}, false
	}

	domains := make([]string, 0, len(domainSet))
	for domain := range domainSet {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	probeDomains := make([]string, 0, len(probeDomainSet))
	for domain := range probeDomainSet {
		probeDomains = append(probeDomains, domain)
	}
	sort.Strings(probeDomains)

	return providerDNSPolicy{
		name:         providerDisplayName(providerName, result.profileTitle),
		domainRule:   strings.Join(domains, ","),
		nameservers:  nameservers,
		probeDomains: probeDomains,
	}, true
}

func providerDisplayName(providerName string, profileTitle string) string {
	profileTitle = strings.TrimSpace(profileTitle)
	if len(profileTitle) >= len("base64:") && strings.EqualFold(profileTitle[:len("base64:")], "base64:") {
		if decoded, err := decodeBase64String(strings.TrimSpace(profileTitle[len("base64:"):])); err == nil {
			profileTitle = strings.TrimSpace(string(decoded))
		}
	}
	if decoded, err := url.PathUnescape(profileTitle); err == nil {
		profileTitle = decoded
	}
	if profileTitle == "" {
		return providerName
	}
	return profileTitle
}

func proxyServerDomainRule(server string, commonDomains []string) string {
	server = normalizedProxyServerDomain(server)
	if server == "" {
		return ""
	}
	if isCommonProxyDomain(server, commonDomains) {
		return server
	}

	labels := strings.Split(server, ".")
	return "+." + strings.Join(labels[len(labels)-2:], ".")
}

func isCommonProxyDomain(server string, commonDomains []string) bool {
	for _, commonDomain := range commonDomains {
		if server == commonDomain || strings.HasSuffix(server, "."+commonDomain) {
			return true
		}
	}
	return false
}

func normalizedProxyServerDomain(server string) string {
	server = strings.TrimSpace(strings.TrimSuffix(server, "."))
	server = strings.TrimPrefix(server, "+.")
	server = strings.TrimPrefix(server, "*.")
	if server == "" || net.ParseIP(strings.Trim(server, "[]")) != nil {
		return ""
	}
	if host, _, err := net.SplitHostPort(server); err == nil {
		server = strings.Trim(host, "[]")
	}

	labels := strings.Split(strings.ToLower(server), ".")
	if len(labels) < 2 {
		return ""
	}
	for _, label := range labels {
		if label == "" {
			return ""
		}
	}
	return strings.Join(labels, ".")
}

func isPublicDNS(nameserver string) bool {
	base, _, _ := strings.Cut(strings.TrimSpace(nameserver), "#")
	lowerBase := strings.ToLower(base)
	if lowerBase == "" || lowerBase == "system" || strings.HasPrefix(lowerBase, "dhcp://") || strings.HasPrefix(lowerBase, "rcode://") {
		return true
	}

	host := nameserverHost(base)
	if _, exists := publicDNSIPs[host]; exists {
		return true
	}
	for _, domain := range publicDNSDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func nameserverHost(nameserver string) string {
	nameserver = strings.TrimSpace(nameserver)
	if parsed, err := url.Parse(nameserver); err == nil && parsed.Hostname() != "" {
		return strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	}
	if ip := net.ParseIP(strings.Trim(nameserver, "[]")); ip != nil {
		return strings.ToLower(ip.String())
	}
	if host, _, err := net.SplitHostPort(nameserver); err == nil {
		return strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
	}
	return strings.ToLower(strings.TrimSuffix(nameserver, "."))
}

func withDirectDNSOptions(nameserver string) string {
	base, fragment, _ := strings.Cut(strings.TrimSpace(nameserver), "#")
	if base == "" {
		return ""
	}

	options := []string{"DIRECT"}
	seen := map[string]struct{}{"direct": {}}
	for _, option := range strings.Split(fragment, "&") {
		option = strings.TrimSpace(option)
		if option == "" {
			continue
		}
		key := strings.ToLower(strings.SplitN(option, "=", 2)[0])
		if key == "direct" || key == "skip-cert-verify" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		options = append(options, option)
	}
	if isDoHURL(base) {
		options = append(options, "skip-cert-verify=true")
	}
	return base + "#" + strings.Join(options, "&")
}

func isDoHURL(nameserver string) bool {
	parsed, err := url.Parse(nameserver)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func marshalProxyDNSPolicy(policies []providerDNSPolicy, commonDomains []string) ([]byte, error) {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	commonDomainList := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, domain := range commonDomains {
		commonDomainList.Content = append(commonDomainList.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: domain},
		)
	}
	policyMap := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: proxyDNSCommonDomainsKey},
		commonDomainList,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "proxy-server-nameserver-policy"},
		policyMap,
	)

	for _, policy := range policies {
		appendPolicyNode(policyMap, policy.domainRule, policy.nameservers, policy.name)
	}

	data, err := yamlutil.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("marshal proxy dns policy: %w", err)
	}
	return data, nil
}

func mergeProviderDNSPolicies(policies []providerDNSPolicy) []providerDNSPolicy {
	byDomain := make(map[string]*providerDNSPolicy)
	orderedDomains := make([]string, 0)
	for _, policy := range policies {
		for _, domain := range strings.Split(policy.domainRule, ",") {
			domain = strings.TrimSpace(domain)
			if domain == "" {
				continue
			}

			if existing, ok := byDomain[domain]; ok {
				existing.name = appendPolicyName(existing.name, policy.name)
				preferred := appendUniqueNameservers(nil, policy.nameservers...)
				existing.nameservers = appendUniqueNameservers(preferred, existing.nameservers...)
				continue
			}

			byDomain[domain] = &providerDNSPolicy{
				name:        policy.name,
				domainRule:  domain,
				nameservers: appendUniqueNameservers(nil, policy.nameservers...),
			}
			orderedDomains = append(orderedDomains, domain)
		}
	}

	domainPolicies := make([]providerDNSPolicy, 0, len(orderedDomains))
	for _, domain := range orderedDomains {
		domainPolicies = append(domainPolicies, *byDomain[domain])
	}

	visited := make([]bool, len(domainPolicies))
	merged := make([]providerDNSPolicy, 0, len(domainPolicies))
	for start := range domainPolicies {
		if visited[start] {
			continue
		}

		visited[start] = true
		queue := []int{start}
		component := make([]int, 0, 1)
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			component = append(component, current)
			for candidate := range domainPolicies {
				if visited[candidate] || !nameserversOverlap(domainPolicies[current].nameservers, domainPolicies[candidate].nameservers) {
					continue
				}
				visited[candidate] = true
				queue = append(queue, candidate)
			}
		}

		sort.Ints(component)
		group := providerDNSPolicy{}
		for _, index := range component {
			policy := domainPolicies[index]
			group.name = appendPolicyName(group.name, policy.name)
			if group.domainRule == "" {
				group.domainRule = policy.domainRule
			} else {
				group.domainRule += "," + policy.domainRule
			}
			preferred := appendUniqueNameservers(nil, policy.nameservers...)
			group.nameservers = appendUniqueNameservers(preferred, group.nameservers...)
		}
		merged = append(merged, group)
	}

	return merged
}

func appendPolicyName(existing string, addition string) string {
	names := make([]string, 0)
	seen := make(map[string]struct{})
	for _, value := range []string{existing, addition} {
		for _, name := range strings.Split(value, " / ") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	return strings.Join(names, " / ")
}

func nameserversOverlap(left []string, right []string) bool {
	identities := make(map[string]struct{}, len(left))
	for _, nameserver := range left {
		identities[nameserverIdentity(nameserver)] = struct{}{}
	}
	for _, nameserver := range right {
		if _, exists := identities[nameserverIdentity(nameserver)]; exists {
			return true
		}
	}
	return false
}

func appendPolicyNode(mapping *yaml.Node, rule string, nameservers []string, comment string) {
	key := &yaml.Node{
		Kind:        yaml.ScalarNode,
		Tag:         "!!str",
		Value:       rule,
		Style:       yaml.DoubleQuotedStyle,
		HeadComment: comment,
	}
	values := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, nameserver := range nameservers {
		values.Content = append(values.Content, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: nameserver,
		})
	}
	mapping.Content = append(mapping.Content, key, values)
}

func appendUniqueNameservers(values []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additions))
	for _, value := range values {
		seen[nameserverIdentity(value)] = struct{}{}
	}
	for _, value := range additions {
		identity := nameserverIdentity(value)
		if _, exists := seen[identity]; exists {
			continue
		}
		seen[identity] = struct{}{}
		values = append(values, value)
	}
	return values
}

func nameserverIdentity(nameserver string) string {
	base, _, _ := strings.Cut(strings.TrimSpace(nameserver), "#")
	return strings.ToLower(base)
}

func writeFileAtomically(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".proxy-dns-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = os.Remove(temporaryPath)
	}()

	if err := temporary.Chmod(0644); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
