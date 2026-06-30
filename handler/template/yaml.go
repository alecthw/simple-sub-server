package template

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

func marshalYAML(node *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(node); err != nil {
		_ = encoder.Close()
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func rootMappingNode(doc *yaml.Node) *yaml.Node {
	root := doc
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		root = doc.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	return root
}

func getOrCreateSequenceNode(root *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(root.Content)-1; i += 2 {
		if root.Content[i].Value == key {
			value := root.Content[i+1]
			if value.Kind != yaml.SequenceNode {
				value.Kind = yaml.SequenceNode
				value.Tag = "!!seq"
				value.Content = nil
			}
			return value
		}
	}

	value := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	root.Content = append(root.Content, newStringNode(key), value)
	return value
}

func getMappingValue(root *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(root.Content)-1; i += 2 {
		if root.Content[i].Value == key {
			return root.Content[i+1]
		}
	}
	return nil
}

func appendUniqueString(seq *yaml.Node, value string) {
	for _, item := range seq.Content {
		if item.Value == value {
			return
		}
	}
	seq.Content = append(seq.Content, newStringNode(value))
}

func getOrCreateMappingNode(root *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(root.Content)-1; i += 2 {
		if root.Content[i].Value == key {
			value := root.Content[i+1]
			if value.Kind != yaml.MappingNode {
				value.Kind = yaml.MappingNode
				value.Tag = "!!map"
				value.Content = nil
			}
			return value
		}
	}

	value := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	root.Content = append(root.Content, newStringNode(key), value)
	return value
}

func setMappingValue(root *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i < len(root.Content)-1; i += 2 {
		if root.Content[i].Value == key {
			root.Content[i+1] = value
			return
		}
	}

	root.Content = append(root.Content, newStringNode(key), value)
}

func newSequenceNode(items ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: items}
}

func newMapNode(items ...any) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for i := 0; i < len(items)-1; i += 2 {
		node.Content = append(node.Content, newStringNode(items[i].(string)), items[i+1].(*yaml.Node))
	}
	return node
}

func newStringNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func newIntNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: value}
}

func newBoolNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: value}
}
