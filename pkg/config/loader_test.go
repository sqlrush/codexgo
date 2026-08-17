package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoadPrecedence(t *testing.T) {
	home := t.TempDir()
	writeFile(t, ConfigTomlPath(home), `
model = "base-model"
review_model = "base-review"

[tui]
animations = true
theme = "base-theme"
`)
	// config.local.toml overlays config.toml, overriding theme and review_model.
	writeFile(t, ConfigLocalTomlPath(home), `
review_model = "local-review"

[tui]
theme = "local-theme"
`)

	res, err := Load(LoadOptions{
		CodexHome: home,
		CliOverrides: []CliOverride{
			{Path: "model", Value: "cli-model"},
		},
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if res.Config.Model == nil || *res.Config.Model != "cli-model" {
		t.Fatalf("model = %v (CLI should win)", res.Config.Model)
	}
	if res.Config.ReviewModel == nil || *res.Config.ReviewModel != "local-review" {
		t.Fatalf("review_model = %v (config.local should win over config.toml)", res.Config.ReviewModel)
	}
	if res.Config.Tui == nil || res.Config.Tui.Theme == nil || *res.Config.Tui.Theme != "local-theme" {
		t.Fatalf("tui.theme = %v (config.local should win)", res.Config.Tui)
	}
	if !res.Config.Tui.Animations {
		t.Fatalf("tui.animations should be preserved from base config.toml")
	}
}

func TestLoadProfileLayer(t *testing.T) {
	home := t.TempDir()
	writeFile(t, ConfigTomlPath(home), "model = \"base\"\n")
	writeFile(t, profileConfigPath(home, "work"), "model = \"work-model\"\n")

	res, err := Load(LoadOptions{CodexHome: home, Profile: "work"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if res.Config.Model == nil || *res.Config.Model != "work-model" {
		t.Fatalf("model = %v (profile should override base)", res.Config.Model)
	}
}

func TestLoadStrictUnknownField(t *testing.T) {
	home := t.TempDir()
	writeFile(t, ConfigTomlPath(home), "bogus = true\n")

	if _, err := Load(LoadOptions{CodexHome: home, StrictConfig: true}); err == nil {
		t.Fatalf("strict load should fail on unknown field")
	}

	res, err := Load(LoadOptions{CodexHome: home, StrictConfig: false})
	if err != nil {
		t.Fatalf("non-strict load: %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Fatalf("expected a warning for unknown field")
	}
}

func TestLoadMissingFilesIsOK(t *testing.T) {
	home := t.TempDir()
	res, err := Load(LoadOptions{CodexHome: home})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Defaults still apply with no config files present.
	if res.Config.AllowLoginShell == nil || !*res.Config.AllowLoginShell {
		t.Fatalf("defaults not applied: %#v", res.Config.AllowLoginShell)
	}
}

func TestLoadSystemLayer(t *testing.T) {
	home := t.TempDir()
	sysDir := t.TempDir()
	sysPath := filepath.Join(sysDir, "config.toml")
	writeFile(t, sysPath, "model = \"system\"\nreview_model = \"system-review\"\n")
	writeFile(t, ConfigTomlPath(home), "model = \"user\"\n")

	res, err := Load(LoadOptions{CodexHome: home, SystemConfigPath: sysPath})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if res.Config.Model == nil || *res.Config.Model != "user" {
		t.Fatalf("model = %v (user should override system)", res.Config.Model)
	}
	if res.Config.ReviewModel == nil || *res.Config.ReviewModel != "system-review" {
		t.Fatalf("review_model = %v (system value should remain)", res.Config.ReviewModel)
	}
}
