package ollama

import (
	"context"
	"fmt"

	"github.com/sqlrush/codexgo/internal/modelproviderinfo"
)

// DefaultOSSModel is the default OSS model used when --oss is passed without an
// explicit -m. It mirrors the Rust codex_ollama::DEFAULT_OSS_MODEL.
const DefaultOSSModel = "gpt-oss:20b"

// minResponsesMajor, minResponsesMinor and minResponsesPatch define the minimum
// Ollama server version that supports the Responses API. They mirror
// min_responses_version (Version::new(0, 13, 4)).
const (
	minResponsesMajor = 0
	minResponsesMinor = 13
	minResponsesPatch = 4
)

// MinResponsesVersion returns the minimum Ollama version that supports the
// Responses API. It mirrors min_responses_version.
func MinResponsesVersion() Version {
	return NewVersion(minResponsesMajor, minResponsesMinor, minResponsesPatch)
}

// SupportsResponses reports whether an Ollama server of the given version
// supports the Responses API. A development build reporting 0.0.0 is always
// supported; otherwise any version >= the minimum (0.13.4) is supported. It
// mirrors supports_responses.
func SupportsResponses(version Version) bool {
	if version == (Version{}) {
		return true
	}
	return version.Compare(MinResponsesVersion()) >= 0
}

// EnsureResponsesSupported verifies the running Ollama server described by
// provider is new enough to support the Responses API. It returns nil when the
// version endpoint is missing or unparsable, and an error when the server is too
// old. It mirrors the Rust ensure_responses_supported.
func EnsureResponsesSupported(ctx context.Context, provider modelproviderinfo.ModelProviderInfo) error {
	client, err := TryFromProvider(ctx, provider)
	if err != nil {
		return err
	}
	version, err := client.FetchVersion(ctx)
	if err != nil {
		return err
	}
	if version == nil {
		return nil
	}
	if SupportsResponses(*version) {
		return nil
	}
	min := MinResponsesVersion()
	return fmt.Errorf("Ollama %s is too old. Codex requires Ollama %s or newer.", version, min)
}

// EnsureOSSReady prepares the local OSS environment when --oss is selected:
//
//   - ensures a local Ollama server is reachable, and
//   - pulls the requested model if it is missing.
//
// The providers map should be derived from config so user overrides are honored;
// model is the requested model (empty means use DefaultOSSModel). It mirrors the
// Rust ensure_oss_ready, where model defaults from config.model. A failure to
// list local models is non-fatal (the pull is skipped), matching the reference.
func EnsureOSSReady(ctx context.Context, providers map[string]modelproviderinfo.ModelProviderInfo, model string) error {
	if model == "" {
		model = DefaultOSSModel
	}

	client, err := TryFromOSSProvider(ctx, providers)
	if err != nil {
		return err
	}

	models, err := client.FetchModels(ctx)
	if err != nil {
		// Not fatal; higher layers may still proceed and surface errors later.
		return nil
	}
	if containsString(models, model) {
		return nil
	}
	reporter := NewCliProgressReporter()
	return client.PullWithReporter(ctx, model, reporter)
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
