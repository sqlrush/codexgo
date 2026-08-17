package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/pkg/modelproviderinfo"
)

// TestPersistModelSelection verifies the config write path: a fresh home gets
// a config.toml with the model key; an existing config keeps its other keys.
func TestPersistModelSelection(t *testing.T) {
	home := t.TempDir()

	if err := persistModelSelection(home, "glm-5.1"); err != nil {
		t.Fatalf("persist into fresh home: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), `model = 'glm-5.1'`) && !strings.Contains(string(data), `model = "glm-5.1"`) {
		t.Fatalf("config missing model key:\n%s", data)
	}

	// Existing keys survive a model update.
	seed := "model = \"glm-5.1\"\ncheck_for_update_on_startup = false\n\n[model_providers.glm]\nname = \"GLM\"\nwire_api = \"chat\"\nmodels = [\"glm-5.1\"]\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	if err := persistModelSelection(home, "deepseek-v4-pro"); err != nil {
		t.Fatalf("persist over existing config: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatalf("re-read config: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "deepseek-v4-pro") {
		t.Errorf("model not updated:\n%s", text)
	}
	for _, keep := range []string{"check_for_update_on_startup", "model_providers", "glm-5.1"} {
		if !strings.Contains(text, keep) {
			t.Errorf("existing key %q lost:\n%s", keep, text)
		}
	}

	if err := persistModelSelection("", "x"); err == nil {
		t.Errorf("expected error for empty codex home")
	}
}

// TestBuildModelPickerEntries verifies the list combines the bundled catalog's
// picker-visible presets with configured provider models.
func TestBuildModelPickerEntries(t *testing.T) {
	cfg := loadedConfig{
		ModelProviders: map[string]modelproviderinfo.ModelProviderInfo{
			"glm":      {Name: "GLM (Zhipu AI)", Models: []string{"glm-5.1"}},
			"deepseek": {Name: "DeepSeek", Models: []string{"deepseek-v4-pro"}},
			"nolist":   {Name: "No Models"},
		},
	}
	entries := buildModelPickerEntries(cfg, true)

	slugs := map[string]string{}
	for _, e := range entries {
		slugs[e.Slug] = e.DisplayName
	}
	// Bundled picker-visible presets present (gpt-5.5 leads the catalog).
	if _, ok := slugs["gpt-5.5"]; !ok {
		t.Errorf("bundled gpt-5.5 missing: %v", slugs)
	}
	// codex-auto-review is hidden from the picker.
	if _, ok := slugs["codex-auto-review"]; ok {
		t.Errorf("hidden codex-auto-review leaked into picker")
	}
	// Custom provider models present with their provider display names.
	if got := slugs["glm-5.1"]; got != "GLM (Zhipu AI)" {
		t.Errorf("glm-5.1 display = %q", got)
	}
	if got := slugs["deepseek-v4-pro"]; got != "DeepSeek" {
		t.Errorf("deepseek-v4-pro display = %q", got)
	}
	// The first entry is the catalog leader.
	if len(entries) == 0 || entries[0].Slug != "gpt-5.5" {
		t.Errorf("first entry = %+v, want gpt-5.5", entries[0])
	}
}
