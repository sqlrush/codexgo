package modelsmanager

import (
	"context"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// Ensure OpenAiModelsManager satisfies the ModelsManager interface.
var _ ModelsManager = (*OpenAiModelsManager)(nil)

// ListModels lists available presets, refreshing per strategy. Rust:
// ModelsManager::list_models default method.
func (m *OpenAiModelsManager) ListModels(ctx context.Context, strategy RefreshStrategy) []ModelPreset {
	catalog := m.RawModelCatalog(ctx, strategy)
	return buildAvailableModels(m.authManager, catalog.Models)
}

// RawModelCatalog refreshes per strategy and returns the active catalog. Refresh
// errors are swallowed (Rust logs them via error!). Rust:
// OpenAiModelsManager::raw_model_catalog.
func (m *OpenAiModelsManager) RawModelCatalog(ctx context.Context, strategy RefreshStrategy) ModelsResponse {
	_ = m.refreshAvailableModels(ctx, strategy)
	return ModelsResponse{Models: m.GetRemoteModels(ctx)}
}

// GetRemoteModels returns a copy of the current in-memory remote catalog. Rust:
// get_remote_models.
func (m *OpenAiModelsManager) GetRemoteModels(_ context.Context) []ModelInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneModels(m.remoteModels)
}

// TryGetRemoteModels attempts a non-blocking read of the remote catalog. Rust:
// try_get_remote_models (TryLockError -> ok=false).
func (m *OpenAiModelsManager) TryGetRemoteModels() ([]ModelInfo, bool) {
	if !m.mu.TryRLock() {
		return nil, false
	}
	defer m.mu.RUnlock()
	return cloneModels(m.remoteModels), true
}

// AuthManager returns the auth manager used for picker filtering. Rust:
// auth_manager.
func (m *OpenAiModelsManager) AuthManager() AuthManager {
	return m.authManager
}

// ListCollaborationModes returns the static collaboration-mode presets. Rust:
// list_collaboration_modes.
func (m *OpenAiModelsManager) ListCollaborationModes() []protocol.CollaborationModeMask {
	return BuiltinCollaborationModePresets()
}

// TryListModels attempts to list presets without blocking, using the current
// cached state. Rust: ModelsManager::try_list_models default method.
func (m *OpenAiModelsManager) TryListModels() ([]ModelPreset, bool) {
	remoteModels, ok := m.TryGetRemoteModels()
	if !ok {
		return nil, false
	}
	return buildAvailableModels(m.authManager, remoteModels), true
}

// GetDefaultModel returns the model id to use, refreshing per strategy. Rust:
// ModelsManager::get_default_model default method.
func (m *OpenAiModelsManager) GetDefaultModel(ctx context.Context, model *string, strategy RefreshStrategy) string {
	if model != nil {
		return *model
	}
	return defaultModelFromAvailable(m.ListModels(ctx, strategy))
}

// GetModelInfo looks up model metadata using the current remote catalog. Rust:
// ModelsManager::get_model_info default method.
func (m *OpenAiModelsManager) GetModelInfo(ctx context.Context, model string, config *ModelsManagerConfig) ModelInfo {
	remoteModels := m.GetRemoteModels(ctx)
	return constructModelInfoFromCandidates(model, remoteModels, config)
}

// RefreshIfNewEtag refreshes models when the supplied ETag differs from the
// cached ETag; otherwise it renews the cache TTL. Rust: refresh_if_new_etag.
func (m *OpenAiModelsManager) RefreshIfNewEtag(ctx context.Context, etag string) {
	currentEtag := m.getEtag()
	if currentEtag != nil && *currentEtag == etag {
		_ = m.cacheManager.renewCacheTTL()
		return
	}
	_ = m.refreshAvailableModels(ctx, RefreshOnline)
}

// refreshAvailableModels refreshes the remote catalog per strategy. Rust:
// refresh_available_models.
func (m *OpenAiModelsManager) refreshAvailableModels(ctx context.Context, strategy RefreshStrategy) error {
	if !m.shouldRefreshModels(ctx) {
		if strategy == RefreshOffline || strategy == RefreshOnlineIfUncached {
			m.tryLoadCache(ctx)
		}
		return nil
	}

	switch strategy {
	case RefreshOffline:
		m.tryLoadCache(ctx)
		return nil
	case RefreshOnlineIfUncached:
		if m.tryLoadCache(ctx) {
			return nil
		}
		return m.fetchAndUpdateModels(ctx)
	case RefreshOnline:
		return m.fetchAndUpdateModels(ctx)
	default:
		return m.fetchAndUpdateModels(ctx)
	}
}

// fetchAndUpdateModels fetches the remote catalog, applies it, and persists the
// cache. Rust: fetch_and_update_models.
func (m *OpenAiModelsManager) fetchAndUpdateModels(ctx context.Context) error {
	clientVersion := ClientVersionToWhole()
	models, etag, err := m.endpointClient.ListModels(ctx, clientVersion)
	if err != nil {
		return err
	}
	m.applyRemoteModels(models)
	m.setEtag(etag)
	_ = m.cacheManager.persistCache(models, etag, clientVersion)
	return nil
}

// shouldRefreshModels reports whether a remote refresh is allowed for the active
// auth. Rust: should_refresh_models.
func (m *OpenAiModelsManager) shouldRefreshModels(ctx context.Context) bool {
	return m.endpointClient.UsesCodexBackend(ctx) || m.endpointClient.HasCommandAuth()
}

// getEtag returns a copy of the cached ETag. Rust: get_etag.
func (m *OpenAiModelsManager) getEtag() *string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneStringPtr(m.etag)
}

// setEtag replaces the cached ETag.
func (m *OpenAiModelsManager) setEtag(etag *string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.etag = cloneStringPtr(etag)
}

// applyRemoteModels replaces the in-memory remote catalog, using the remote list
// as the source of truth for ChatGPT auth when it has at least one listed model,
// otherwise merging remote models onto the bundled catalog. Rust:
// apply_remote_models.
func (m *OpenAiModelsManager) applyRemoteModels(models []ModelInfo) {
	if m.shouldUseRemoteModelsOnly(models) {
		m.mu.Lock()
		m.remoteModels = cloneModels(models)
		m.mu.Unlock()
		return
	}

	existing := loadRemoteModelsFromFile()
	for _, model := range models {
		index := indexOfSlug(existing, model.Slug)
		if index >= 0 {
			existing[index] = model
		} else {
			existing = append(existing, model)
		}
	}
	m.mu.Lock()
	m.remoteModels = existing
	m.mu.Unlock()
}

// shouldUseRemoteModelsOnly reports whether the remote catalog should fully
// replace the bundled catalog. Rust: should_use_remote_models_only inline check.
func (m *OpenAiModelsManager) shouldUseRemoteModelsOnly(models []ModelInfo) bool {
	if len(models) == 0 {
		return false
	}
	hasListed := false
	for _, model := range models {
		if model.Visibility == ModelVisibilityList {
			hasListed = true
			break
		}
	}
	if !hasListed {
		return false
	}
	return m.authIsChatGPT()
}

// authIsChatGPT reports whether the active auth mode is ChatGPT or
// ChatGPT-auth-tokens.
func (m *OpenAiModelsManager) authIsChatGPT() bool {
	if m.authManager == nil {
		return false
	}
	mode := m.authManager.AuthMode()
	if mode == nil {
		return false
	}
	return *mode == appserverproto.AuthModeChatgpt || *mode == appserverproto.AuthModeChatgptAuthTokens
}

// tryLoadCache attempts to satisfy a refresh from the cache. Returns true when a
// fresh, matching cache entry was applied. Rust: try_load_cache.
func (m *OpenAiModelsManager) tryLoadCache(_ context.Context) bool {
	clientVersion := ClientVersionToWhole()
	cache, err := m.cacheManager.loadFresh(clientVersion)
	if err != nil || cache == nil {
		return false
	}
	m.setEtag(cache.Etag)
	m.applyRemoteModels(cache.Models)
	return true
}

// indexOfSlug returns the index of the first model with the given slug, or -1.
func indexOfSlug(models []ModelInfo, slug string) int {
	for i := range models {
		if models[i].Slug == slug {
			return i
		}
	}
	return -1
}
