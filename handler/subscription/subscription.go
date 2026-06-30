package subscription

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Entry represents one line in subscribe.txt.
type Entry struct {
	Name string
	URL  string
}

// LoadEntries reads subscribe.txt from a uuid directory.
func LoadEntries(subDir string, uid string) ([]Entry, error) {
	urlFilePath := filepath.Join(subDir, uid, "subscribe.txt")

	fh, err := os.Open(urlFilePath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = fh.Close()
	}()

	scanner := bufio.NewScanner(fh)
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
		if hasName {
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
