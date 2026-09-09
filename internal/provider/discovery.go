package provider

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/go-resty/resty/v2"
)

const cfgUserAgent = "Mozilla/5.0 (dart:io) SuperAccelerator"

type cfgResponse struct {
	HostSource string   `json:"host_source"`
	Hosts      []string `json:"hosts"`
}

func fetchBaseURLs(client *resty.Client, cfgURLs []string) ([]string, error) {
	if len(cfgURLs) == 0 {
		return nil, fmt.Errorf("cfgUrls is empty")
	}

	type cfgResult struct {
		index int
		hosts []string
		err   error
	}

	results := make([]cfgResult, len(cfgURLs))
	ch := make(chan cfgResult, len(cfgURLs))
	var wg sync.WaitGroup
	for i, cfgURL := range cfgURLs {
		wg.Add(1)
		go func(index int, url string) {
			defer wg.Done()
			hosts, err := fetchConfigHosts(client, url)
			ch <- cfgResult{index: index, hosts: hosts, err: err}
		}(i, cfgURL)
	}
	wg.Wait()
	close(ch)

	for result := range ch {
		results[result.index] = result
	}

	seen := make(map[string]struct{})
	baseURLs := make([]string, 0)
	var lastErr error
	for _, result := range results {
		if result.err != nil {
			lastErr = result.err
			continue
		}
		for _, host := range result.hosts {
			for _, baseURL := range baseURLCandidates(host) {
				if _, ok := seen[baseURL]; ok {
					continue
				}
				seen[baseURL] = struct{}{}
				baseURLs = append(baseURLs, baseURL)
			}
		}
	}
	if len(baseURLs) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("no hosts found in cfgUrls")
	}
	return baseURLs, nil
}

func fetchConfigHosts(client *resty.Client, cfgURL string) ([]string, error) {
	resp, err := client.R().
		SetHeader("User-Agent", cfgUserAgent).
		Get(cfgURL)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != 200 || len(resp.Body()) == 0 {
		return nil, fmt.Errorf("cfg url returned status %d", resp.StatusCode())
	}

	decoded, err := decodeBase64String(strings.TrimSpace(string(resp.Body())))
	if err != nil {
		return nil, err
	}

	var cfg cfgResponse
	if err := json.Unmarshal(decoded, &cfg); err != nil {
		return nil, err
	}
	hosts := append([]string{}, cfg.Hosts...)
	if cfg.HostSource != "" {
		hosts = append(hosts, cfg.HostSource)
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("cfg hosts is empty")
	}
	return hosts, nil
}

func baseURLCandidates(baseURL string) []string {
	normalized := normalizeBaseURL(baseURL)
	if normalized == "" {
		return nil
	}
	if strings.HasSuffix(normalized, "/api/v1") {
		return []string{normalized}
	}
	if strings.HasSuffix(normalized, "/api") {
		return []string{normalized, normalized + "/v1"}
	}
	return []string{normalized + "/api/v1"}
}

func normalizeBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}
