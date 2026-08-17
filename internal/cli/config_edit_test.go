package cli

import (
	"os"
	"testing"

	"github.com/sqlrush/codexgo/pkg/config"
)

func TestSetFeatureEnabledInConfig(t *testing.T) {
	home := t.TempDir()

	if err := setFeatureEnabledInConfig(home, "shell_tool", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	assertFeatureValue(t, home, "shell_tool", false)

	// Toggling again to true round-trips through the document.
	if err := setFeatureEnabledInConfig(home, "shell_tool", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	assertFeatureValue(t, home, "shell_tool", true)

	// A second feature is added without clobbering the first.
	if err := setFeatureEnabledInConfig(home, "apps", false); err != nil {
		t.Fatalf("second feature: %v", err)
	}
	assertFeatureValue(t, home, "shell_tool", true)
	assertFeatureValue(t, home, "apps", false)
}

func TestSetFeatureEnabledPreservesExistingTopLevelKeys(t *testing.T) {
	home := t.TempDir()
	path := config.ConfigTomlPath(home)
	if err := os.WriteFile(path, []byte("model = \"gpt-5\"\n"), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	if err := setFeatureEnabledInConfig(home, "shell_tool", true); err != nil {
		t.Fatalf("enable: %v", err)
	}

	result, err := config.Load(config.LoadOptions{CodexHome: home})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if result.Config.Model == nil || *result.Config.Model != "gpt-5" {
		t.Errorf("model not preserved: %+v", result.Config.Model)
	}
}

func assertFeatureValue(t *testing.T, home, feature string, want bool) {
	t.Helper()
	result, err := config.Load(config.LoadOptions{CodexHome: home})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if result.Config.Features == nil {
		t.Fatalf("features table is nil")
	}
	entries := result.Config.Features.Entries()
	got, ok := entries[feature]
	if !ok {
		t.Fatalf("feature %q not present in entries %v", feature, entries)
	}
	if got != want {
		t.Errorf("feature %q = %v, want %v", feature, got, want)
	}
}
