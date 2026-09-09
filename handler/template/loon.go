package template

type LoonInjector struct{}

func (LoonInjector) Match(file string) bool {
	return isNamedConfFile(file, "loon")
}

func (LoonInjector) Inject(ctx Context, content []byte) ([]byte, error) {
	result := string(content)
	section, sectionStart, sectionEnd, ok := findSection(result, "[Remote Proxy]")
	if ok {
		for _, entry := range ctx.Entries {
			if entry.Name == "" || entry.URL == "" {
				continue
			}
			section = appendLine(section, entry.Name+" = "+entry.URL+", udp=true, fast-open=true")
		}
		result = result[:sectionStart] + section + result[sectionEnd:]
	}

	result, err := injectHostProxyDNSPolicy(ctx, result)
	if err != nil {
		return nil, err
	}
	return []byte(result), nil
}
