package yamlutil

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMarshalCollapsesLongExplicitQuotedKey(t *testing.T) {
	longKey := "+." + strings.Repeat("long-domain.example,", 8) + "+.final.example"
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	policy := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	key := &yaml.Node{
		Kind:        yaml.ScalarNode,
		Tag:         "!!str",
		Value:       longKey,
		Style:       yaml.DoubleQuotedStyle,
		HeadComment: "merged providers",
	}
	servers := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "192.0.2.1#DIRECT"},
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "192.0.2.2#DIRECT"},
	}}
	policy.Content = append(policy.Content, key, servers)
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "proxy-server-nameserver-policy"},
		policy,
	)

	got, err := Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "\n  ? ") || strings.Contains(string(got), "\n  : ") {
		t.Fatalf("long key still uses explicit YAML syntax:\n%s", got)
	}
	wantKey := "  \"" + longKey + "\":\n    - 192.0.2.1#DIRECT"
	if !strings.Contains(string(got), wantKey) {
		t.Fatalf("long key is not emitted conventionally:\n%s", got)
	}

	var decoded struct {
		Policy map[string][]string `yaml:"proxy-server-nameserver-policy"`
	}
	if err := yaml.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("normalized output is invalid YAML: %v\n%s", err, got)
	}
	serversForKey := decoded.Policy[longKey]
	if len(serversForKey) != 2 || serversForKey[0] != "192.0.2.1#DIRECT" || serversForKey[1] != "192.0.2.2#DIRECT" {
		t.Fatalf("decoded servers = %#v", serversForKey)
	}
}
