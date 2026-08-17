package websearch

import (
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func strptr(s string) *string { return &s }

func TestCommandActionReportsQueriesAndNavigationDetail(t *testing.T) {
	tests := []struct {
		name      string
		arguments string
		want      protocol.WebSearchAction
	}{
		{
			name:      "image queries",
			arguments: `{"image_query":[{"q":"waterfalls"},{"q":"mountains"}]}`,
			want: protocol.WebSearchAction{
				Type:    protocol.WebSearchActionKindSearch,
				Queries: &[]string{"waterfalls", "mountains"},
			},
		},
		{
			name:      "open literal url",
			arguments: `{"open":[{"ref_id":"https://example.com/docs"}]}`,
			want: protocol.WebSearchAction{
				Type: protocol.WebSearchActionKindOpenPage,
				URL:  strptr("https://example.com/docs"),
			},
		},
		{
			name:      "find with literal url",
			arguments: `{"find":[{"ref_id":"https://example.com/docs","pattern":"install"}]}`,
			want: protocol.WebSearchAction{
				Type:    protocol.WebSearchActionKindFindInPage,
				URL:     strptr("https://example.com/docs"),
				Pattern: strptr("install"),
			},
		},
		{
			name:      "find with ref id (no url)",
			arguments: `{"find":[{"ref_id":"turn0search0","pattern":"install"}]}`,
			want: protocol.WebSearchAction{
				Type:    protocol.WebSearchActionKindFindInPage,
				URL:     nil,
				Pattern: strptr("install"),
			},
		},
		{
			name:      "open ref id (not url) -> other",
			arguments: `{"open":[{"ref_id":"turn0search0"}]}`,
			want:      protocol.WebSearchAction{Type: protocol.WebSearchActionKindOther},
		},
		{
			name:      "single search query",
			arguments: `{"search_query":[{"q":"capital of France"}]}`,
			want: protocol.WebSearchAction{
				Type:  protocol.WebSearchActionKindSearch,
				Query: strptr("capital of France"),
			},
		},
		{
			name:      "empty commands -> other",
			arguments: `{}`,
			want:      protocol.WebSearchAction{Type: protocol.WebSearchActionKindOther},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commands, err := ParseCommands(tt.arguments)
			if err != nil {
				t.Fatalf("ParseCommands: %v", err)
			}
			got := CommandAction(commands)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseCommandsEmptyArgumentsYieldsDefault(t *testing.T) {
	commands, err := ParseCommands("   ")
	if err != nil {
		t.Fatalf("ParseCommands: %v", err)
	}
	if !reflect.DeepEqual(commands, SearchCommands{}) {
		t.Errorf("got %#v, want zero value", commands)
	}
}

func TestToolSpecUsesWebNamespace(t *testing.T) {
	spec, err := ToolSpec()
	if err != nil {
		t.Fatalf("ToolSpec: %v", err)
	}
	if spec.Namespace == nil || spec.Namespace.Name != Namespace {
		t.Fatalf("namespace mismatch: %#v", spec.Namespace)
	}
	if len(spec.Namespace.Tools) != 1 || spec.Namespace.Tools[0].Function.Name != RunToolName {
		t.Errorf("tool name mismatch: %#v", spec.Namespace.Tools)
	}
	if spec.Namespace.Tools[0].Function.Strict {
		t.Errorf("strict should be false")
	}
}

func TestIsAbsoluteURL(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"https://example.com/docs", true},
		{"http://x", true},
		{"turn0search0", false},
		{"", false},
		{":nohost", false},
		{"1abc://x", false},
		{"mailto:a@b.com", true},
	}
	for _, tt := range tests {
		if got := isAbsoluteURL(tt.in); got != tt.want {
			t.Errorf("isAbsoluteURL(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
