// Package config loads, merges, and validates the codexgo configuration stored
// in ~/.codexgo/config.toml into the resolved Config. It is a faithful Go port
// of the Rust codex-config crate, preserving the on-disk TOML and JSON
// serialization formats byte-for-byte (after key-order canonicalization); only
// the directory location and environment variable names differ, because
// codexgo keeps a fully isolated local namespace (see internal/brand).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sqlrush/codexgo/internal/brand"
)

// CodexHomeEnvVar is the environment variable that overrides the codexgo
// configuration directory.
const CodexHomeEnvVar = brand.HomeEnvVar

// DefaultCodexDirName is the directory name used under the user's home directory
// when CODEXGO_HOME is not set.
const DefaultCodexDirName = brand.DotDirName

// ConfigTomlFile is the canonical config file name within CODEXGO_HOME.
const ConfigTomlFile = "config.toml"

// ConfigLocalTomlFile is an optional overlay file that is merged on top of
// config.toml from the same directory, giving its values precedence over the
// base config.toml but below CLI/env overrides.
const ConfigLocalTomlFile = "config.local.toml"

// FindCodexHome resolves the codexgo configuration directory.
//
// When CODEXGO_HOME is set and non-empty, the value must exist and be a
// directory; it is resolved to an absolute path or an error is returned. When
// CODEXGO_HOME is unset, the default is <home>/.codexgo and its existence is
// NOT verified. This mirrors the Rust find_codex_home behavior.
func FindCodexHome() (string, error) {
	env := os.Getenv(CodexHomeEnvVar)
	return findCodexHomeFromEnv(env)
}

func findCodexHomeFromEnv(env string) (string, error) {
	if env != "" {
		info, err := os.Stat(env)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("%s points to %q, but that path does not exist", CodexHomeEnvVar, env)
			}
			return "", fmt.Errorf("failed to read %s %q: %w", CodexHomeEnvVar, env, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("%s points to %q, but that path is not a directory", CodexHomeEnvVar, env)
		}
		abs, err := filepath.Abs(env)
		if err != nil {
			return "", fmt.Errorf("failed to resolve %s %q: %w", CodexHomeEnvVar, env, err)
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return "", fmt.Errorf("failed to canonicalize %s %q: %w", CodexHomeEnvVar, env, err)
		}
		return resolved, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not find home directory: %w", err)
	}
	return filepath.Join(home, DefaultCodexDirName), nil
}

// ConfigTomlPath returns the path to config.toml within the given CODEXGO_HOME.
func ConfigTomlPath(codexHome string) string {
	return filepath.Join(codexHome, ConfigTomlFile)
}

// ConfigLocalTomlPath returns the path to config.local.toml within CODEXGO_HOME.
func ConfigLocalTomlPath(codexHome string) string {
	return filepath.Join(codexHome, ConfigLocalTomlFile)
}
