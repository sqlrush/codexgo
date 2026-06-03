// Package modelsmanager is a faithful, drop-in-compatible Go port of the
// codex-rs `models-manager` crate (codex 0.136.0). See openai_models.go for the
// package-level overview.
package modelsmanager

import (
	"encoding/json"
	"fmt"
	"strings"

	_ "embed"

	"github.com/sqlrush/codexgo/internal/modelproviderinfo"
)

// bundledModelsJSON is the model catalog shipped with the crate. Rust:
// include_str!("../models.json").
//
//go:embed models.json
var bundledModelsJSON []byte

// BundledModelsResponse loads the bundled model catalog shipped with this
// package. Rust: bundled_models_response.
func BundledModelsResponse() (ModelsResponse, error) {
	var response ModelsResponse
	if err := json.Unmarshal(bundledModelsJSON, &response); err != nil {
		return ModelsResponse{}, fmt.Errorf("parse bundled models.json: %w", err)
	}
	return response, nil
}

// ClientVersionToWhole converts the crate version string to a whole version
// string (e.g. "1.2.3-alpha.4" -> "1.2.3"). Rust: client_version_to_whole,
// which uses CARGO_PKG_VERSION_{MAJOR,MINOR,PATCH}.
func ClientVersionToWhole() string {
	return wholeVersion(modelproviderinfo.CodexPkgVersion)
}

// wholeVersion extracts the MAJOR.MINOR.PATCH triple from a semver-style string,
// stripping any pre-release / build suffix. Missing components default to "0".
func wholeVersion(version string) string {
	// Drop pre-release (`-`) and build metadata (`+`) suffixes.
	core := version
	if idx := strings.IndexAny(core, "-+"); idx >= 0 {
		core = core[:idx]
	}
	parts := strings.Split(core, ".")
	major := versionComponent(parts, 0)
	minor := versionComponent(parts, 1)
	patch := versionComponent(parts, 2)
	return fmt.Sprintf("%s.%s.%s", major, minor, patch)
}

// versionComponent returns the component at index, or "0" when absent/empty.
func versionComponent(parts []string, index int) string {
	if index < len(parts) && parts[index] != "" {
		return parts[index]
	}
	return "0"
}
