package multiagent

import (
	"errors"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func TestResolveAgentPath(t *testing.T) {
	tests := []struct {
		name      string
		base      string
		reference string
		want      string
		wantErr   bool
	}{
		{name: "absolute root", base: "/root/researcher", reference: "/root", want: "/root"},
		{name: "absolute child", base: "/root/researcher", reference: "/root/other", want: "/root/other"},
		{name: "relative single", base: "/root/researcher", reference: "child", want: "/root/researcher/child"},
		{name: "relative nested", base: "/root", reference: "a/b", want: "/root/a/b"},
		{name: "empty reference errors", base: "/root", reference: "", wantErr: true},
		{name: "trailing slash errors", base: "/root", reference: "child/", wantErr: true},
		{name: "invalid absolute errors", base: "/root", reference: "/nope", wantErr: true},
		{name: "invalid relative segment errors", base: "/root", reference: "Bad-Name", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := mustPath(t, tt.base)
			got, err := resolveAgentPath(base, tt.reference)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got.String())
				}
				if !errors.Is(err, ErrUnsupportedOperation) {
					t.Fatalf("error = %v, want ErrUnsupportedOperation", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.String() != tt.want {
				t.Fatalf("resolve = %q, want %q", got.String(), tt.want)
			}
		})
	}
}

func TestAgentMatchesPrefix(t *testing.T) {
	mk := func(s string) *protocol.AgentPath {
		p := mustPath(t, s)
		return &p
	}
	tests := []struct {
		name   string
		path   *protocol.AgentPath
		prefix string
		want   bool
	}{
		{name: "root prefix matches everything", path: mk("/root/a"), prefix: "/root", want: true},
		{name: "root prefix matches nil path", path: nil, prefix: "/root", want: true},
		{name: "exact match", path: mk("/root/a"), prefix: "/root/a", want: true},
		{name: "child under prefix", path: mk("/root/a/b"), prefix: "/root/a", want: true},
		{name: "sibling not under prefix", path: mk("/root/ab"), prefix: "/root/a", want: false},
		{name: "nil path with non-root prefix", path: nil, prefix: "/root/a", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix := mustPath(t, tt.prefix)
			if got := agentMatchesPrefix(tt.path, prefix); got != tt.want {
				t.Fatalf("agentMatchesPrefix = %v, want %v", got, tt.want)
			}
		})
	}
}
