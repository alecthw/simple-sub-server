package yamlutil

import (
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

// Marshal encodes YAML with two-space indentation and keeps long quoted
// scalar mapping keys in the conventional "key": form. yaml.v3 otherwise
// emits keys over 128 bytes using the valid but poorly supported ?/: form.
func Marshal(node *yaml.Node) ([]byte, error) {
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(node); err != nil {
		_ = encoder.Close()
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return collapseExplicitQuotedKeys(output.Bytes()), nil
}

func collapseExplicitQuotedKeys(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	result := make([]string, 0, len(lines))
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		indentLength := len(line) - len(strings.TrimLeft(line, " "))
		indent := line[:indentLength]
		trimmed := line[indentLength:]
		if !isCollapsibleExplicitKey(trimmed) || index+1 >= len(lines) {
			result = append(result, line)
			continue
		}

		next := lines[index+1]
		if !strings.HasPrefix(next, indent+":") {
			result = append(result, line)
			continue
		}
		remainder := strings.TrimSpace(strings.TrimPrefix(next, indent+":"))
		key := strings.TrimPrefix(trimmed, "? ")
		if remainder == "" {
			result = append(result, indent+key+":")
		} else if strings.HasPrefix(remainder, "- ") {
			result = append(result, indent+key+":", indent+"  "+remainder)
		} else {
			result = append(result, indent+key+": "+remainder)
		}
		index++
	}
	return []byte(strings.Join(result, "\n"))
}

func isCollapsibleExplicitKey(value string) bool {
	if !strings.HasPrefix(value, "? \"") || !strings.HasSuffix(value, "\"") {
		return false
	}
	key := strings.TrimPrefix(value, "? ")
	return len(key) <= 1024
}
