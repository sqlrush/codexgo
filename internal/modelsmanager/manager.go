package modelsmanager

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/protocol"
)

const (
	// modelCacheFile is the on-disk cache filename under codex home.
	modelCacheFile = "models_cache.json"
	// defaultModelCacheTTL is the default cache freshness window. Rust:
	// DEFAULT_MODEL_CACHE_TTL = 300s.
	defaultModelCacheTTL = 300 * time.Second
)

// AuthManager is the narrow view of the auth manager required by the models
// manager: the current auth mode and whether it uses the Codex backend. The
// concrete implementation lives in the (not-yet-ported) login package; this
// interface lets the models manager depend only on the methods it consumes.
type AuthManager interface {
	// AuthMode returns the currently resolved auth mode, or nil when unknown.
	AuthMode() *appserverproto.AuthMode
	// CurrentAuthUsesCodexBackend reports whether the current auth can use
	// Codex backend-only models.
	CurrentAuthUsesCodexBackend() bool
}

// ModelsEndpointClient is the remote endpoint used by the OpenAI-compatible
// model manager. Implementations own provider-specific auth and transport; the
// manager owns refresh policy, cache behavior, and catalog merging. Rust:
// ModelsEndpointClient.
type ModelsEndpointClient interface {
	// HasCommandAuth reports whether this provider can authenticate
	// command-scoped requests.
	HasCommandAuth() bool
	// UsesCodexBackend reports whether the resolved auth can use Codex
	// backend-only models.
	UsesCodexBackend(ctx context.Context) bool
	// ListModels fetches the latest remote model catalog and optional ETag.
	ListModels(ctx context.Context, clientVersion string) ([]ModelInfo, *string, error)
}

// RefreshStrategy selects how the manager refreshes available models. Rust:
// RefreshStrategy.
type RefreshStrategy int

const (
	// RefreshOnline always fetches from the network, ignoring cache.
	RefreshOnline RefreshStrategy = iota
	// RefreshOffline only uses cached data, never fetching from the network.
	RefreshOffline
	// RefreshOnlineIfUncached uses cache if available and fresh, otherwise
	// fetches from the network.
	RefreshOnlineIfUncached
)

// String returns the lowercase identifier for the strategy. Rust:
// RefreshStrategy::as_str / Display.
func (s RefreshStrategy) String() string {
	switch s {
	case RefreshOnline:
		return "online"
	case RefreshOffline:
		return "offline"
	case RefreshOnlineIfUncached:
		return "online_if_uncached"
	default:
		return "online_if_uncached"
	}
}

// ModelsManager coordinates model discovery plus cached metadata on disk. Rust:
// ModelsManager trait. The shared default methods are provided as free helpers
// to avoid Go's lack of trait default-method support; concrete managers expose
// them directly.
type ModelsManager interface {
	// ListModels lists available presets, refreshing per the strategy. Returns
	// presets sorted by priority and filtered by auth mode and visibility.
	ListModels(ctx context.Context, strategy RefreshStrategy) []ModelPreset
	// RawModelCatalog returns the active raw catalog, refreshing per strategy.
	RawModelCatalog(ctx context.Context, strategy RefreshStrategy) ModelsResponse
	// GetRemoteModels returns the current in-memory remote catalog.
	GetRemoteModels(ctx context.Context) []ModelInfo
	// TryGetRemoteModels attempts to return the current in-memory remote
	// catalog without blocking; ok is false when the lock is contended.
	TryGetRemoteModels() (models []ModelInfo, ok bool)
	// AuthManager returns the auth manager used for picker filtering, or nil.
	AuthManager() AuthManager
	// ListCollaborationModes returns the static collaboration-mode presets.
	ListCollaborationModes() []protocol.CollaborationModeMask
	// GetDefaultModel returns the model id to use, refreshing per strategy.
	GetDefaultModel(ctx context.Context, model *string, strategy RefreshStrategy) string
	// GetModelInfo looks up model metadata, applying remote overrides and
	// config adjustments.
	GetModelInfo(ctx context.Context, model string, config *ModelsManagerConfig) ModelInfo
	// RefreshIfNewEtag refreshes models when the supplied ETag differs from the
	// cached ETag.
	RefreshIfNewEtag(ctx context.Context, etag string)
}

// buildAvailableModels builds picker-ready presets from a catalog snapshot. It
// sorts by priority, converts to presets, filters by auth, and marks the
// default. Rust: ModelsManager::build_available_models. The input slice is not
// mutated (a copy is sorted).
func buildAvailableModels(auth AuthManager, remoteModels []ModelInfo) []ModelPreset {
	sorted := cloneModels(remoteModels)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	presets := make([]ModelPreset, 0, len(sorted))
	for _, info := range sorted {
		presets = append(presets, modelPresetFromInfo(info))
	}

	usesCodexBackend := auth != nil && auth.CurrentAuthUsesCodexBackend()
	presets = FilterByAuth(presets, usesCodexBackend)
	MarkDefaultByPickerVisibility(presets)
	return presets
}

// defaultModelFromAvailable returns the default model id from a preset list:
// the marked default, else the first preset, else "". Rust:
// default_model_from_available.
func defaultModelFromAvailable(available []ModelPreset) string {
	for _, preset := range available {
		if preset.IsDefault {
			return preset.Model
		}
	}
	if len(available) > 0 {
		return available[0].Model
	}
	return ""
}

// findModelByLongestPrefix returns the candidate whose slug is the longest
// prefix of model. Rust: find_model_by_longest_prefix.
func findModelByLongestPrefix(model string, candidates []ModelInfo) *ModelInfo {
	var best *ModelInfo
	for i := range candidates {
		candidate := candidates[i]
		if !strings.HasPrefix(model, candidate.Slug) {
			continue
		}
		if best == nil || len(candidate.Slug) > len(best.Slug) {
			c := candidate
			best = &c
		}
	}
	return best
}

// findModelByNamespacedSuffix retries metadata lookup for a single namespaced
// slug like `namespace/model-name`, stripping one leading namespace segment when
// the namespace looks like a simple provider id. Rust:
// find_model_by_namespaced_suffix.
func findModelByNamespacedSuffix(model string, candidates []ModelInfo) *ModelInfo {
	namespace, suffix, found := strings.Cut(model, "/")
	if !found {
		return nil
	}
	if strings.Contains(suffix, "/") {
		return nil
	}
	if namespace == "" || !isSimpleProviderID(namespace) {
		return nil
	}
	return findModelByLongestPrefix(suffix, candidates)
}

// isSimpleProviderID reports whether s contains only ASCII alphanumerics,
// underscores, or hyphens.
func isSimpleProviderID(s string) bool {
	for _, r := range s {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !isAlnum && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

// constructModelInfoFromCandidates resolves model metadata from candidates,
// applying namespaced-suffix fallback and config overrides. Rust:
// construct_model_info_from_candidates.
func constructModelInfoFromCandidates(model string, candidates []ModelInfo, config *ModelsManagerConfig) ModelInfo {
	remote := findModelByLongestPrefix(model, candidates)
	if remote == nil {
		remote = findModelByNamespacedSuffix(model, candidates)
	}

	var modelInfo ModelInfo
	if remote != nil {
		modelInfo = *remote
		modelInfo.Slug = model
		modelInfo.UsedFallbackModelMetadata = false
	} else {
		modelInfo = ModelInfoFromSlug(model)
	}
	return WithConfigOverrides(modelInfo, config)
}

// OpenAiModelsManager is the OpenAI-compatible model manager backed by bundled
// models, an on-disk cache, and the remote `/models` endpoint. Rust:
// OpenAiModelsManager.
type OpenAiModelsManager struct {
	mu             sync.RWMutex
	remoteModels   []ModelInfo
	etag           *string
	cacheManager   *modelsCacheManager
	endpointClient ModelsEndpointClient
	authManager    AuthManager
}

// NewOpenAiModelsManager constructs an OpenAI-compatible remote model manager.
// Rust: OpenAiModelsManager::new.
func NewOpenAiModelsManager(codexHome string, endpointClient ModelsEndpointClient, authManager AuthManager) *OpenAiModelsManager {
	cachePath := filepath.Join(codexHome, modelCacheFile)
	cacheManager := newModelsCacheManager(cachePath, defaultModelCacheTTL)
	remoteModels := loadRemoteModelsFromFile()
	return &OpenAiModelsManager{
		remoteModels:   remoteModels,
		cacheManager:   cacheManager,
		endpointClient: endpointClient,
		authManager:    authManager,
	}
}

// StaticModelsManager is a model manager backed by an authoritative in-process
// catalog. Rust: StaticModelsManager.
type StaticModelsManager struct {
	remoteModels []ModelInfo
	authManager  AuthManager
}

// NewStaticModelsManager constructs a static model manager from an authoritative
// catalog. Rust: StaticModelsManager::new.
func NewStaticModelsManager(authManager AuthManager, catalog ModelsResponse) *StaticModelsManager {
	return &StaticModelsManager{
		remoteModels: catalog.Models,
		authManager:  authManager,
	}
}

// loadRemoteModelsFromFile loads the bundled models, returning nil on error
// (mirroring Rust's unwrap_or_default). Rust: load_remote_models_from_file.
func loadRemoteModelsFromFile() []ModelInfo {
	response, err := BundledModelsResponse()
	if err != nil {
		return nil
	}
	return response.Models
}
