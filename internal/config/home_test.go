package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindCodexHomeFromEnv(t *testing.T) {
	dir := t.TempDir()
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}

	file := filepath.Join(dir, "afile.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tests := []struct {
		name      string
		env       string
		wantErr   string
		wantValue string
	}{
		{name: "valid dir canonicalizes", env: dir, wantValue: resolvedDir},
		{name: "missing path is fatal", env: filepath.Join(dir, "nope"), wantErr: "does not exist"},
		{name: "file path is fatal", env: file, wantErr: "not a directory"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := findCodexHomeFromEnv(tt.env)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantValue {
				t.Fatalf("got %q, want %q", got, tt.wantValue)
			}
		})
	}
}

func TestFindCodexHomeDefault(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	got, err := findCodexHomeFromEnv("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(home, DefaultCodexDirName)
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestConfigPathHelpers(t *testing.T) {
	home := "/codexhome"
	if got := ConfigTomlPath(home); got != filepath.Join(home, "config.toml") {
		t.Fatalf("ConfigTomlPath = %q", got)
	}
	if got := ConfigLocalTomlPath(home); got != filepath.Join(home, "config.local.toml") {
		t.Fatalf("ConfigLocalTomlPath = %q", got)
	}
	if got := profileConfigPath(home, "work"); got != filepath.Join(home, "work.config.toml") {
		t.Fatalf("profileConfigPath = %q", got)
	}
}
