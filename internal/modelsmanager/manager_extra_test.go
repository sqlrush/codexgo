package modelsmanager

import (
	"context"
	"testing"
)

// TestRefreshStrategyString covers RefreshStrategy::Display / as_str parity.
func TestRefreshStrategyString(t *testing.T) {
	cases := []struct {
		strategy RefreshStrategy
		want     string
	}{
		{RefreshOnline, "online"},
		{RefreshOffline, "offline"},
		{RefreshOnlineIfUncached, "online_if_uncached"},
		{RefreshStrategy(99), "online_if_uncached"},
	}
	for _, tc := range cases {
		if got := tc.strategy.String(); got != tc.want {
			t.Fatalf("RefreshStrategy(%d).String() = %q, want %q", tc.strategy, got, tc.want)
		}
	}
}

// TestGetDefaultModelReturnsProvided covers the early-return when a model is
// supplied (get_default_model: returns it directly).
func TestGetDefaultModelReturnsProvided(t *testing.T) {
	ctx := context.Background()
	manager := NewStaticModelsManager(nil, ModelsResponse{Models: nil})
	provided := "explicit-model"
	if got := manager.GetDefaultModel(ctx, &provided, RefreshOnline); got != provided {
		t.Fatalf("GetDefaultModel(provided) = %q, want %q", got, provided)
	}
}

// TestGetDefaultModelPicksDefaultPreset covers default_model_from_available: the
// preset marked is_default is selected over the first preset.
func TestGetDefaultModelPicksDefaultPreset(t *testing.T) {
	ctx := context.Background()
	hidden := remoteModelWithVisibility(t, "hidden", "Hidden", 0, "hide")
	visible := remoteModelWithVisibility(t, "visible", "Visible", 1, "list")
	manager := NewStaticModelsManager(nil, ModelsResponse{Models: []ModelInfo{hidden, visible}})

	// The visible model is marked default by MarkDefaultByPickerVisibility even
	// though the hidden model sorts first by priority.
	if got := manager.GetDefaultModel(ctx, nil, RefreshOnline); got != "visible" {
		t.Fatalf("GetDefaultModel(nil) = %q, want %q", got, "visible")
	}
}

// TestGetDefaultModelEmptyCatalog covers default_model_from_available's
// unwrap_or_default when no presets are available.
func TestGetDefaultModelEmptyCatalog(t *testing.T) {
	ctx := context.Background()
	manager := NewStaticModelsManager(nil, ModelsResponse{Models: nil})
	if got := manager.GetDefaultModel(ctx, nil, RefreshOnline); got != "" {
		t.Fatalf("GetDefaultModel(empty) = %q, want empty string", got)
	}
}

// TestRefreshOfflineLoadsCacheWithoutFetch covers the Offline branch of
// refresh_available_models: cache is consulted but the network is never hit.
func TestRefreshOfflineLoadsCacheWithoutFetch(t *testing.T) {
	ctx := context.Background()
	codexHome := t.TempDir()
	remoteModels := []ModelInfo{remoteModel(t, "offline-cached", "Offline Cached", 0)}

	// Seed the cache via an online fetch with a fresh manager.
	seedEndpoint := newFakeEndpoint([][]ModelInfo{remoteModels})
	seedManager := NewOpenAiModelsManager(codexHome, seedEndpoint, chatgptAuth())
	if err := seedManager.refreshAvailableModels(ctx, RefreshOnline); err != nil {
		t.Fatalf("seed refresh: %v", err)
	}

	// A second manager using Offline must load from cache and never fetch.
	offlineEndpoint := newFakeEndpoint(nil)
	offlineManager := NewOpenAiModelsManager(codexHome, offlineEndpoint, chatgptAuth())
	if err := offlineManager.refreshAvailableModels(ctx, RefreshOffline); err != nil {
		t.Fatalf("offline refresh: %v", err)
	}
	if !containsSlug(offlineManager.GetRemoteModels(ctx), "offline-cached") {
		t.Fatalf("offline refresh should load cached models")
	}
	if offlineEndpoint.count() != 0 {
		t.Fatalf("offline strategy must not fetch, got %d", offlineEndpoint.count())
	}
}

// TestRefreshOfflineNoCacheKeepsBundled covers the Offline branch when no cache
// is present: the bundled catalog remains and no fetch occurs.
func TestRefreshOfflineNoCacheKeepsBundled(t *testing.T) {
	ctx := context.Background()
	endpoint := newFakeEndpoint([][]ModelInfo{{remoteModel(t, "should-not-load", "Nope", 0)}})
	manager := newOpenAIManagerForTests(t, endpoint)
	bundledCount := len(loadRemoteModelsFromFile())

	if err := manager.refreshAvailableModels(ctx, RefreshOffline); err != nil {
		t.Fatalf("offline refresh: %v", err)
	}
	if got := manager.GetRemoteModels(ctx); len(got) != bundledCount {
		t.Fatalf("offline-with-no-cache should keep bundled catalog (%d), got %d", bundledCount, len(got))
	}
	if endpoint.count() != 0 {
		t.Fatalf("offline strategy must not fetch, got %d", endpoint.count())
	}
}

// TestRefreshSkipsButLoadsCacheWhenNoAuthOffline covers the should_refresh ==
// false branch with an Offline/OnlineIfUncached strategy: cache is still loaded.
func TestRefreshSkipsButLoadsCacheWhenNoAuthOffline(t *testing.T) {
	ctx := context.Background()
	codexHome := t.TempDir()
	remoteModels := []ModelInfo{remoteModel(t, "no-auth-cached", "No Auth Cached", 0)}

	// Seed cache with a refreshing manager.
	seedEndpoint := newFakeEndpoint([][]ModelInfo{remoteModels})
	seedManager := NewOpenAiModelsManager(codexHome, seedEndpoint, chatgptAuth())
	if err := seedManager.refreshAvailableModels(ctx, RefreshOnline); err != nil {
		t.Fatalf("seed refresh: %v", err)
	}

	// A non-refreshing endpoint (should_refresh == false) with OnlineIfUncached
	// must still load the cache rather than fetch.
	endpoint := newFakeEndpointWithoutRefresh(nil)
	manager := NewOpenAiModelsManager(codexHome, endpoint, chatgptAuth())
	if err := manager.refreshAvailableModels(ctx, RefreshOnlineIfUncached); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !containsSlug(manager.GetRemoteModels(ctx), "no-auth-cached") {
		t.Fatalf("non-refreshing manager should still load cache")
	}
	if endpoint.count() != 0 {
		t.Fatalf("non-refreshing endpoint must not fetch, got %d", endpoint.count())
	}
}

// TestRefreshIfNewEtagFetchesOnMismatch covers the refresh_if_new_etag branch
// where the supplied ETag differs from the cached ETag, triggering an Online
// fetch.
func TestRefreshIfNewEtagFetchesOnMismatch(t *testing.T) {
	ctx := context.Background()
	models := []ModelInfo{remoteModel(t, "etag-new", "Etag New", 0)}
	endpoint := newFakeEndpoint([][]ModelInfo{models})
	manager := newOpenAIManagerForTests(t, endpoint)
	current := "old-etag"
	manager.setEtag(&current)

	manager.RefreshIfNewEtag(ctx, "different-etag")
	if endpoint.count() != 1 {
		t.Fatalf("mismatched etag should fetch once, got %d", endpoint.count())
	}
	if !containsSlug(manager.GetRemoteModels(ctx), "etag-new") {
		t.Fatalf("mismatched etag refresh should apply fetched models")
	}
}

// TestStaticManagerAccessors covers the static manager's accessor and no-op
// methods to confirm parity with the trait defaults.
func TestStaticManagerAccessors(t *testing.T) {
	ctx := context.Background()
	auth := apiKeyAuth()
	model := remoteModel(t, "static-model", "Static", 0)
	manager := NewStaticModelsManager(auth, ModelsResponse{Models: []ModelInfo{model}})

	if manager.AuthManager() != auth {
		t.Fatalf("AuthManager should return the configured auth manager")
	}
	if len(manager.ListCollaborationModes()) == 0 {
		t.Fatalf("ListCollaborationModes should return builtin presets")
	}

	got, ok := manager.TryGetRemoteModels()
	if !ok || len(got) != 1 || got[0].Slug != "static-model" {
		t.Fatalf("TryGetRemoteModels = (%v, %v)", got, ok)
	}

	presets, ok := manager.TryListModels()
	if !ok || len(presets) != 1 || presets[0].Model != "static-model" {
		t.Fatalf("TryListModels = (%v, %v)", presetModels(presets), ok)
	}

	// RefreshIfNewEtag and RawModelCatalog are no-ops / pass-throughs.
	manager.RefreshIfNewEtag(ctx, "any")
	catalog := manager.RawModelCatalog(ctx, RefreshOffline)
	if len(catalog.Models) != 1 || catalog.Models[0].Slug != "static-model" {
		t.Fatalf("RawModelCatalog should return the static catalog")
	}
}

// TestOpenAIManagerAccessors covers the OpenAI manager accessor methods.
func TestOpenAIManagerAccessors(t *testing.T) {
	auth := chatgptAuth()
	manager := NewOpenAiModelsManager(t.TempDir(), newFakeEndpoint(nil), auth)

	if manager.AuthManager() != auth {
		t.Fatalf("AuthManager should return the configured auth manager")
	}
	if len(manager.ListCollaborationModes()) == 0 {
		t.Fatalf("ListCollaborationModes should return builtin presets")
	}
}

// TestOpenAIGetDefaultModel covers the OpenAI manager's get_default_model path:
// both the explicit-model short-circuit and the catalog-derived default.
func TestOpenAIGetDefaultModel(t *testing.T) {
	ctx := context.Background()
	models := []ModelInfo{remoteModel(t, "default-pick", "Default Pick", 0)}
	endpoint := newFakeEndpoint([][]ModelInfo{models})
	manager := newOpenAIManagerForTests(t, endpoint)

	provided := "explicit"
	if got := manager.GetDefaultModel(ctx, &provided, RefreshOnline); got != provided {
		t.Fatalf("GetDefaultModel(provided) = %q, want %q", got, provided)
	}

	if got := manager.GetDefaultModel(ctx, nil, RefreshOnline); got != "default-pick" {
		t.Fatalf("GetDefaultModel(nil) = %q, want %q", got, "default-pick")
	}
}
