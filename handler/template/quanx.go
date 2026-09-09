package template

type QuanxInjector struct{}

func (QuanxInjector) Match(file string) bool {
	return isNamedConfFile(file, "quanx")
}

func (QuanxInjector) Inject(ctx Context, content []byte) ([]byte, error) {
	result := string(content)
	section, sectionStart, sectionEnd, ok := findSection(result, "[server_remote]")
	if ok {
		for _, entry := range ctx.Entries {
			if entry.Name == "" || entry.URL == "" {
				continue
			}
			section = appendLine(section, entry.URL+", tag="+entry.Name+", update-interval=86400, opt-parser=true")
		}
		result = result[:sectionStart] + section + result[sectionEnd:]
	}

	result, err := injectQuanxProxyDNSPolicy(ctx, result)
	if err != nil {
		return nil, err
	}
	return []byte(result), nil
}
