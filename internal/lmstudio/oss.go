package lmstudio

import (
	"context"

	"github.com/sqlrush/codexgo/internal/modelproviderinfo"
)

// DefaultOSSModel is the default OSS model used when --oss is passed without an
// explicit -m. It mirrors the Rust codex_lmstudio::DEFAULT_OSS_MODEL.
const DefaultOSSModel = "openai/gpt-oss-20b"

// EnsureOSSReady prepares the local OSS environment when --oss is selected:
//
//   - ensures a local LM Studio server is reachable,
//   - downloads the requested model if it is missing, and
//   - loads (warms up) the model in the background.
//
// The providers map should be derived from config so user overrides are honored;
// model is the requested model (empty means use DefaultOSSModel). It mirrors the
// Rust ensure_oss_ready. A failure to list local models is non-fatal (the
// download is skipped), and the background load is best-effort, matching the
// reference behavior.
//
// The background load goroutine outlives this call and uses context.Background
// so it is not cancelled when ctx is (the Rust code spawns it detached). Callers
// that need to await or cancel the load should invoke LoadModel directly.
func EnsureOSSReady(ctx context.Context, providers map[string]modelproviderinfo.ModelProviderInfo, model string) error {
	if model == "" {
		model = DefaultOSSModel
	}

	client, err := TryFromProvider(ctx, providers)
	if err != nil {
		return err
	}

	if models, err := client.FetchModels(ctx); err == nil {
		if !containsString(models, model) {
			if err := client.DownloadModel(ctx, model); err != nil {
				return err
			}
		}
	}
	// A FetchModels error is non-fatal; higher layers may still proceed and
	// surface errors later, matching the reference.

	// Load the model in the background (best-effort, detached from ctx).
	go func() {
		_ = client.LoadModel(context.Background(), model)
	}()

	return nil
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
