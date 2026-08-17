package modelsmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// remoteModel builds a list-visible test ModelInfo. Mirrors the Rust
// `remote_model` test helper (deserialized from JSON to exercise serde defaults).
func remoteModel(t *testing.T, slug, display string, priority int32) ModelInfo {
	t.Helper()
	return remoteModelWithVisibility(t, slug, display, priority, "list")
}

// remoteModelWithVisibility builds a test ModelInfo with the given visibility.
// Mirrors the Rust `remote_model_with_visibility` helper.
func remoteModelWithVisibility(t *testing.T, slug, display string, priority int32, visibility string) ModelInfo {
	t.Helper()
	raw := fmt.Sprintf(`{
		"slug": %q,
		"display_name": %q,
		"description": %q,
		"default_reasoning_level": "medium",
		"supported_reasoning_levels": [{"effort": "low", "description": "low"}, {"effort": "medium", "description": "medium"}],
		"shell_type": "shell_command",
		"visibility": %q,
		"supported_in_api": true,
		"priority": %d,
		"upgrade": null,
		"base_instructions": "base instructions",
		"supports_reasoning_summaries": false,
		"support_verbosity": false,
		"default_verbosity": null,
		"apply_patch_tool_type": null,
		"truncation_policy": {"mode": "bytes", "limit": 10000},
		"supports_parallel_tool_calls": false,
		"supports_image_detail_original": false,
		"context_window": 272000,
		"max_context_window": 272000,
		"experimental_supported_tools": []
	}`, slug, display, display+" desc", visibility, priority)
	var info ModelInfo
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		t.Fatalf("decode remote model: %v", err)
	}
	return info
}

// fakeAuthManager is a test AuthManager controlled directly by the test.
type fakeAuthManager struct {
	mu               sync.Mutex
	mode             *appserverproto.AuthMode
	usesCodexBackend bool
}

func (f *fakeAuthManager) AuthMode() *appserverproto.AuthMode {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mode == nil {
		return nil
	}
	m := *f.mode
	return &m
}

func (f *fakeAuthManager) CurrentAuthUsesCodexBackend() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.usesCodexBackend
}

func (f *fakeAuthManager) setMode(mode *appserverproto.AuthMode, usesCodex bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mode = mode
	f.usesCodexBackend = usesCodex
}

func chatgptAuth() *fakeAuthManager {
	mode := appserverproto.AuthModeChatgpt
	return &fakeAuthManager{mode: &mode, usesCodexBackend: true}
}

func apiKeyAuth() *fakeAuthManager {
	mode := appserverproto.AuthModeApiKey
	return &fakeAuthManager{mode: &mode, usesCodexBackend: false}
}

// fakeEndpoint is a queue-backed ModelsEndpointClient for tests, mirroring the
// Rust TestModelsEndpoint.
type fakeEndpoint struct {
	mu               sync.Mutex
	hasCommandAuth   bool
	usesCodexBackend bool
	responses        [][]ModelInfo
	fetchCount       int
}

func newFakeEndpoint(responses [][]ModelInfo) *fakeEndpoint {
	return &fakeEndpoint{usesCodexBackend: true, responses: responses}
}

func newFakeEndpointWithoutRefresh(responses [][]ModelInfo) *fakeEndpoint {
	return &fakeEndpoint{usesCodexBackend: false, responses: responses}
}

func (e *fakeEndpoint) HasCommandAuth() bool { return e.hasCommandAuth }

func (e *fakeEndpoint) UsesCodexBackend(context.Context) bool { return e.usesCodexBackend }

func (e *fakeEndpoint) ListModels(context.Context, string) ([]ModelInfo, *string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.fetchCount++
	if len(e.responses) == 0 {
		return nil, nil, nil
	}
	models := e.responses[0]
	e.responses = e.responses[1:]
	return models, nil, nil
}

func (e *fakeEndpoint) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.fetchCount
}

func newOpenAIManagerForTests(t *testing.T, endpoint ModelsEndpointClient) *OpenAiModelsManager {
	t.Helper()
	return NewOpenAiModelsManager(t.TempDir(), endpoint, chatgptAuth())
}

func containsSlug(models []ModelInfo, slug string) bool {
	return indexOfSlug(models, slug) >= 0
}

func presetModels(presets []ModelPreset) []string {
	out := make([]string, 0, len(presets))
	for _, p := range presets {
		out = append(out, p.Model)
	}
	return out
}

func TestGetModelInfoTracksFallbackUsage(t *testing.T) {
	ctx := context.Background()
	config := &ModelsManagerConfig{}
	manager := newOpenAIManagerForTests(t, newFakeEndpoint(nil))

	remote := manager.GetRemoteModels(ctx)
	if len(remote) == 0 {
		t.Fatalf("bundled models should include at least one model")
	}
	knownSlug := remote[0].Slug

	known := manager.GetModelInfo(ctx, knownSlug, config)
	if known.UsedFallbackModelMetadata {
		t.Fatalf("known slug should not use fallback metadata")
	}
	if known.Slug != knownSlug {
		t.Fatalf("known slug mismatch: %q", known.Slug)
	}

	unknown := manager.GetModelInfo(ctx, "model-that-does-not-exist", config)
	if !unknown.UsedFallbackModelMetadata {
		t.Fatalf("unknown slug should use fallback metadata")
	}
	if unknown.Slug != "model-that-does-not-exist" {
		t.Fatalf("unknown slug mismatch: %q", unknown.Slug)
	}
}

func TestGetModelInfoUsesCustomCatalog(t *testing.T) {
	ctx := context.Background()
	config := &ModelsManagerConfig{}
	overlay := remoteModel(t, "gpt-overlay", "Overlay", 0)
	overlay.SupportsImageDetailOrig = true

	manager := NewStaticModelsManager(nil, ModelsResponse{Models: []ModelInfo{overlay}})

	info := manager.GetModelInfo(ctx, "gpt-overlay-experiment", config)
	if info.Slug != "gpt-overlay-experiment" {
		t.Fatalf("slug = %q", info.Slug)
	}
	if info.DisplayName != "Overlay" {
		t.Fatalf("display name = %q", info.DisplayName)
	}
	if info.ContextWindow == nil || *info.ContextWindow != 272_000 {
		t.Fatalf("context window mismatch: %v", info.ContextWindow)
	}
	if !info.SupportsImageDetailOrig {
		t.Fatalf("supports_image_detail_original should be true")
	}
	if info.SupportsParallelToolCalls {
		t.Fatalf("supports_parallel_tool_calls should be false")
	}
	if info.UsedFallbackModelMetadata {
		t.Fatalf("should not use fallback metadata")
	}
}

func TestGetModelInfoMatchesNamespacedSuffix(t *testing.T) {
	ctx := context.Background()
	config := &ModelsManagerConfig{}
	remote := remoteModel(t, "gpt-image", "Image", 0)
	remote.SupportsImageDetailOrig = true
	manager := NewStaticModelsManager(nil, ModelsResponse{Models: []ModelInfo{remote}})

	info := manager.GetModelInfo(ctx, "custom/gpt-image", config)
	if info.Slug != "custom/gpt-image" {
		t.Fatalf("slug = %q", info.Slug)
	}
	if !info.SupportsImageDetailOrig {
		t.Fatalf("supports_image_detail_original should be true")
	}
	if info.UsedFallbackModelMetadata {
		t.Fatalf("should not use fallback metadata")
	}
}

func TestGetModelInfoMatchesHyphenatedProviderNamespaceSuffix(t *testing.T) {
	ctx := context.Background()
	config := &ModelsManagerConfig{}
	remote := remoteModel(t, "gpt-image", "Image", 0)
	manager := NewStaticModelsManager(nil, ModelsResponse{Models: []ModelInfo{remote}})

	info := manager.GetModelInfo(ctx, "openai-codex/gpt-image", config)
	if info.Slug != "openai-codex/gpt-image" {
		t.Fatalf("slug = %q", info.Slug)
	}
	if info.UsedFallbackModelMetadata {
		t.Fatalf("should not use fallback metadata")
	}
}

func TestGetModelInfoRejectsMultiSegmentNamespaceSuffix(t *testing.T) {
	ctx := context.Background()
	config := &ModelsManagerConfig{}
	manager := newOpenAIManagerForTests(t, newFakeEndpoint(nil))
	knownSlug := manager.GetRemoteModels(ctx)[0].Slug
	namespaced := "ns1/ns2/" + knownSlug

	info := manager.GetModelInfo(ctx, namespaced, config)
	if info.Slug != namespaced {
		t.Fatalf("slug = %q", info.Slug)
	}
	if !info.UsedFallbackModelMetadata {
		t.Fatalf("multi-segment namespace should fall back to default metadata")
	}
}

func TestRefreshAvailableModelsSortsByPriority(t *testing.T) {
	ctx := context.Background()
	remoteModels := []ModelInfo{
		remoteModel(t, "priority-low", "Low", 1),
		remoteModel(t, "priority-high", "High", 0),
	}
	endpoint := newFakeEndpoint([][]ModelInfo{remoteModels})
	manager := newOpenAIManagerForTests(t, endpoint)

	if err := manager.refreshAvailableModels(ctx, RefreshOnlineIfUncached); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	available := manager.ListModels(ctx, RefreshOnlineIfUncached)
	highIdx, lowIdx := -1, -1
	for i, p := range available {
		switch p.Model {
		case "priority-high":
			highIdx = i
		case "priority-low":
			lowIdx = i
		}
	}
	if highIdx < 0 || lowIdx < 0 {
		t.Fatalf("both priority models should be listed: %v", presetModels(available))
	}
	if highIdx >= lowIdx {
		t.Fatalf("higher priority should be listed first")
	}
	if endpoint.count() != 1 {
		t.Fatalf("expected single fetch, got %d", endpoint.count())
	}
}

func TestRefreshUsesRemoteOnlyCatalogForChatGPT(t *testing.T) {
	ctx := context.Background()
	remoteModels := []ModelInfo{remoteModel(t, "chatgpt-visible", "ChatGPT Visible", 0)}
	endpoint := newFakeEndpoint([][]ModelInfo{remoteModels})
	manager := newOpenAIManagerForTests(t, endpoint)

	if err := manager.refreshAvailableModels(ctx, RefreshOnlineIfUncached); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	got := manager.GetRemoteModels(ctx)
	if len(got) != 1 || got[0].Slug != "chatgpt-visible" {
		t.Fatalf("expected remote-only catalog, got %v", got)
	}
	if endpoint.count() != 1 {
		t.Fatalf("expected single fetch, got %d", endpoint.count())
	}
}

func TestRefreshUsesCachedRemoteOnlyCatalog(t *testing.T) {
	ctx := context.Background()
	codexHome := t.TempDir()
	remoteModels := []ModelInfo{remoteModel(t, "chatgpt-cached", "ChatGPT Cached", 0)}

	fetchEndpoint := newFakeEndpoint([][]ModelInfo{remoteModels})
	fetchManager := NewOpenAiModelsManager(codexHome, fetchEndpoint, chatgptAuth())
	if err := fetchManager.refreshAvailableModels(ctx, RefreshOnlineIfUncached); err != nil {
		t.Fatalf("initial refresh: %v", err)
	}

	cacheEndpoint := newFakeEndpoint(nil)
	cacheManager := NewOpenAiModelsManager(codexHome, cacheEndpoint, chatgptAuth())
	if err := cacheManager.refreshAvailableModels(ctx, RefreshOnlineIfUncached); err != nil {
		t.Fatalf("cached refresh: %v", err)
	}
	got := cacheManager.GetRemoteModels(ctx)
	if len(got) != 1 || got[0].Slug != "chatgpt-cached" {
		t.Fatalf("expected cached remote-only catalog, got %v", got)
	}
	if cacheEndpoint.count() != 0 {
		t.Fatalf("fresh cache should avoid fetch, got %d", cacheEndpoint.count())
	}
}

func TestRefreshPreservesBundledCatalogForEmptyChatGPTRemote(t *testing.T) {
	ctx := context.Background()
	endpoint := newFakeEndpoint([][]ModelInfo{{}})
	manager := newOpenAIManagerForTests(t, endpoint)
	expected := loadRemoteModelsFromFile()

	if err := manager.refreshAvailableModels(ctx, RefreshOnlineIfUncached); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	got := manager.GetRemoteModels(ctx)
	if len(got) != len(expected) {
		t.Fatalf("expected bundled catalog of %d models, got %d", len(expected), len(got))
	}
}

func TestRefreshMergesHiddenOnlyRemoteWithBundled(t *testing.T) {
	ctx := context.Background()
	hidden := remoteModelWithVisibility(t, "chatgpt-hidden-only", "ChatGPT Hidden", 0, "hide")
	endpoint := newFakeEndpoint([][]ModelInfo{{hidden}})
	manager := newOpenAIManagerForTests(t, endpoint)
	bundledCount := len(loadRemoteModelsFromFile())

	if err := manager.refreshAvailableModels(ctx, RefreshOnlineIfUncached); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	got := manager.GetRemoteModels(ctx)
	if len(got) != bundledCount+1 {
		t.Fatalf("expected bundled + 1 hidden model, got %d", len(got))
	}
	if !containsSlug(got, "chatgpt-hidden-only") {
		t.Fatalf("hidden model should be merged into catalog")
	}
}

func TestRefreshKeepsMergingForApiAuth(t *testing.T) {
	ctx := context.Background()
	remoteModels := []ModelInfo{remoteModel(t, "api-auth-visible", "API Auth Visible", 0)}
	endpoint := &fakeEndpoint{hasCommandAuth: true, usesCodexBackend: false, responses: [][]ModelInfo{remoteModels}}
	manager := NewOpenAiModelsManager(t.TempDir(), endpoint, apiKeyAuth())
	bundledCount := len(loadRemoteModelsFromFile())

	if err := manager.refreshAvailableModels(ctx, RefreshOnlineIfUncached); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	got := manager.GetRemoteModels(ctx)
	if len(got) != bundledCount+1 {
		t.Fatalf("expected bundled + 1 remote model, got %d", len(got))
	}
	if !containsSlug(got, "api-auth-visible") {
		t.Fatalf("api remote model should be merged")
	}
	if endpoint.count() != 1 {
		t.Fatalf("expected single fetch, got %d", endpoint.count())
	}
}

func TestRefreshUsesCacheWhenFresh(t *testing.T) {
	ctx := context.Background()
	remoteModels := []ModelInfo{remoteModel(t, "cached", "Cached", 5)}
	endpoint := newFakeEndpoint([][]ModelInfo{remoteModels})
	manager := newOpenAIManagerForTests(t, endpoint)

	if err := manager.refreshAvailableModels(ctx, RefreshOnlineIfUncached); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if !containsSlug(manager.GetRemoteModels(ctx), "cached") {
		t.Fatalf("first refresh should populate cache")
	}
	if err := manager.refreshAvailableModels(ctx, RefreshOnlineIfUncached); err != nil {
		t.Fatalf("cached refresh: %v", err)
	}
	if endpoint.count() != 1 {
		t.Fatalf("cache hit should avoid second fetch, got %d", endpoint.count())
	}
}

func TestRefreshRefetchesWhenCacheStale(t *testing.T) {
	ctx := context.Background()
	initial := []ModelInfo{remoteModel(t, "stale", "Stale", 1)}
	updated := []ModelInfo{remoteModel(t, "fresh", "Fresh", 9)}
	endpoint := newFakeEndpoint([][]ModelInfo{initial, updated})
	manager := newOpenAIManagerForTests(t, endpoint)

	if err := manager.refreshAvailableModels(ctx, RefreshOnlineIfUncached); err != nil {
		t.Fatalf("initial refresh: %v", err)
	}
	if err := manager.cacheManager.manipulateCacheForTest(func(fetchedAt *time.Time) {
		*fetchedAt = time.Now().Add(-time.Hour)
	}); err != nil {
		t.Fatalf("cache manipulation: %v", err)
	}
	if err := manager.refreshAvailableModels(ctx, RefreshOnlineIfUncached); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if !containsSlug(manager.GetRemoteModels(ctx), "fresh") {
		t.Fatalf("stale cache should refetch updated models")
	}
	if endpoint.count() != 2 {
		t.Fatalf("stale cache should fetch twice, got %d", endpoint.count())
	}
}

func TestRefreshRefetchesWhenVersionMismatch(t *testing.T) {
	ctx := context.Background()
	initial := []ModelInfo{remoteModel(t, "old", "Old", 1)}
	updated := []ModelInfo{remoteModel(t, "new", "New", 2)}
	endpoint := newFakeEndpoint([][]ModelInfo{initial, updated})
	manager := newOpenAIManagerForTests(t, endpoint)

	if err := manager.refreshAvailableModels(ctx, RefreshOnlineIfUncached); err != nil {
		t.Fatalf("initial refresh: %v", err)
	}
	if err := manager.cacheManager.mutateCacheForTest(func(cache *modelsCache) {
		mismatch := ClientVersionToWhole() + "-mismatch"
		cache.ClientVersion = &mismatch
	}); err != nil {
		t.Fatalf("cache mutation: %v", err)
	}
	if err := manager.refreshAvailableModels(ctx, RefreshOnlineIfUncached); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if !containsSlug(manager.GetRemoteModels(ctx), "new") {
		t.Fatalf("version mismatch should refetch updated models")
	}
	if endpoint.count() != 2 {
		t.Fatalf("version mismatch should fetch twice, got %d", endpoint.count())
	}
}

func TestRefreshDropsRemovedRemoteModels(t *testing.T) {
	ctx := context.Background()
	initial := []ModelInfo{remoteModel(t, "remote-old", "Remote Old", 1)}
	refreshed := []ModelInfo{remoteModel(t, "remote-new", "Remote New", 1)}
	endpoint := newFakeEndpoint([][]ModelInfo{initial, refreshed})
	manager := newOpenAIManagerForTests(t, endpoint)
	manager.cacheManager.setTTL(0)

	if err := manager.refreshAvailableModels(ctx, RefreshOnlineIfUncached); err != nil {
		t.Fatalf("initial refresh: %v", err)
	}
	if err := manager.refreshAvailableModels(ctx, RefreshOnlineIfUncached); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	available, ok := manager.TryListModels()
	if !ok {
		t.Fatalf("models should be available")
	}
	models := presetModels(available)
	if !containsString(models, "remote-new") {
		t.Fatalf("new remote model should be listed")
	}
	if containsString(models, "remote-old") {
		t.Fatalf("removed remote model should not be listed")
	}
	if endpoint.count() != 2 {
		t.Fatalf("zero TTL should fetch twice, got %d", endpoint.count())
	}
}

func TestRefreshSkipsNetworkWithoutChatGPTAuth(t *testing.T) {
	ctx := context.Background()
	dynamicSlug := "dynamic-model-only-for-test-noauth"
	endpoint := newFakeEndpointWithoutRefresh([][]ModelInfo{{remoteModel(t, dynamicSlug, "No Auth", 1)}})
	manager := NewOpenAiModelsManager(t.TempDir(), endpoint, nil)

	if err := manager.refreshAvailableModels(ctx, RefreshOnline); err != nil {
		t.Fatalf("refresh should no-op: %v", err)
	}
	if containsSlug(manager.GetRemoteModels(ctx), dynamicSlug) {
		t.Fatalf("remote refresh should be skipped without chatgpt auth")
	}
	if endpoint.count() != 0 {
		t.Fatalf("endpoint that cannot refresh should avoid fetches, got %d", endpoint.count())
	}
}

func TestBuildAvailableModelsPicksDefaultAfterHiding(t *testing.T) {
	manager := NewStaticModelsManager(nil, ModelsResponse{Models: nil})

	hidden := remoteModelWithVisibility(t, "hidden", "Hidden", 0, "hide")
	visible := remoteModelWithVisibility(t, "visible", "Visible", 1, "list")

	expectedHidden := modelPresetFromInfo(hidden)
	expectedVisible := modelPresetFromInfo(visible)
	expectedVisible.IsDefault = true

	available := buildAvailableModels(manager.authManager, []ModelInfo{hidden, visible})
	if len(available) != 2 {
		t.Fatalf("expected 2 presets, got %d", len(available))
	}
	if available[0].Model != expectedHidden.Model || available[0].IsDefault {
		t.Fatalf("hidden preset should not be default: %+v", available[0])
	}
	if available[1].Model != expectedVisible.Model || !available[1].IsDefault {
		t.Fatalf("visible preset should be default: %+v", available[1])
	}
}

func TestStaticManagerReadsLatestAuthMode(t *testing.T) {
	ctx := context.Background()
	auth := chatgptAuth()
	chatgptOnly := remoteModel(t, "chatgpt-only", "ChatGPT Only", 0)
	chatgptOnly.SupportedInAPI = false
	apiModel := remoteModel(t, "api-model", "API Model", 1)
	manager := NewStaticModelsManager(auth, ModelsResponse{Models: []ModelInfo{chatgptOnly, apiModel}})

	chatgptModels := presetModels(manager.ListModels(ctx, RefreshOnline))
	if len(chatgptModels) != 2 || chatgptModels[0] != "chatgpt-only" || chatgptModels[1] != "api-model" {
		t.Fatalf("chatgpt mode should list both models, got %v", chatgptModels)
	}

	mode := appserverproto.AuthModeApiKey
	auth.setMode(&mode, false)
	apiModels := presetModels(manager.ListModels(ctx, RefreshOnline))
	if len(apiModels) != 1 || apiModels[0] != "api-model" {
		t.Fatalf("api mode should list only api-model, got %v", apiModels)
	}
}

func TestRefreshIfNewEtagRenewsOnMatch(t *testing.T) {
	ctx := context.Background()
	etag := "etag-1"
	models := []ModelInfo{remoteModel(t, "etag-model", "Etag", 0)}
	endpoint := newFakeEndpoint([][]ModelInfo{models})
	endpoint.responses = [][]ModelInfo{models}
	manager := newOpenAIManagerForTests(t, endpoint)
	// Seed an etag by fetching with one.
	manager.setEtag(&etag)
	manager.applyRemoteModels(models)
	// Persist cache so renew has a file to update.
	if err := manager.cacheManager.persistCache(models, &etag, ClientVersionToWhole()); err != nil {
		t.Fatalf("persist cache: %v", err)
	}

	manager.RefreshIfNewEtag(ctx, etag)
	if endpoint.count() != 0 {
		t.Fatalf("matching etag should not fetch, got %d", endpoint.count())
	}
}

func TestBundledModelsJSONRoundtrips(t *testing.T) {
	response, err := BundledModelsResponse()
	if err != nil {
		t.Fatalf("bundled models.json should parse: %v", err)
	}
	serialized, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	var roundtripped ModelsResponse
	if err := json.Unmarshal(serialized, &roundtripped); err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	if len(roundtripped.Models) != len(response.Models) {
		t.Fatalf("roundtrip model count mismatch: %d vs %d", len(roundtripped.Models), len(response.Models))
	}
	if len(response.Models) == 0 {
		t.Fatalf("bundled models.json should contain at least one model")
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
