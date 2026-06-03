package ossutil

import (
	"context"
	"errors"
	"fmt"
)

// ossSetupFailedFormat is the prefix applied to errors surfaced by a provider's
// ensure-ready step. It mirrors the Rust crate's format string
// "OSS setup failed: {e}" so callers and logs observe identical messages.
const ossSetupFailedFormat = "OSS setup failed: %s"

// OSSProvisioner abstracts the network-dependent operations required to prepare
// a local OSS provider for use. In the Rust crate these are implemented by the
// codex-lmstudio and codex-ollama crates; here they are injected so the
// dispatch and error-wrapping behavior can be ported faithfully and tested
// without network access.
//
// Implementations must not mutate any shared state observable by the caller and
// must honor cancellation via the supplied context.
type OSSProvisioner interface {
	// EnsureLMStudioReady ensures a local LM Studio server is reachable and the
	// required model is available. It corresponds to
	// codex_lmstudio::ensure_oss_ready.
	EnsureLMStudioReady(ctx context.Context) error

	// EnsureOllamaResponsesSupported ensures the running Ollama server is new
	// enough to support the Responses API. It corresponds to
	// codex_ollama::ensure_responses_supported.
	EnsureOllamaResponsesSupported(ctx context.Context) error

	// EnsureOllamaReady ensures a local Ollama server is reachable and the
	// required model is available. It corresponds to
	// codex_ollama::ensure_oss_ready.
	EnsureOllamaReady(ctx context.Context) error
}

// EnsureOSSProviderReady ensures the specified OSS provider is ready (models
// downloaded, service reachable) by dispatching to the supplied provisioner.
//
// The dispatch mirrors the Rust crate exactly:
//
//   - "lmstudio": EnsureLMStudioReady; any error is wrapped as
//     "OSS setup failed: {e}".
//   - "ollama": EnsureOllamaResponsesSupported first (its error is returned
//     verbatim), then EnsureOllamaReady whose error is wrapped as
//     "OSS setup failed: {e}".
//   - any other identifier: unknown provider, setup is skipped and nil is
//     returned.
//
// A nil provisioner is rejected at the boundary for any recognized provider; an
// unknown provider returns nil without consulting the provisioner.
func EnsureOSSProviderReady(ctx context.Context, providerID string, prov OSSProvisioner) error {
	switch providerID {
	case LMStudioOSSProviderID:
		if prov == nil {
			return errors.New("ossutil: provisioner must not be nil")
		}
		if err := prov.EnsureLMStudioReady(ctx); err != nil {
			return fmt.Errorf(ossSetupFailedFormat, err)
		}
		return nil

	case OllamaOSSProviderID:
		if prov == nil {
			return errors.New("ossutil: provisioner must not be nil")
		}
		// ensure_responses_supported is awaited first; its error propagates
		// unwrapped, exactly as in the Rust crate (it uses `?`).
		if err := prov.EnsureOllamaResponsesSupported(ctx); err != nil {
			return err
		}
		if err := prov.EnsureOllamaReady(ctx); err != nil {
			return fmt.Errorf(ossSetupFailedFormat, err)
		}
		return nil

	default:
		// Unknown provider, skip setup.
		return nil
	}
}
