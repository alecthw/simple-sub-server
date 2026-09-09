package subscription

import (
	"bufio"
	"io"
	"strings"
)

// Entry represents one line in subscribe.txt.
type Entry struct {
	Name string
	URL  string
}

// Parse reads named or anonymous subscriptions from a text stream.
func Parse(reader io.Reader) ([]Entry, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Split(bufio.ScanLines)

	var entries []Entry
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		entry := Entry{URL: trimmed}
		name, url, hasName := strings.Cut(trimmed, "=")
		if hasName && !strings.Contains(name, "://") {
			entry.Name = strings.TrimSpace(name)
			entry.URL = strings.TrimSpace(url)
		}
		if entry.URL != "" {
			entries = append(entries, entry)
		}
	}

	return entries, scanner.Err()
}

// JoinURLs joins only the URL part of subscription entries for subconverter.
func JoinURLs(entries []Entry) string {
	var urls []string
	for _, entry := range entries {
		urls = append(urls, entry.URL)
	}
	return strings.Join(urls, "|")
}
