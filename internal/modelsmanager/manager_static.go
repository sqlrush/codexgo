package modelsmanager

import (
	"context"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// Ensure StaticModelsManager satisfies the ModelsManager interface.
var _ ModelsManager = (*StaticModelsManager)(nil)

// ListModels lists available presets. The static manager never refreshes. Rust:
// ModelsManager::list_models default method.
func (m *StaticModelsManager) ListModels(ctx context.Context, strategy RefreshStrategy) []ModelPreset {
	catalog := m.RawModelCatalog(ctx, strategy)
	return buildAvailableModels(m.authManager, catalog.Models)
}

// RawModelCatalog returns the static catalog, ignoring the strategy. Rust:
// StaticModelsManager::raw_model_catalog.
func (m *StaticModelsManager) RawModelCatalog(ctx context.Context, _ RefreshStrategy) ModelsResponse {
	return ModelsResponse{Models: m.GetRemoteModels(ctx)}
}

// GetRemoteModels returns a copy of the static catalog. Rust: get_remote_models.
func (m *StaticModelsManager) GetRemoteModels(_ context.Context) []ModelInfo {
	return cloneModels(m.remoteModels)
}

// TryGetRemoteModels returns the static catalog; it never blocks. Rust:
// try_get_remote_models.
func (m *StaticModelsManager) TryGetRemoteModels() ([]ModelInfo, bool) {
	return cloneModels(m.remoteModels), true
}

// AuthManager returns the auth manager used for picker filtering. Rust:
// auth_manager.
func (m *StaticModelsManager) AuthManager() AuthManager {
	return m.authManager
}

// ListCollaborationModes returns the static collaboration-mode presets. Rust:
// list_collaboration_modes.
func (m *StaticModelsManager) ListCollaborationModes() []protocol.CollaborationModeMask {
	return BuiltinCollaborationModePresets()
}

// TryListModels lists presets from the static catalog without blocking. Rust:
// ModelsManager::try_list_models default method.
func (m *StaticModelsManager) TryListModels() ([]ModelPreset, bool) {
	models, _ := m.TryGetRemoteModels()
	return buildAvailableModels(m.authManager, models), true
}

// GetDefaultModel returns the model id to use. Rust:
// ModelsManager::get_default_model default method.
func (m *StaticModelsManager) GetDefaultModel(ctx context.Context, model *string, strategy RefreshStrategy) string {
	if model != nil {
		return *model
	}
	return defaultModelFromAvailable(m.ListModels(ctx, strategy))
}

// GetModelInfo looks up model metadata from the static catalog. Rust:
// ModelsManager::get_model_info default method.
func (m *StaticModelsManager) GetModelInfo(ctx context.Context, model string, config *ModelsManagerConfig) ModelInfo {
	remoteModels := m.GetRemoteModels(ctx)
	return constructModelInfoFromCandidates(model, remoteModels, config)
}

// RefreshIfNewEtag is a no-op for the static manager. Rust: refresh_if_new_etag.
func (m *StaticModelsManager) RefreshIfNewEtag(_ context.Context, _ string) {}
