package template

import (
	"strings"

	"github.com/alecthw/sub-server/internal/subscription"
)

func findSection(content string, header string) (string, int, int, bool) {
	start := strings.Index(content, header)
	if start < 0 {
		return "", 0, 0, false
	}

	sectionStart := start + len(header)
	rest := content[sectionStart:]
	nextSectionOffset := strings.Index(rest, "\n[")
	sectionEnd := len(content)
	if nextSectionOffset >= 0 {
		sectionEnd = sectionStart + nextSectionOffset
	}

	return content[sectionStart:sectionEnd], sectionStart, sectionEnd, true
}

func appendLine(section string, line string) string {
	trimmed := strings.TrimRight(section, "\n")
	if trimmed == "" {
		return "\n" + line + "\n"
	}
	return trimmed + "\n" + line + "\n"
}

type textCodec struct{}

func (textCodec) Decode(content []byte) (string, bool, error) { return string(content), true, nil }
func (textCodec) Encode(document string) ([]byte, error)      { return []byte(document), nil }

// injectSection applies entries only when the template contains the section.
func injectSection(ctx Context, content, header string, appendEntry func(string, subscription.Entry) string) string {
	section, start, end, ok := findSection(content, header)
	if !ok {
		return content
	}
	for _, entry := range ctx.Entries {
		section = appendEntry(section, entry)
	}
	return content[:start] + section + content[end:]
}
