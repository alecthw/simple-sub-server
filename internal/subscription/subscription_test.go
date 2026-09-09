package subscription

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseSubscriptions(t *testing.T) {
	entries, err := Parse(strings.NewReader("# ignored\n\n Demo = https://example.com/sub?token=a=b \r\nhttps://example.com/anonymous?token=a=b\nEmpty=\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{{Name: "Demo", URL: "https://example.com/sub?token=a=b"}, {URL: "https://example.com/anonymous?token=a=b"}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries = %#v, want %#v", entries, want)
	}
	if got := JoinURLs(entries); got != "https://example.com/sub?token=a=b|https://example.com/anonymous?token=a=b" {
		t.Fatalf("joined URLs = %q", got)
	}
}

func TestParseReportsIncompleteInput(t *testing.T) {
	_, err := Parse(strings.NewReader("Demo=https://example.com\n" + strings.Repeat("x", 70*1024)))
	if err == nil {
		t.Fatal("expected scanner error for oversized line")
	}
}
