// Package ollama is a faithful, drop-in-compatible Go port of the Rust crate
// codex-ollama (codex 0.136.0).
//
// It provides a client for a locally running Ollama server and the helpers used
// when Codex is launched in OSS mode (--oss) against Ollama:
//
//   - detecting a reachable local server (default http://localhost:11434),
//   - checking the server version (>= 0.13.4) before using the Responses-API
//     compatible path,
//   - listing and pulling models with streaming progress, and
//   - preparing the OSS environment via EnsureOSSReady / EnsureResponsesSupported.
//
// The HTTP endpoints, request/response shapes, version cutoff, default model,
// and user-facing error strings match real codex so on-disk config and runtime
// behavior are interchangeable.
//
// Unlike the Rust crate, the constructors accept a provider (or provider map)
// directly rather than a Config value, because the configuration crate is not a
// dependency of this package. Callers derive the provider map from config and
// pass it in, preserving the "honor user overrides" behavior.
package ollama
