package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// LayerSource identifies the origin of a config layer, ordered low-to-high in
// precedence (later sources override earlier ones).
type LayerSource int

const (
	// LayerSystem is /etc/codex/config.toml (Unix) style system config.
	LayerSystem LayerSource = iota
	// LayerUser is ${CODEXGO_HOME}/config.toml.
	LayerUser
	// LayerUserProfile is ${CODEXGO_HOME}/<name>.config.toml (profile-v2).
	LayerUserProfile
	// LayerConfigLocal is ${CODEXGO_HOME}/config.local.toml overlay.
	LayerConfigLocal
	// LayerEnv is config values derived from environment variables.
	LayerEnv
	// LayerSessionFlags is CLI `-c key=value` overrides.
	LayerSessionFlags
)

// ConfigLayer is a single named config layer with its parsed TOML value tree.
type ConfigLayer struct {
	Source LayerSource
	// Path is the originating file path, when the layer came from a file.
	Path string
	// Value is the parsed TOML value tree for this layer.
	Value TomlValue
}

// LoadOptions controls config loading behavior.
type LoadOptions struct {
	// CodexHome is the resolved configuration directory. When empty it is
	// resolved via FindCodexHome.
	CodexHome string
	// Profile selects a profile-v2 layer (${CODEXGO_HOME}/<profile>.config.toml).
	Profile string
	// SystemConfigPath overrides the system config.toml path (test hook).
	SystemConfigPath string
	// CliOverrides are `-c key=value` dotted-path overrides.
	CliOverrides []CliOverride
	// EnvOverrides is an optional TOML value tree from environment variables.
	EnvOverrides TomlValue
	// StrictConfig fails on unknown fields; otherwise they are collected as
	// warnings.
	StrictConfig bool
	// IgnoreUserConfig skips loading the user config.toml layer.
	IgnoreUserConfig bool
}

// LoadResult is the outcome of loading and merging the config layers.
type LoadResult struct {
	// CodexHome is the resolved configuration directory.
	CodexHome string
	// Config is the resolved, typed configuration.
	Config ConfigToml
	// Merged is the merged TOML value tree across all layers.
	Merged TomlValue
	// Layers are the contributing layers ordered low-to-high in precedence.
	Layers []ConfigLayer
	// Warnings holds non-fatal messages (e.g. unknown fields in warn mode).
	Warnings []string
}

// Load resolves CODEXGO_HOME, reads and parses each config layer, merges them in
// precedence order (CLI > env > config.local > profile > config.toml > system),
// and decodes the merged result into a typed ConfigToml.
func Load(opts LoadOptions) (LoadResult, error) {
	codexHome := opts.CodexHome
	if codexHome == "" {
		resolved, err := FindCodexHome()
		if err != nil {
			return LoadResult{}, err
		}
		codexHome = resolved
	}

	var layers []ConfigLayer
	var warnings []string

	addLayer := func(source LayerSource, path string) error {
		value, ok, err := readTomlFile(path)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if err := collectStrict(value, opts.StrictConfig, path, &warnings); err != nil {
			return err
		}
		layers = append(layers, ConfigLayer{Source: source, Path: path, Value: value})
		return nil
	}

	if opts.SystemConfigPath != "" {
		if err := addLayer(LayerSystem, opts.SystemConfigPath); err != nil {
			return LoadResult{}, err
		}
	}

	if !opts.IgnoreUserConfig {
		if err := addLayer(LayerUser, ConfigTomlPath(codexHome)); err != nil {
			return LoadResult{}, err
		}
	}

	if opts.Profile != "" {
		profilePath := profileConfigPath(codexHome, opts.Profile)
		if err := addLayer(LayerUserProfile, profilePath); err != nil {
			return LoadResult{}, err
		}
	}

	if err := addLayer(LayerConfigLocal, ConfigLocalTomlPath(codexHome)); err != nil {
		return LoadResult{}, err
	}

	if opts.EnvOverrides != nil {
		layers = append(layers, ConfigLayer{Source: LayerEnv, Value: opts.EnvOverrides})
	}

	if len(opts.CliOverrides) > 0 {
		cliLayer := BuildCliOverridesLayer(opts.CliOverrides)
		if err := collectStrict(cliLayer, opts.StrictConfig, "-c overrides", &warnings); err != nil {
			return LoadResult{}, err
		}
		layers = append(layers, ConfigLayer{Source: LayerSessionFlags, Value: cliLayer})
	}

	merged := TomlValue(map[string]any{})
	for _, layer := range layers {
		MergeTomlValues(&merged, layer.Value)
	}

	cfg, err := DecodeConfigToml(merged)
	if err != nil {
		return LoadResult{}, err
	}

	return LoadResult{
		CodexHome: codexHome,
		Config:    cfg,
		Merged:    merged,
		Layers:    layers,
		Warnings:  warnings,
	}, nil
}

func collectStrict(value TomlValue, strict bool, source string, warnings *[]string) error {
	field := UnknownConfigField(value)
	if field == "" {
		return nil
	}
	if strict {
		return fmt.Errorf("%s: unknown configuration field `%s`", source, field)
	}
	*warnings = append(*warnings, fmt.Sprintf("%s: unknown configuration field `%s`", source, field))
	return nil
}

// readTomlFile reads and parses a TOML file. It returns ok=false when the file
// does not exist (a missing layer is not an error), matching the loader's
// best-effort layer reads.
func readTomlFile(path string) (TomlValue, bool, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	value, err := ParseTomlValue(contents)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", path, err)
	}
	return value, true, nil
}

// profileConfigPath returns ${CODEXGO_HOME}/<profile>.config.toml, the profile-v2
// layer file used when a profile is selected.
func profileConfigPath(codexHome, profile string) string {
	return filepath.Join(codexHome, profile+".config.toml")
}
