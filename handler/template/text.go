package template

import "strings"

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
