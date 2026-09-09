package provider

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	dnsProbeTimeout     = 8 * time.Second
	dnsProbeConcurrency = 8
	maxDNSProbeDomains  = 3
)

type dnsProbeFunc func(nameserver string, domains []string) bool

var dnsAvailabilityProbe dnsProbeFunc = probeDNSServer

type dnsProbeTarget struct {
	nameserver string
	domains    []string
}

type dnsProbeResult struct {
	identity  string
	available bool
}

func mergeAndPrioritizeDNSPolicies(policies []providerDNSPolicy, probe dnsProbeFunc) []providerDNSPolicy {
	targets := make(map[string]*dnsProbeTarget)
	for _, policy := range policies {
		for _, nameserver := range policy.nameservers {
			identity := nameserverIdentity(nameserver)
			target, exists := targets[identity]
			if !exists {
				target = &dnsProbeTarget{nameserver: nameserver}
				targets[identity] = target
			}
			target.domains = appendUniqueStrings(target.domains, policy.probeDomains...)
		}
	}

	results := make(chan dnsProbeResult, len(targets))
	semaphore := make(chan struct{}, dnsProbeConcurrency)
	var waitGroup sync.WaitGroup
	for identity, target := range targets {
		waitGroup.Add(1)
		go func(identity string, target *dnsProbeTarget) {
			defer waitGroup.Done()
			semaphore <- struct{}{}
			defer func() {
				<-semaphore
			}()
			results <- dnsProbeResult{
				identity:  identity,
				available: probe(target.nameserver, target.domains),
			}
		}(identity, target)
	}
	waitGroup.Wait()
	close(results)

	availability := make(map[string]bool, len(targets))
	for result := range results {
		availability[result.identity] = result.available
	}
	policies = mergeProviderDNSPolicies(policies)

	prioritized := make([]providerDNSPolicy, 0, len(policies))
	for _, policy := range policies {
		availableNameservers := make([]string, 0, len(policy.nameservers))
		unavailableNameservers := make([]string, 0, len(policy.nameservers))
		for _, nameserver := range policy.nameservers {
			if availability[nameserverIdentity(nameserver)] {
				availableNameservers = append(availableNameservers, nameserver)
			} else {
				unavailableNameservers = append(unavailableNameservers, nameserver)
			}
		}
		policy.nameservers = append(availableNameservers, unavailableNameservers...)
		prioritized = append(prioritized, policy)
	}

	availableTargets := 0
	for _, available := range availability {
		if available {
			availableTargets++
		}
	}
	zap.S().Infow("dns availability probe completed",
		"candidates", len(targets),
		"available", availableTargets,
		"unavailable", len(targets)-availableTargets,
	)
	return prioritized
}

func probeDNSServer(nameserver string, domains []string) bool {
	if len(domains) == 0 {
		return false
	}
	if len(domains) > maxDNSProbeDomains {
		domains = domains[:maxDNSProbeDomains]
	}

	ctx, cancel := context.WithTimeout(context.Background(), dnsProbeTimeout)
	defer cancel()

	base := nameserverIdentity(nameserver)
	if isDoHURL(base) {
		transport := &http.Transport{
			Proxy: nil,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // Required by generated skip-cert-verify=true semantics.
			},
			ForceAttemptHTTP2: true,
		}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport}
		for _, domain := range domains {
			if probeDoHDomain(ctx, client, base, domain) {
				return true
			}
		}
		return false
	}

	address, ok := udpDNSAddress(base)
	if !ok {
		return false
	}
	for _, domain := range domains {
		if probeUDPDomain(ctx, address, domain) {
			return true
		}
	}
	return false
}

func probeDoHDomain(ctx context.Context, client *http.Client, endpoint string, domain string) bool {
	for _, queryType := range []uint16{1, 28} {
		query, id, err := buildDNSQuery(domain, queryType)
		if err != nil {
			continue
		}
		if response, err := doHRequest(ctx, client, http.MethodPost, endpoint, query); err == nil && validDNSAnswer(response, id) {
			return true
		}
		if response, err := doHRequest(ctx, client, http.MethodGet, endpoint, query); err == nil && validDNSAnswer(response, id) {
			return true
		}
	}
	return false
}

func doHRequest(ctx context.Context, client *http.Client, method string, endpoint string, query []byte) ([]byte, error) {
	requestURL := endpoint
	var body io.Reader
	if method == http.MethodGet {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return nil, err
		}
		values := parsed.Query()
		values.Set("dns", base64.RawURLEncoding.EncodeToString(query))
		parsed.RawQuery = values.Encode()
		requestURL = parsed.String()
	} else {
		body = bytes.NewReader(query)
	}

	request, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/dns-message")
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/dns-message")
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DoH returned status %d", response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, 64*1024))
}

func probeUDPDomain(ctx context.Context, address string, domain string) bool {
	for _, queryType := range []uint16{1, 28} {
		query, id, err := buildDNSQuery(domain, queryType)
		if err != nil {
			continue
		}
		connection, err := (&net.Dialer{}).DialContext(ctx, "udp", address)
		if err != nil {
			return false
		}
		if deadline, ok := ctx.Deadline(); ok {
			_ = connection.SetDeadline(deadline)
		}
		_, writeErr := connection.Write(query)
		response := make([]byte, 64*1024)
		readCount, readErr := connection.Read(response)
		_ = connection.Close()
		if writeErr == nil && readErr == nil && validDNSAnswer(response[:readCount], id) {
			return true
		}
		if ctx.Err() != nil {
			return false
		}
	}
	return false
}

func udpDNSAddress(nameserver string) (string, bool) {
	if strings.Contains(nameserver, "://") {
		parsed, err := url.Parse(nameserver)
		if err != nil || parsed.Scheme != "udp" || parsed.Hostname() == "" {
			return "", false
		}
		port := parsed.Port()
		if port == "" {
			port = "53"
		}
		return net.JoinHostPort(parsed.Hostname(), port), true
	}
	if host, port, err := net.SplitHostPort(nameserver); err == nil {
		return net.JoinHostPort(strings.Trim(host, "[]"), port), true
	}
	if nameserver == "" {
		return "", false
	}
	return net.JoinHostPort(strings.Trim(nameserver, "[]"), "53"), true
}

func buildDNSQuery(domain string, queryType uint16) ([]byte, uint16, error) {
	domain = strings.TrimSuffix(strings.TrimSpace(domain), ".")
	if domain == "" {
		return nil, 0, fmt.Errorf("DNS query domain is empty")
	}

	idBytes := [2]byte{}
	if _, err := rand.Read(idBytes[:]); err != nil {
		return nil, 0, err
	}
	id := binary.BigEndian.Uint16(idBytes[:])
	query := make([]byte, 12, 512)
	binary.BigEndian.PutUint16(query[0:2], id)
	binary.BigEndian.PutUint16(query[2:4], 0x0100)
	binary.BigEndian.PutUint16(query[4:6], 1)
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 {
			return nil, 0, fmt.Errorf("invalid DNS label length")
		}
		query = append(query, byte(len(label)))
		query = append(query, label...)
	}
	query = append(query, 0, byte(queryType>>8), byte(queryType), 0, 1)
	return query, id, nil
}

func validDNSAnswer(response []byte, id uint16) bool {
	if len(response) < 12 || binary.BigEndian.Uint16(response[0:2]) != id {
		return false
	}
	flags := binary.BigEndian.Uint16(response[2:4])
	answerCount := binary.BigEndian.Uint16(response[6:8])
	return flags&0x8000 != 0 && flags&0x000f == 0 && answerCount > 0
}

func appendUniqueStrings(values []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additions))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}
