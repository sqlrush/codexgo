package chatgpt

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	// WorkspaceSettingsTimeout bounds a workspace-settings fetch.
	WorkspaceSettingsTimeout = 10 * time.Second
	// WorkspaceSettingsCacheTTL is how long a cached result is reused.
	WorkspaceSettingsCacheTTL = 15 * time.Minute
	// codexPluginsBetaSetting is the beta-settings key for plugins.
	codexPluginsBetaSetting = "enable_plugins"
)

// workspaceSettingsResponse mirrors the Rust `WorkspaceSettingsResponse`.
type workspaceSettingsResponse struct {
	BetaSettings map[string]bool `json:"beta_settings"`
}

// WorkspaceSettingsCacheKey identifies a cached workspace-settings entry. It
// mirrors the Rust `WorkspaceSettingsCacheKey`.
type WorkspaceSettingsCacheKey struct {
	ChatgptBaseURL string
	AccountID      string
}

// cachedWorkspaceSettings mirrors the Rust `CachedWorkspaceSettings`.
type cachedWorkspaceSettings struct {
	key                 WorkspaceSettingsCacheKey
	expiresAt           time.Time
	codexPluginsEnabled bool
}

// WorkspaceSettingsCache is a single-entry TTL cache of workspace settings. It
// mirrors the Rust `WorkspaceSettingsCache`.
type WorkspaceSettingsCache struct {
	mu    sync.RWMutex
	entry *cachedWorkspaceSettings
}

// NewWorkspaceSettingsCache returns an empty cache.
func NewWorkspaceSettingsCache() *WorkspaceSettingsCache {
	return &WorkspaceSettingsCache{}
}

func (c *WorkspaceSettingsCache) getCodexPluginsEnabled(key WorkspaceSettingsCacheKey) (bool, bool) {
	c.mu.RLock()
	if c.entry != nil && time.Now().Before(c.entry.expiresAt) && c.entry.key == key {
		enabled := c.entry.codexPluginsEnabled
		c.mu.RUnlock()
		return enabled, true
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entry != nil && (time.Now().After(c.entry.expiresAt) || c.entry.key != key) {
		c.entry = nil
	}
	return false, false
}

func (c *WorkspaceSettingsCache) setCodexPluginsEnabled(key WorkspaceSettingsCacheKey, enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entry = &cachedWorkspaceSettings{
		key:                 key,
		expiresAt:           time.Now().Add(WorkspaceSettingsCacheTTL),
		codexPluginsEnabled: enabled,
	}
}

// CodexPluginsEnabledForWorkspace reports whether the Codex plugins beta is
// enabled for the workspace. It mirrors the Rust
// `codex_plugins_enabled_for_workspace`: non-workspace accounts default to true,
// and a missing beta setting defaults to true.
func CodexPluginsEnabledForWorkspace(ctx context.Context, session Session, cache *WorkspaceSettingsCache) (bool, error) {
	auth := session.Auth
	if auth == nil {
		return true, nil
	}
	if !auth.IsChatgptAuth() {
		return true, nil
	}
	tokenData, err := auth.GetTokenData()
	if err != nil {
		return false, fmt.Errorf("ChatGPT token data is not available: %w", err)
	}
	if !tokenData.IDToken.IsWorkspaceAccount() {
		return true, nil
	}
	accountID := ""
	if tokenData.AccountID != nil {
		accountID = *tokenData.AccountID
	}
	if accountID == "" {
		return true, nil
	}

	cacheKey := WorkspaceSettingsCacheKey{
		ChatgptBaseURL: session.ChatgptBaseURL,
		AccountID:      accountID,
	}
	if cache != nil {
		if enabled, ok := cache.getCodexPluginsEnabled(cacheKey); ok {
			return enabled, nil
		}
	}

	encoded := encodePathSegment(accountID)
	var settings workspaceSettingsResponse
	if err := session.GetWithTimeout(ctx,
		fmt.Sprintf("/accounts/%s/settings", encoded),
		WorkspaceSettingsTimeout,
		&settings,
	); err != nil {
		return false, err
	}

	codexPluginsEnabled := true
	if v, ok := settings.BetaSettings[codexPluginsBetaSetting]; ok {
		codexPluginsEnabled = v
	}

	if cache != nil {
		cache.setCodexPluginsEnabled(cacheKey, codexPluginsEnabled)
	}
	return codexPluginsEnabled, nil
}

// encodePathSegment percent-encodes a path segment, leaving unreserved ASCII
// characters intact. It mirrors the Rust `encode_path_segment`.
func encodePathSegment(value string) string {
	var b []byte
	for i := 0; i < len(value); i++ {
		c := value[i]
		if isUnreservedPathByte(c) {
			b = append(b, c)
		} else {
			b = append(b, '%')
			b = append(b, upperHex(c>>4), upperHex(c&0x0f))
		}
	}
	return string(b)
}

func isUnreservedPathByte(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	case c == '-' || c == '.' || c == '_' || c == '~':
		return true
	default:
		return false
	}
}

func upperHex(nibble byte) byte {
	if nibble < 10 {
		return '0' + nibble
	}
	return 'A' + (nibble - 10)
}
