package sandbox

import (
	"reflect"
	"testing"
)

func TestLexicalPathKey(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{`C:\Users\Foo\`, "c:/users/foo"},
		{`C:/Users/Foo`, "c:/users/foo"},
		{`C:\Users\Foo\\`, "c:/users/foo"},
		{`/etc/secret/`, "/etc/secret"},
		{``, ""},
	}
	for _, tt := range tests {
		if got := lexicalPathKey(tt.in); got != tt.want {
			t.Fatalf("lexicalPathKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPlanDenyReadACLPaths(t *testing.T) {
	existing := map[string]bool{
		`C:\work\secret.env`: true,
	}
	canonical := map[string]string{
		`C:\work\secret.env`: `C:\real\secret.env`,
	}
	exists := func(p string) bool { return existing[p] }
	canon := func(p string) string {
		if c, ok := canonical[p]; ok {
			return c
		}
		return p
	}

	tests := []struct {
		name  string
		paths []string
		want  []string
	}{
		{
			name:  "missing path preserved",
			paths: []string{`C:\work\future-secret.env`},
			want:  []string{`C:\work\future-secret.env`},
		},
		{
			name:  "existing path adds canonical target",
			paths: []string{`C:\work\secret.env`},
			want:  []string{`C:\work\secret.env`, `C:\real\secret.env`},
		},
		{
			name:  "duplicate spellings collapse",
			paths: []string{`C:\work\future-secret.env`, `C:/work/future-secret.env/`},
			want:  []string{`C:\work\future-secret.env`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := planDenyReadACLPaths(tt.paths, exists, canon)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("planDenyReadACLPaths = %v, want %v", got, tt.want)
			}
		})
	}
}
