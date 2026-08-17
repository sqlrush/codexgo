package cli

// Host-side wiring for the TUI /model picker: the entry list (bundled OpenAI
// catalog + custom-provider models from config.toml) and the persistence
// callback that writes the selection back into config.toml.

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/pelletier/go-toml/v2"

	"github.com/sqlrush/codexgo/internal/tui"
	"github.com/sqlrush/codexgo/pkg/config"
	"github.com/sqlrush/codexgo/pkg/modelsmanager"
)

// buildModelPickerEntries assembles the /model picker list: the bundled
// catalog's picker-visible presets first (in priority order), then each
// configured provider's declared models (the codexgo routing extension),
// sorted by provider id for a stable order.
func buildModelPickerEntries(cfg loadedConfig, haveCfg bool) []tui.ModelPickerEntry {
	var entries []tui.ModelPickerEntry

	if resp, err := modelsmanager.BundledModelsResponse(); err == nil {
		mgr := modelsmanager.NewStaticModelsManager(nil, resp)
		for _, preset := range mgr.ListModels(context.Background(), modelsmanager.RefreshOffline) {
			if !preset.ShowInPicker {
				continue
			}
			entries = append(entries, tui.ModelPickerEntry{
				Slug:        preset.Model,
				DisplayName: preset.DisplayName,
				Description: preset.Description,
			})
		}
	}

	if haveCfg {
		ids := make([]string, 0, len(cfg.ModelProviders))
		for id := range cfg.ModelProviders {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			info := cfg.ModelProviders[id]
			for _, slug := range info.Models {
				if slug == "" {
					continue
				}
				entries = append(entries, tui.ModelPickerEntry{
					Slug:        slug,
					DisplayName: info.Name,
				})
			}
		}
	}

	return entries
}

// persistModelSelection writes `model = "<slug>"` into config.toml (creating
// the file when missing), so the picked model becomes the default for future
// sessions. It follows the value-tree read-modify-write pattern of
// setFeatureEnabledInConfig.
func persistModelSelection(codexHome, slug string) error {
	if codexHome == "" || slug == "" {
		return fmt.Errorf("cli: persist model: missing codex home or slug")
	}
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		return fmt.Errorf("creating codex home %q: %w", codexHome, err)
	}

	path := config.ConfigTomlPath(codexHome)
	root, err := readConfigDocument(path)
	if err != nil {
		return err
	}
	root["model"] = slug

	encoded, err := toml.Marshal(root)
	if err != nil {
		return fmt.Errorf("encoding config.toml: %w", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return fmt.Errorf("writing %q: %w", path, err)
	}
	return nil
}
