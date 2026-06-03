package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// urlencode percent-encodes a string the way the Rust urlencoding::encode crate
// does: every byte except the RFC 3986 unreserved set (ALPHA / DIGIT / '-' /
// '.' / '_' / '~') is rendered as %XX with uppercase hex. Notably, a space
// becomes %20 (not '+', as url.QueryEscape would produce).
func urlencode(s string) string {
	const upperhex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isUnreservedURLByte(c) {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(upperhex[c>>4])
			b.WriteByte(upperhex[c&0x0F])
		}
	}
	return b.String()
}

func isUnreservedURLByte(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z':
		return true
	case c >= 'a' && c <= 'z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '-' || c == '.' || c == '_' || c == '~':
		return true
	default:
		return false
	}
}

// ConnectorsCacheTTL is the in-memory directory cache lifetime. Rust:
// CONNECTORS_CACHE_TTL (3600s).
const ConnectorsCacheTTL = time.Hour

// ConnectorDirectoryCacheKey identifies a connector directory cache entry by
// account context. Rust: ConnectorDirectoryCacheKey (snake_case serde).
type ConnectorDirectoryCacheKey struct {
	ChatgptBaseURL     string  `json:"chatgpt_base_url"`
	AccountID          *string `json:"account_id"`
	ChatgptUserID      *string `json:"chatgpt_user_id"`
	IsWorkspaceAccount bool    `json:"is_workspace_account"`
}

// NewConnectorDirectoryCacheKey builds a cache key. Rust:
// ConnectorDirectoryCacheKey::new.
func NewConnectorDirectoryCacheKey(chatgptBaseURL string, accountID, chatgptUserID *string, isWorkspaceAccount bool) ConnectorDirectoryCacheKey {
	return ConnectorDirectoryCacheKey{
		ChatgptBaseURL:     chatgptBaseURL,
		AccountID:          accountID,
		ChatgptUserID:      chatgptUserID,
		IsWorkspaceAccount: isWorkspaceAccount,
	}
}

// cachedConnectorDirectory is the in-memory cache entry. Rust:
// CachedConnectorDirectory.
type cachedConnectorDirectory struct {
	key        ConnectorDirectoryCacheKey
	expiresAt  time.Time
	connectors []appserverproto.AppInfo
}

// connectorDirectoryCache is the process-wide in-memory cache (a single slot,
// matching the Rust LazyLock<Mutex<Option<..>>>).
var connectorDirectoryCache struct {
	mu    sync.Mutex
	entry *cachedConnectorDirectory
}

// DirectoryListResponse is one page of directory apps plus a pagination token.
// Rust: DirectoryListResponse (nextToken alias).
type DirectoryListResponse struct {
	Apps      []DirectoryApp `json:"apps"`
	NextToken *string        `json:"next_token"`
}

// UnmarshalJSON accepts the snake_case next_token and the camelCase nextToken
// alias, matching the Rust #[serde(alias = "nextToken")].
func (d *DirectoryListResponse) UnmarshalJSON(data []byte) error {
	var w struct {
		Apps          []DirectoryApp `json:"apps"`
		NextToken     *string        `json:"next_token"`
		NextTokenCaml *string        `json:"nextToken"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return fmt.Errorf("connectors: decode DirectoryListResponse: %w", err)
	}
	d.Apps = w.Apps
	d.NextToken = w.NextToken
	if d.NextToken == nil {
		d.NextToken = w.NextTokenCaml
	}
	return nil
}

// DirectoryApp is one app from the connectors directory feed. Rust:
// DirectoryApp; all camelCase keys are accepted as input aliases.
type DirectoryApp struct {
	ID                  string
	Name                string
	Description         *string
	AppMetadata         *appserverproto.AppMetadata
	Branding            *appserverproto.AppBranding
	Labels              *map[string]string
	LogoURL             *string
	LogoURLDark         *string
	DistributionChannel *string
	Visibility          *string
}

// UnmarshalJSON accepts both snake_case and the documented camelCase aliases.
func (d *DirectoryApp) UnmarshalJSON(data []byte) error {
	var w struct {
		ID                  string                      `json:"id"`
		Name                string                      `json:"name"`
		Description         *string                     `json:"description"`
		AppMetadata         *appserverproto.AppMetadata `json:"app_metadata"`
		AppMetadataCaml     *appserverproto.AppMetadata `json:"appMetadata"`
		Branding            *appserverproto.AppBranding `json:"branding"`
		Labels              *map[string]string          `json:"labels"`
		LogoURL             *string                     `json:"logo_url"`
		LogoURLCaml         *string                     `json:"logoUrl"`
		LogoURLDark         *string                     `json:"logo_url_dark"`
		LogoURLDarkCaml     *string                     `json:"logoUrlDark"`
		DistributionChannel *string                     `json:"distribution_channel"`
		DistributionCaml    *string                     `json:"distributionChannel"`
		Visibility          *string                     `json:"visibility"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return fmt.Errorf("connectors: decode DirectoryApp: %w", err)
	}
	d.ID = w.ID
	d.Name = w.Name
	d.Description = w.Description
	d.AppMetadata = firstNonNilMetadata(w.AppMetadata, w.AppMetadataCaml)
	d.Branding = w.Branding
	d.Labels = w.Labels
	d.LogoURL = firstNonNilString(w.LogoURL, w.LogoURLCaml)
	d.LogoURLDark = firstNonNilString(w.LogoURLDark, w.LogoURLDarkCaml)
	d.DistributionChannel = firstNonNilString(w.DistributionChannel, w.DistributionCaml)
	d.Visibility = w.Visibility
	return nil
}

func firstNonNilString(a, b *string) *string {
	if a != nil {
		return a
	}
	return b
}

func firstNonNilMetadata(a, b *appserverproto.AppMetadata) *appserverproto.AppMetadata {
	if a != nil {
		return a
	}
	return b
}

// FetchPage fetches one directory page by request path. Rust: the FnMut(String)
// -> Future closure injected into list_all_connectors_with_options.
type FetchPage func(ctx context.Context, path string) (DirectoryListResponse, error)

// CachedDirectoryConnectors returns directory connectors from the in-memory
// cache or, failing that, the disk cache (warming the in-memory cache with a
// zero TTL). Rust: cached_directory_connectors.
func CachedDirectoryConnectors(cacheContext ConnectorDirectoryCacheContext) ([]appserverproto.AppInfo, bool) {
	if cached, ok := cachedDirectoryConnectorsInMemory(cacheContext.cacheKey); ok {
		return cached, true
	}
	load := loadCachedDirectoryConnectorsFromDisk(cacheContext)
	if load.kind != diskLoadHit {
		return nil, false
	}
	writeCachedDirectoryConnectorsInMemory(cacheContext.cacheKey, load.connectors, 0)
	return load.connectors, true
}

// cachedDirectoryConnectorsInMemory returns the in-memory entry for a key,
// ignoring expiry. Rust: cached_directory_connectors_in_memory.
func cachedDirectoryConnectorsInMemory(cacheKey ConnectorDirectoryCacheKey) ([]appserverproto.AppInfo, bool) {
	connectorDirectoryCache.mu.Lock()
	defer connectorDirectoryCache.mu.Unlock()
	entry := connectorDirectoryCache.entry
	if entry == nil || !cacheKeyEqual(entry.key, cacheKey) {
		return nil, false
	}
	return cloneConnectors(entry.connectors), true
}

// unexpiredDirectoryConnectorsInMemory returns the in-memory entry only when it
// matches and is unexpired. Rust: unexpired_directory_connectors_in_memory.
func unexpiredDirectoryConnectorsInMemory(cacheKey ConnectorDirectoryCacheKey) ([]appserverproto.AppInfo, bool) {
	connectorDirectoryCache.mu.Lock()
	defer connectorDirectoryCache.mu.Unlock()
	entry := connectorDirectoryCache.entry
	if entry == nil {
		return nil, false
	}
	if cacheKeyEqual(entry.key, cacheKey) && time.Now().Before(entry.expiresAt) {
		return cloneConnectors(entry.connectors), true
	}
	return nil, false
}

// ListAllConnectorsWithOptions fetches, merges, normalizes, sorts, and caches
// the connectors directory. Rust: list_all_connectors_with_options.
func ListAllConnectorsWithOptions(
	ctx context.Context,
	cacheContext ConnectorDirectoryCacheContext,
	isWorkspaceAccount bool,
	forceRefetch bool,
	fetchPage FetchPage,
) ([]appserverproto.AppInfo, error) {
	if !forceRefetch {
		if cached, ok := unexpiredDirectoryConnectorsInMemory(cacheContext.cacheKey); ok {
			return cached, nil
		}
	}

	apps, err := listDirectoryConnectors(ctx, fetchPage)
	if err != nil {
		return nil, err
	}
	if isWorkspaceAccount {
		workspaceApps, err := listWorkspaceConnectors(ctx, fetchPage)
		if err != nil {
			return nil, err
		}
		apps = append(apps, workspaceApps...)
	}

	mergedApps := mergeDirectoryApps(apps)
	connectors := make([]appserverproto.AppInfo, 0, len(mergedApps))
	for _, app := range mergedApps {
		connectors = append(connectors, directoryAppToAppInfo(app))
	}
	for i := range connectors {
		var installURL string
		if connectors[i].InstallURL != nil {
			installURL = *connectors[i].InstallURL
		} else {
			installURL = ConnectorInstallURL(connectors[i].Name, connectors[i].ID)
		}
		connectors[i].Name = normalizeConnectorName(connectors[i].Name, connectors[i].ID)
		connectors[i].Description = normalizeConnectorValue(connectors[i].Description)
		connectors[i].InstallURL = &installURL
		connectors[i].IsAccessible = false
	}
	sort.SliceStable(connectors, func(i, j int) bool {
		if connectors[i].Name != connectors[j].Name {
			return connectors[i].Name < connectors[j].Name
		}
		return connectors[i].ID < connectors[j].ID
	})
	writeCachedDirectoryConnectors(cacheContext, connectors)
	return connectors, nil
}

// writeCachedDirectoryConnectors stores connectors to both caches. Rust:
// write_cached_directory_connectors.
func writeCachedDirectoryConnectors(cacheContext ConnectorDirectoryCacheContext, connectors []appserverproto.AppInfo) {
	writeCachedDirectoryConnectorsInMemory(cacheContext.cacheKey, connectors, ConnectorsCacheTTL)
	writeCachedDirectoryConnectorsToDisk(cacheContext, connectors)
}

// writeCachedDirectoryConnectorsInMemory replaces the in-memory cache slot.
// Rust: write_cached_directory_connectors_in_memory.
func writeCachedDirectoryConnectorsInMemory(cacheKey ConnectorDirectoryCacheKey, connectors []appserverproto.AppInfo, ttl time.Duration) {
	connectorDirectoryCache.mu.Lock()
	defer connectorDirectoryCache.mu.Unlock()
	connectorDirectoryCache.entry = &cachedConnectorDirectory{
		key:        cacheKey,
		expiresAt:  time.Now().Add(ttl),
		connectors: cloneConnectors(connectors),
	}
}

// listDirectoryConnectors paginates the directory listing endpoint, dropping
// hidden apps. Rust: list_directory_connectors.
func listDirectoryConnectors(ctx context.Context, fetchPage FetchPage) ([]DirectoryApp, error) {
	var apps []DirectoryApp
	var nextToken *string
	for {
		var path string
		if nextToken != nil {
			encodedToken := urlencode(*nextToken)
			path = fmt.Sprintf("/connectors/directory/list?token=%s&external_logos=true", encodedToken)
		} else {
			path = "/connectors/directory/list?external_logos=true"
		}
		response, err := fetchPage(ctx, path)
		if err != nil {
			return nil, err
		}
		for _, app := range response.Apps {
			if !isHiddenDirectoryApp(app) {
				apps = append(apps, app)
			}
		}
		nextToken = trimToken(response.NextToken)
		if nextToken == nil {
			break
		}
	}
	return apps, nil
}

// trimToken trims a pagination token and discards it when empty. Mirrors the
// Rust map/filter chain.
func trimToken(token *string) *string {
	if token == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*token)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// listWorkspaceConnectors fetches the single workspace listing page, swallowing
// any error to an empty result. Rust: list_workspace_connectors.
func listWorkspaceConnectors(ctx context.Context, fetchPage FetchPage) ([]DirectoryApp, error) {
	response, err := fetchPage(ctx, "/connectors/directory/list_workspace?external_logos=true")
	if err != nil {
		return nil, nil
	}
	apps := make([]DirectoryApp, 0, len(response.Apps))
	for _, app := range response.Apps {
		if !isHiddenDirectoryApp(app) {
			apps = append(apps, app)
		}
	}
	return apps, nil
}

// isHiddenDirectoryApp reports whether an app is hidden. Rust:
// is_hidden_directory_app.
func isHiddenDirectoryApp(app DirectoryApp) bool {
	return app.Visibility != nil && *app.Visibility == "HIDDEN"
}

// directoryAppToAppInfo converts a directory app into an AppInfo. Rust:
// directory_app_to_app_info.
func directoryAppToAppInfo(app DirectoryApp) appserverproto.AppInfo {
	return appserverproto.AppInfo{
		ID:                  app.ID,
		Name:                app.Name,
		Description:         app.Description,
		LogoURL:             app.LogoURL,
		LogoURLDark:         app.LogoURLDark,
		DistributionChannel: app.DistributionChannel,
		Branding:            app.Branding,
		AppMetadata:         app.AppMetadata,
		Labels:              app.Labels,
		InstallURL:          nil,
		IsAccessible:        false,
		IsEnabled:           true,
		PluginDisplayNames:  []string{},
	}
}

// cacheKeyEqual compares two cache keys by value (the Rust derive(Eq)).
func cacheKeyEqual(a, b ConnectorDirectoryCacheKey) bool {
	return a.ChatgptBaseURL == b.ChatgptBaseURL &&
		ptrStringEqual(a.AccountID, b.AccountID) &&
		ptrStringEqual(a.ChatgptUserID, b.ChatgptUserID) &&
		a.IsWorkspaceAccount == b.IsWorkspaceAccount
}

func ptrStringEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// cloneConnectors deep-copies the connector slice so cached entries are immune
// to caller mutation (the Rust cache stores owned Vec<AppInfo> clones).
func cloneConnectors(connectors []appserverproto.AppInfo) []appserverproto.AppInfo {
	if connectors == nil {
		return nil
	}
	out := make([]appserverproto.AppInfo, len(connectors))
	copy(out, connectors)
	return out
}
