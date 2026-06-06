package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/sqlrush/codexgo/internal/config"
)

// setFeatureEnabledInConfig sets `[features].<feature> = enabled` in
// ${codexHome}/config.toml, creating the file (and CODEXGO_HOME directory) when
// absent. It mirrors ConfigEditsBuilder::set_feature_enabled.
//
// The edit is performed by decoding the existing document into a value tree,
// updating the nested key, and re-encoding. Comments are not preserved (the Go
// config crate does not yet expose a comment-preserving nested-edit builder), but
// the resulting document is byte-for-byte loadable by the config loader.
func setFeatureEnabledInConfig(codexHome, feature string, enabled bool) error {
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		return fmt.Errorf("creating codex home %q: %w", codexHome, err)
	}

	path := config.ConfigTomlPath(codexHome)
	root, err := readConfigDocument(path)
	if err != nil {
		return err
	}

	featuresTable, ok := root["features"].(map[string]any)
	if !ok {
		featuresTable = map[string]any{}
		root["features"] = featuresTable
	}
	featuresTable[feature] = enabled

	encoded, err := toml.Marshal(root)
	if err != nil {
		return fmt.Errorf("encoding config.toml: %w", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return fmt.Errorf("writing %q: %w", path, err)
	}
	return nil
}

// readConfigDocument reads and decodes config.toml into a value-tree map. A
// missing file yields an empty document (not an error), matching the create-on-
// write behavior of the edits builder.
func readConfigDocument(path string) (map[string]any, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("reading %q: %w", path, err)
	}
	var root map[string]any
	if err := toml.Unmarshal(contents, &root); err != nil {
		return nil, fmt.Errorf("parsing %q: %w", path, err)
	}
	if root == nil {
		root = map[string]any{}
	}
	return root, nil
}
