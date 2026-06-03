package gitutils

import "testing"

func TestCanonicalizeGitRemoteURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		// GitHub variants all normalize to the same lowercased form.
		{"scp github", "git@github.com:OpenAI/Codex.git", "github.com/openai/codex", true},
		{"ssh github", "ssh://git@github.com/openai/codex.git", "github.com/openai/codex", true},
		{"ssh github default port", "ssh://git@github.com:22/OpenAI/Codex.git", "github.com/openai/codex", true},
		{"https github", "https://github.com/openai/codex.git", "github.com/openai/codex", true},
		{"https github default port", "https://github.com:443/openai/codex.git", "github.com/openai/codex", true},
		{"https token trailing slash", "https://token@github.com/openai/codex/", "github.com/openai/codex", true},
		{"bare github", "github.com/OpenAI/Codex.git", "github.com/openai/codex", true},

		// GHE preserves path casing; non-default ports are retained.
		{"ghe scp", "git@ghe.company.com:Org/Repo.git", "ghe.company.com/Org/Repo", true},
		{"ghe ssh nondefault port", "ssh://git@ghe.company.com:2222/Org/Repo.git", "ghe.company.com:2222/Org/Repo", true},

		// Rejected values.
		{"empty", "", "", false},
		{"file scheme", "file:///tmp/repo", "", false},
		{"owner only", "github.com/openai", "", false},
		{"abs path", "/tmp/repo", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := CanonicalizeGitRemoteURL(tc.input)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (got %q)", ok, tc.ok, got)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
