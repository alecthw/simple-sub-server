package template

import "strings"

type SurgeInjector struct{}

func (SurgeInjector) Match(file string) bool {
	return isNamedConfFile(file, "surge")
}

func (SurgeInjector) Inject(ctx Context, content []byte) ([]byte, error) {
	result := string(content)
	section, sectionStart, sectionEnd, ok := findSection(result, "[Proxy Group]")
	if !ok {
		return content, nil
	}

	for _, entry := range ctx.Entries {
		if entry.Name == "" || entry.URL == "" {
			continue
		}
		section = appendSurgePolicyName(section, entry.Name)
		section = appendSurgeExternalGroup(section, entry.Name, entry.URL)
	}

	result = result[:sectionStart] + section + result[sectionEnd:]
	if ctx.ManagedURL != "" {
		result = prependSurgeManagedConfig(ctx.ManagedURL, result)
	}

	return []byte(result), nil
}

func prependSurgeManagedConfig(managedURL string, content string) string {
	header := "#!MANAGED-CONFIG " + managedURL
	if strings.HasPrefix(content, header) {
		return content
	}
	return header + "\n" + content
}

func appendSurgePolicyName(section string, name string) string {
	lines := strings.Split(section, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "include-other-group=") {
			continue
		}
		lines[i] = appendIncludeOtherGroup(line, name)
	}
	return strings.Join(lines, "\n")
}

func appendIncludeOtherGroup(line string, name string) string {
	key := "include-other-group=\""
	start := strings.Index(line, key)
	if start < 0 {
		return line
	}
	valueStart := start + len(key)
	valueEnd := strings.Index(line[valueStart:], "\"")
	if valueEnd < 0 {
		return line
	}
	valueEnd += valueStart
	value := line[valueStart:valueEnd]
	parts := splitCommaList(value)
	for _, part := range parts {
		if part == name {
			return line
		}
	}
	parts = append(parts, name)
	return line[:valueStart] + strings.Join(parts, ", ") + line[valueEnd:]
}

func splitCommaList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	items := strings.Split(value, ",")
	var result []string
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func appendSurgeExternalGroup(section string, name string, url string) string {
	line := name + " = select, hidden=0, policy-path=" + url + ", update-interval=86400, icon-url=https://raw.githubusercontent.com/Koolson/Qure/master/IconSet/Color/Airport.png"
	return appendLine(section, line)
}
