package homedir

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// strPtr returns a pointer to s; used to build the *string argument of
// findCodexHomeFromEnv.
func strPtr(s string) *string { return &s }

func TestFindCodexHomeFromEnv(t *testing.T) {
	// canonRoot is the canonicalized temp dir, since macOS /var -> /private/var
	// and similar symlinks mean the raw temp path differs from its canonical
	// form. All directory-based expectations are derived from this.
	tempRoot := t.TempDir()
	canonRoot, err := filepath.EvalSymlinks(tempRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks(tempRoot): %v", err)
	}

	missing := filepath.Join(tempRoot, "missing-codex-home")

	filePath := filepath.Join(tempRoot, "codex-home.txt")
	if err := os.WriteFile(filePath, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	tests := []struct {
		name string
		env  *string
		// want is the expected resolved path. Empty means "expect an error".
		want string
		// wantErrIs, when non-nil, is matched with errors.Is.
		wantErrIs error
		// wantErrContains, when non-empty, must appear in the error string.
		wantErrContains string
	}{
		{
			name:            "missing path is fatal NotFound",
			env:             strPtr(missing),
			wantErrIs:       ErrNotFound,
			wantErrContains: "CODEXGO_HOME",
		},
		{
			name:            "file path is fatal InvalidInput",
			env:             strPtr(filePath),
			wantErrIs:       ErrInvalidInput,
			wantErrContains: "not a directory",
		},
		{
			name: "valid directory canonicalizes",
			env:  strPtr(tempRoot),
			want: normalizeAbsolute(canonRoot),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := findCodexHomeFromEnv(tt.env)

			if tt.want != "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tt.want {
					t.Fatalf("got %q, want %q", got, tt.want)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error, got nil (result %q)", got)
			}
			if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
				t.Fatalf("error %v is not %v", err, tt.wantErrIs)
			}
			if tt.wantErrContains != "" && !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErrContains)
			}
		})
	}
}

func TestFindCodexHomeFromEnv_DefaultUsesHomeDir(t *testing.T) {
	got, err := findCodexHomeFromEnv(nil)
	if err != nil {
		t.Fatalf("default CODEX_HOME: %v", err)
	}

	home, err := userHomeDir()
	if err != nil {
		t.Fatalf("home dir: %v", err)
	}
	want := normalizeAbsolute(filepath.Join(home, ".codexgo"))
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFindCodexHome_EnvPrecedence(t *testing.T) {
	tempRoot := t.TempDir()
	canonRoot, err := filepath.EvalSymlinks(tempRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks(tempRoot): %v", err)
	}

	t.Setenv(envVar, tempRoot)
	got, err := FindCodexHome()
	if err != nil {
		t.Fatalf("FindCodexHome with env override: %v", err)
	}
	if want := normalizeAbsolute(canonRoot); got != want {
		t.Fatalf("env override: got %q, want %q", got, want)
	}
}

func TestFindCodexHome_EmptyEnvFallsBackToDefault(t *testing.T) {
	// An empty CODEX_HOME must be treated as unset, matching the upstream
	// `.filter(|val| !val.is_empty())`.
	t.Setenv(envVar, "")

	got, err := FindCodexHome()
	if err != nil {
		t.Fatalf("FindCodexHome with empty env: %v", err)
	}

	home, err := userHomeDir()
	if err != nil {
		t.Fatalf("home dir: %v", err)
	}
	want := normalizeAbsolute(filepath.Join(home, ".codexgo"))
	if got != want {
		t.Fatalf("empty env: got %q, want %q", got, want)
	}
}

func TestNormalizeAbsolute(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "already clean", in: "/tmp/codex", want: "/tmp/codex"},
		{name: "current dir component", in: "/tmp/./codex", want: "/tmp/codex"},
		{name: "parent dir component", in: "/tmp/codex/../codex-home", want: "/tmp/codex-home"},
		{name: "redundant separators", in: "/tmp//codex", want: "/tmp/codex"},
		{name: "empty becomes dot", in: "", want: "."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip POSIX-shaped expectations on platforms whose separators or
			// roots differ to keep the package portable; the logic itself is
			// platform-delegated through filepath.Clean.
			if filepath.Separator != '/' && strings.HasPrefix(tt.in, "/") {
				t.Skipf("non-POSIX separator: %c", filepath.Separator)
			}
			if got := normalizeAbsolute(tt.in); got != tt.want {
				t.Fatalf("normalizeAbsolute(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
