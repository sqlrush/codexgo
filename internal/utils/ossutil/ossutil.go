// Package ossutil provides OSS-mode helpers shared between the TUI and exec
// front-ends for selecting and preparing open-source model providers.
//
// It is a faithful Go port of the codex-utils-oss Rust crate used by Codex
// 0.136.0. The externally observable behavior is preserved exactly:
//
//   - the set of recognized OSS provider identifiers,
//   - the default model returned for each provider,
//   - the dispatch order and error-message formatting used when preparing a
//     provider for use.
//
// In the Rust crate, provider preparation calls directly into the
// codex-lmstudio and codex-ollama client crates (which perform network I/O).
// Those crates are not yet part of this Go module, so EnsureOSSProviderReady
// accepts an OSSProvisioner interface that abstracts those operations. This
// keeps the dispatch logic, sequencing, and error wrapping identical while
// allowing the network-dependent pieces to be supplied (and faithfully ported)
// later.
//
// All exported functions are pure with respect to their inputs: they never
// mutate caller-provided values.
package ossutil

const (
	// LMStudioOSSProviderID is the provider identifier for a locally running
	// LM Studio server. It mirrors codex_model_provider_info::LMSTUDIO_OSS_PROVIDER_ID.
	LMStudioOSSProviderID = "lmstudio"

	// OllamaOSSProviderID is the provider identifier for a locally running
	// Ollama server. It mirrors codex_model_provider_info::OLLAMA_OSS_PROVIDER_ID.
	OllamaOSSProviderID = "ollama"
)

const (
	// LMStudioDefaultOSSModel is the default model used when LM Studio is
	// selected via --oss without an explicit -m. It mirrors
	// codex_lmstudio::DEFAULT_OSS_MODEL.
	LMStudioDefaultOSSModel = "openai/gpt-oss-20b"

	// OllamaDefaultOSSModel is the default model used when Ollama is selected
	// via --oss without an explicit -m. It mirrors codex_ollama::DEFAULT_OSS_MODEL.
	OllamaDefaultOSSModel = "gpt-oss:20b"
)

// GetDefaultModelForOSSProvider returns the default model for a given OSS
// provider identifier.
//
// It returns the default model and ok=true for a recognized provider, or an
// empty string and ok=false for any other (unknown) provider. This mirrors the
// Rust signature returning Option<&'static str>.
func GetDefaultModelForOSSProvider(providerID string) (model string, ok bool) {
	switch providerID {
	case LMStudioOSSProviderID:
		return LMStudioDefaultOSSModel, true
	case OllamaOSSProviderID:
		return OllamaDefaultOSSModel, true
	default:
		return "", false
	}
}
