package pluginutil

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// abs is a test helper that converts an absolute filesystem path into an
// AbsolutePathBuf, failing the test on error.
func abs(t *testing.T, path string) abspath.AbsolutePathBuf {
	t.Helper()
	p, err := abspath.FromAbsolutePath(path)
	if err != nil {
		t.Fatalf("FromAbsolutePath(%q): %v", path, err)
	}
	return p
}

func TestPluginNamespaceForSkillPath(t *testing.T) {
	t.Parallel()

	const codexManifestRel = ".codex-plugin/plugin.json"
	const claudeManifestRel = ".claude-plugin/plugin.json"

	tests := []struct {
		name string
		// manifestRel is the relative manifest path to write under the plugin root.
		manifestRel string
		// manifestJSON is the manifest file contents.
		manifestJSON string
		// pluginDir is the plugin root directory name (final component matters for
		// the blank-name fallback).
		pluginDir string
		want      string
	}{
		{
			name:         "uses manifest name from codex manifest",
			manifestRel:  codexManifestRel,
			manifestJSON: `{"name":"sample"}`,
			pluginDir:    "sample",
			want:         "sample",
		},
		{
			name:         "uses manifest name from alternate claude manifest",
			manifestRel:  claudeManifestRel,
			manifestJSON: `{"name":"sample"}`,
			pluginDir:    "sample",
			want:         "sample",
		},
		{
			name:         "blank name falls back to plugin dir name",
			manifestRel:  codexManifestRel,
			manifestJSON: `{"name":"   "}`,
			pluginDir:    "fallback-dir",
			want:         "fallback-dir",
		},
		{
			name:         "missing name field falls back to plugin dir name",
			manifestRel:  codexManifestRel,
			manifestJSON: `{}`,
			pluginDir:    "another-dir",
			want:         "another-dir",
		},
		{
			name:         "non-blank name is used verbatim including surrounding spaces",
			manifestRel:  codexManifestRel,
			manifestJSON: `{"name":" spaced "}`,
			pluginDir:    "ignored-dir",
			want:         " spaced ",
		},
		{
			name:         "unknown fields are ignored",
			manifestRel:  codexManifestRel,
			manifestJSON: `{"name":"sample","version":"1.0.0","extra":true}`,
			pluginDir:    "sample",
			want:         "sample",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmp := t.TempDir()
			pluginRoot := filepath.Join(tmp, "plugins", tt.pluginDir)
			skillPath := filepath.Join(pluginRoot, "skills", "search", "SKILL.md")

			if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
				t.Fatalf("mkdir skill dir: %v", err)
			}
			manifestPath := filepath.Join(pluginRoot, filepath.FromSlash(tt.manifestRel))
			if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
				t.Fatalf("mkdir manifest dir: %v", err)
			}
			if err := os.WriteFile(manifestPath, []byte(tt.manifestJSON), 0o644); err != nil {
				t.Fatalf("write manifest: %v", err)
			}
			if err := os.WriteFile(skillPath, []byte("---\ndescription: search\n---\n"), 0o644); err != nil {
				t.Fatalf("write skill: %v", err)
			}

			got, ok := PluginNamespaceForSkillPath(context.Background(), LocalFS{}, abs(t, skillPath))
			if !ok {
				t.Fatalf("PluginNamespaceForSkillPath returned ok=false, want %q", tt.want)
			}
			if got != tt.want {
				t.Fatalf("PluginNamespaceForSkillPath = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPluginNamespaceForSkillPathNoManifest(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	skillPath := filepath.Join(tmp, "no-plugin", "skills", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(skillPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	if _, ok := PluginNamespaceForSkillPath(context.Background(), LocalFS{}, abs(t, skillPath)); ok {
		t.Fatalf("expected ok=false when no ancestor has a manifest")
	}
}

func TestPluginNamespaceForSkillPathInvalidJSONIsSkipped(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	pluginRoot := filepath.Join(tmp, "plugins", "broken")
	skillPath := filepath.Join(pluginRoot, "skills", "SKILL.md")
	manifestPath := filepath.Join(pluginRoot, ".codex-plugin", "plugin.json")

	for _, dir := range []string{filepath.Dir(skillPath), filepath.Dir(manifestPath)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", dir, err)
		}
	}
	if err := os.WriteFile(manifestPath, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(skillPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	if _, ok := PluginNamespaceForSkillPath(context.Background(), LocalFS{}, abs(t, skillPath)); ok {
		t.Fatalf("expected ok=false when manifest JSON is invalid")
	}
}

func TestFindPluginManifestPath(t *testing.T) {
	t.Parallel()

	t.Run("finds claude manifest", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		pluginRoot := filepath.Join(tmp, "plugins", "sample")
		manifestPath := filepath.Join(pluginRoot, ".claude-plugin", "plugin.json")
		if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(manifestPath, []byte(`{"name":"sample"}`), 0o644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		got, ok := FindPluginManifestPath(pluginRoot)
		if !ok {
			t.Fatalf("FindPluginManifestPath returned ok=false")
		}
		if got != manifestPath {
			t.Fatalf("FindPluginManifestPath = %q, want %q", got, manifestPath)
		}
	})

	t.Run("prefers codex manifest over claude manifest", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		pluginRoot := filepath.Join(tmp, "plugins", "sample")
		codexManifest := filepath.Join(pluginRoot, ".codex-plugin", "plugin.json")
		claudeManifest := filepath.Join(pluginRoot, ".claude-plugin", "plugin.json")
		for _, m := range []string{codexManifest, claudeManifest} {
			if err := os.MkdirAll(filepath.Dir(m), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(m, []byte(`{"name":"sample"}`), 0o644); err != nil {
				t.Fatalf("write manifest: %v", err)
			}
		}

		got, ok := FindPluginManifestPath(pluginRoot)
		if !ok {
			t.Fatalf("FindPluginManifestPath returned ok=false")
		}
		if got != codexManifest {
			t.Fatalf("FindPluginManifestPath = %q, want %q (codex preferred)", got, codexManifest)
		}
	})

	t.Run("none found", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		if _, ok := FindPluginManifestPath(tmp); ok {
			t.Fatalf("expected ok=false when no manifest exists")
		}
	})
}
