package shellcmd

import "testing"

func TestExtractApplyPatchHeredoc(t *testing.T) {
	t.Parallel()
	const patch = "*** Begin Patch\n*** Add File: foo\n+hi\n*** End Patch"

	tests := []struct {
		name        string
		argv        []string
		wantOK      bool
		wantBody    string
		wantWorkdir string
	}{
		{
			name:     "direct heredoc",
			argv:     []string{"/bin/zsh", "-lc", "apply_patch <<'EOF'\n" + patch + "\nEOF\n"},
			wantOK:   true,
			wantBody: patch,
		},
		{
			name:     "applypatch alias",
			argv:     []string{"/bin/bash", "-lc", "applypatch <<'EOF'\n" + patch + "\nEOF"},
			wantOK:   true,
			wantBody: patch,
		},
		{
			name:        "cd then apply_patch",
			argv:        []string{"/bin/zsh", "-lc", "cd sub && apply_patch <<'EOF'\n" + patch + "\nEOF"},
			wantOK:      true,
			wantBody:    patch,
			wantWorkdir: "sub",
		},
		{
			name:        "cd quoted path with spaces",
			argv:        []string{"/bin/zsh", "-lc", "cd 'a b' && apply_patch <<'EOF'\n" + patch + "\nEOF"},
			wantOK:      true,
			wantBody:    patch,
			wantWorkdir: "a b",
		},
		{
			name:   "non-login shell flag",
			argv:   []string{"/bin/sh", "-c", "apply_patch <<'EOF'\n" + patch + "\nEOF"},
			wantOK: true, wantBody: patch,
		},
		{
			name:   "plain echo is not apply_patch",
			argv:   []string{"/bin/zsh", "-lc", "echo hi"},
			wantOK: false,
		},
		{
			name:   "apply_patch with an extra arg does not match",
			argv:   []string{"/bin/zsh", "-lc", "apply_patch foo <<'EOF'\n" + patch + "\nEOF"},
			wantOK: false,
		},
		{
			name:   "cd with semicolon does not match",
			argv:   []string{"/bin/zsh", "-lc", "cd foo; apply_patch <<'EOF'\n" + patch + "\nEOF"},
			wantOK: false,
		},
		{
			name:   "cd or apply_patch does not match",
			argv:   []string{"/bin/zsh", "-lc", "cd foo || apply_patch <<'EOF'\n" + patch + "\nEOF"},
			wantOK: false,
		},
		{
			name:   "trailing command does not match",
			argv:   []string{"/bin/zsh", "-lc", "apply_patch <<'EOF'\n" + patch + "\nEOF\n && echo done"},
			wantOK: false,
		},
		{
			name:   "not a shell invocation",
			argv:   []string{"apply_patch", patch},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ExtractApplyPatchHeredoc(tt.argv)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (got %+v)", ok, tt.wantOK, got)
			}
			if !tt.wantOK {
				return
			}
			if got.Body != tt.wantBody {
				t.Errorf("body = %q, want %q", got.Body, tt.wantBody)
			}
			if got.Workdir != tt.wantWorkdir {
				t.Errorf("workdir = %q, want %q", got.Workdir, tt.wantWorkdir)
			}
		})
	}
}
