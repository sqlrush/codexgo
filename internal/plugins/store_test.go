package plugins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

func mustAbs(t *testing.T, path string) abspath.AbsolutePathBuf {
	t.Helper()
	resolved, err := abspath.FromAbsolutePathChecked(path)
	if err != nil {
		t.Fatalf("abs %q: %v", path, err)
	}
	return resolved
}

func writeSourcePlugin(t *testing.T, root, name, version string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".codex-plugin"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	versionLine := ""
	if version != "" {
		versionLine = `, "version": "` + version + `"`
	}
	contents := `{ "name": "` + name + `"` + versionLine + ` }`
	if err := os.WriteFile(filepath.Join(root, ".codex-plugin", "plugin.json"), []byte(contents), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestPluginStorePaths(t *testing.T) {
	home := t.TempDir()
	store, err := NewPluginStore(home)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	id, _ := NewPluginID("github", "openai-curated")
	if got := store.PluginBaseRoot(id).Path(); got != filepath.Join(home, "plugins/cache", "openai-curated", "github") {
		t.Fatalf("base root got %q", got)
	}
	if got := store.PluginRoot(id, "1.0.0").Path(); got != filepath.Join(home, "plugins/cache", "openai-curated", "github", "1.0.0") {
		t.Fatalf("plugin root got %q", got)
	}
	if got := store.PluginDataRoot(id).Path(); got != filepath.Join(home, "plugins/data", "github-openai-curated") {
		t.Fatalf("data root got %q", got)
	}
}

func TestPluginStoreInstallAndActiveVersion(t *testing.T) {
	home := t.TempDir()
	store, err := NewPluginStore(home)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	id, _ := NewPluginID("github", "test")

	src := filepath.Join(t.TempDir(), "src")
	writeSourcePlugin(t, src, "github", "1.2.3")

	result, err := store.Install(mustAbs(t, src), id)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if result.PluginVersion != "1.2.3" {
		t.Fatalf("version got %q", result.PluginVersion)
	}
	if !store.IsInstalled(id) {
		t.Fatal("expected installed")
	}
	if v, ok := store.ActivePluginVersion(id); !ok || v != "1.2.3" {
		t.Fatalf("active version got %q ok=%v", v, ok)
	}
	if !isFile(filepath.Join(result.InstalledPath.Path(), ".codex-plugin", "plugin.json")) {
		t.Fatal("expected installed manifest")
	}
}

func TestPluginStoreUpgradeReplacesActiveVersion(t *testing.T) {
	home := t.TempDir()
	store, _ := NewPluginStore(home)
	id, _ := NewPluginID("github", "test")

	src1 := filepath.Join(t.TempDir(), "v1")
	writeSourcePlugin(t, src1, "github", "1.0.0")
	if _, err := store.Install(mustAbs(t, src1), id); err != nil {
		t.Fatalf("install v1: %v", err)
	}

	src2 := filepath.Join(t.TempDir(), "v2")
	writeSourcePlugin(t, src2, "github", "2.0.0")
	if _, err := store.Install(mustAbs(t, src2), id); err != nil {
		t.Fatalf("install v2: %v", err)
	}

	// After upgrade, the old version should be pruned and the new one active.
	if v, ok := store.ActivePluginVersion(id); !ok || v != "2.0.0" {
		t.Fatalf("active version got %q ok=%v", v, ok)
	}
	if pathExists(store.PluginRoot(id, "1.0.0").Path()) {
		t.Fatal("expected old version pruned")
	}
}

func TestPluginStoreLocalVersionWins(t *testing.T) {
	// When several valid version directories coexist on disk, the special
	// "local" version always wins regardless of semver ordering. We materialize
	// the version directories directly to exercise ActivePluginVersion's
	// selection rather than the pruning install path.
	home := t.TempDir()
	store, _ := NewPluginStore(home)
	id, _ := NewPluginID("github", "test")

	base := store.PluginBaseRoot(id).Path()
	for _, v := range []string{"9.9.9", DefaultPluginVersion, "1.0.0"} {
		if err := os.MkdirAll(filepath.Join(base, v), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", v, err)
		}
	}

	if v, ok := store.ActivePluginVersion(id); !ok || v != DefaultPluginVersion {
		t.Fatalf("expected local to win, got %q ok=%v", v, ok)
	}
}

func TestPluginStoreHighestSemverWinsWithoutLocal(t *testing.T) {
	home := t.TempDir()
	store, _ := NewPluginStore(home)
	id, _ := NewPluginID("github", "test")
	base := store.PluginBaseRoot(id).Path()
	for _, v := range []string{"1.0.0", "2.5.0", "2.10.0", "0.1.0"} {
		if err := os.MkdirAll(filepath.Join(base, v), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", v, err)
		}
	}
	if v, ok := store.ActivePluginVersion(id); !ok || v != "2.10.0" {
		t.Fatalf("expected 2.10.0 to win, got %q ok=%v", v, ok)
	}
}

func TestPluginStoreInstallRejectsNameMismatch(t *testing.T) {
	home := t.TempDir()
	store, _ := NewPluginStore(home)
	id, _ := NewPluginID("github", "test")

	src := filepath.Join(t.TempDir(), "src")
	writeSourcePlugin(t, src, "other", "1.0.0")
	_, err := store.Install(mustAbs(t, src), id)
	if err == nil {
		t.Fatal("expected name mismatch error")
	}
	want := "plugin.json name `other` does not match marketplace plugin name `github`"
	if err.Error() != want {
		t.Fatalf("got %q, want %q", err.Error(), want)
	}
}

func TestPluginStoreUninstall(t *testing.T) {
	home := t.TempDir()
	store, _ := NewPluginStore(home)
	id, _ := NewPluginID("github", "test")
	src := filepath.Join(t.TempDir(), "src")
	writeSourcePlugin(t, src, "github", "1.0.0")
	if _, err := store.Install(mustAbs(t, src), id); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := store.Uninstall(id); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if store.IsInstalled(id) {
		t.Fatal("expected not installed after uninstall")
	}
}

func TestValidatePluginVersionSegment(t *testing.T) {
	tests := []struct {
		version string
		wantErr bool
	}{
		{"1.2.3", false},
		{"1.2.3-beta+7", false},
		{"local", false},
		{"", true},
		{".", true},
		{"..", true},
		{"a/b", true},
		{"a b", true},
	}
	for _, tt := range tests {
		err := validatePluginVersionSegment(tt.version)
		if (err != nil) != tt.wantErr {
			t.Errorf("version %q: err=%v wantErr=%v", tt.version, err, tt.wantErr)
		}
	}
}

func TestPluginVersionForSourceDefaultsToLocal(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	writeSourcePlugin(t, src, "github", "")
	v, err := PluginVersionForSource(src)
	if err != nil {
		t.Fatalf("version for source: %v", err)
	}
	if v != DefaultPluginVersion {
		t.Fatalf("got %q", v)
	}
}
