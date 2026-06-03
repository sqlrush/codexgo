package collabtemplates

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTemplatesMatchEmbeddedFiles confirms each exported template is the exact
// byte content of its embedded markdown file. This guards against accidental
// drift between the embed directive and the on-disk template.
func TestTemplatesMatchEmbeddedFiles(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		embedded string
	}{
		{"plan", "templates/plan.md", Plan},
		{"default", "templates/default.md", Default},
		{"execute", "templates/execute.md", Execute},
		{"pair_programming", "templates/pair_programming.md", PairProgramming},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Clean(tt.file))
			if err != nil {
				t.Fatalf("read %s: %v", tt.file, err)
			}
			if tt.embedded != string(want) {
				t.Errorf("%s: embedded content does not match file", tt.name)
			}
			if len(tt.embedded) == 0 {
				t.Errorf("%s: embedded content is empty", tt.name)
			}
		})
	}
}

// TestTemplatesAreDistinct verifies the four templates are not accidentally
// pointing at the same embedded file.
func TestTemplatesAreDistinct(t *testing.T) {
	all := map[string]string{
		"plan":             Plan,
		"default":          Default,
		"execute":          Execute,
		"pair_programming": PairProgramming,
	}
	seen := make(map[string]string, len(all))
	for name, content := range all {
		if prev, ok := seen[content]; ok {
			t.Errorf("templates %q and %q have identical content", prev, name)
		}
		seen[content] = name
	}
}
