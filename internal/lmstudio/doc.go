// Package lmstudio is a faithful, drop-in-compatible Go port of the Rust crate
// codex-lmstudio (codex 0.136.0).
//
// It provides a client for a locally running LM Studio server and the helpers
// used when Codex is launched in OSS mode (--oss) against LM Studio:
//
//   - detecting a reachable local server (default http://localhost:1234/v1),
//   - discovering available models via the OpenAI-compatible /models endpoint,
//   - loading a model (warm-up) via the Responses-compatible /responses endpoint,
//   - downloading a missing model by shelling out to the `lms` CLI, and
//   - preparing the OSS environment via EnsureOSSReady.
//
// The HTTP endpoints, request/response shapes, default model, CLI fallback
// paths, and user-facing error strings match real codex so on-disk config and
// runtime behavior are interchangeable.
//
// Unlike the Rust crate, TryFromProvider accepts a provider map directly rather
// than a Config value, because the configuration crate is not a dependency of
// this package. Callers derive the provider map from config and pass it in,
// preserving the "honor user overrides" behavior.
package lmstudio
