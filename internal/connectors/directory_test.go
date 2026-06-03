package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// directoryCacheTestLock serializes tests that touch the process-global
// in-memory connector cache, mirroring the Rust CONNECTOR_DIRECTORY_CACHE_TEST_LOCK.
var directoryCacheTestLock sync.Mutex

func clearDirectoryMemoryCache() {
	connectorDirectoryCache.mu.Lock()
	defer connectorDirectoryCache.mu.Unlock()
	connectorDirectoryCache.entry = nil
}

func cacheKey(id string) ConnectorDirectoryCacheKey {
	account := "account-" + id
	user := "user-" + id
	return NewConnectorDirectoryCacheKey("https://chatgpt.example", &account, &user, true)
}

func cacheContext(t *testing.T, id string) ConnectorDirectoryCacheContext {
	t.Helper()
	return NewConnectorDirectoryCacheContext(t.TempDir(), cacheKey(id))
}

func directoryApp(id, name string) DirectoryApp {
	return DirectoryApp{ID: id, Name: name}
}

func TestListAllConnectorsUsesSharedCache(t *testing.T) {
	directoryCacheTestLock.Lock()
	defer directoryCacheTestLock.Unlock()
	clearDirectoryMemoryCache()

	var calls atomic.Int64
	ctx := cacheContext(t, "shared")

	first, err := ListAllConnectorsWithOptions(context.Background(), ctx, false, false,
		func(_ context.Context, _ string) (DirectoryListResponse, error) {
			calls.Add(1)
			return DirectoryListResponse{Apps: []DirectoryApp{directoryApp("alpha", "Alpha")}}, nil
		})
	if err != nil {
		t.Fatalf("first list: %v", err)
	}

	second, err := ListAllConnectorsWithOptions(context.Background(), ctx, false, false,
		func(_ context.Context, _ string) (DirectoryListResponse, error) {
			return DirectoryListResponse{}, errors.New("cache should have been used")
		})
	if err != nil {
		t.Fatalf("second list: %v", err)
	}

	if got := calls.Load(); got != 1 {
		t.Errorf("fetch calls = %d, want 1", got)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("first != second")
	}
}

func TestListAllConnectorsMergesAndNormalizes(t *testing.T) {
	directoryCacheTestLock.Lock()
	defer directoryCacheTestLock.Unlock()
	clearDirectoryMemoryCache()

	var calls atomic.Int64
	ctx := cacheContext(t, "merged")

	connectors, err := ListAllConnectorsWithOptions(context.Background(), ctx, true, true,
		func(_ context.Context, path string) (DirectoryListResponse, error) {
			calls.Add(1)
			if strings.HasPrefix(path, "/connectors/directory/list_workspace") {
				return DirectoryListResponse{Apps: []DirectoryApp{
					{
						ID:          "alpha",
						Name:        "",
						Description: strptr("Merged description"),
						Branding: &appserverproto.AppBranding{
							Category:          strptr("calendar"),
							IsDiscoverableApp: true,
						},
					},
					{ID: "hidden", Name: "Hidden", Visibility: strptr("HIDDEN")},
				}}, nil
			}
			return DirectoryListResponse{Apps: []DirectoryApp{
				directoryApp("alpha", " Alpha "),
				directoryApp("beta", "Beta"),
			}}, nil
		})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if got := calls.Load(); got != 2 {
		t.Errorf("fetch calls = %d, want 2", got)
	}
	if len(connectors) != 2 {
		t.Fatalf("len = %d, want 2", len(connectors))
	}
	if connectors[0].ID != "alpha" || connectors[0].Name != "Alpha" {
		t.Errorf("connector[0] = %q/%q, want alpha/Alpha", connectors[0].ID, connectors[0].Name)
	}
	if connectors[0].Description == nil || *connectors[0].Description != "Merged description" {
		t.Errorf("connector[0].Description = %v, want Merged description", connectors[0].Description)
	}
	if connectors[0].InstallURL == nil || *connectors[0].InstallURL != "https://chatgpt.com/apps/alpha/alpha" {
		t.Errorf("connector[0].InstallURL = %v", connectors[0].InstallURL)
	}
	if connectors[0].Branding == nil || connectors[0].Branding.Category == nil || *connectors[0].Branding.Category != "calendar" {
		t.Errorf("connector[0].Branding.Category mismatch")
	}
	if connectors[1].ID != "beta" || connectors[1].Name != "Beta" {
		t.Errorf("connector[1] = %q/%q, want beta/Beta", connectors[1].ID, connectors[1].Name)
	}
}

func TestCachedDirectoryConnectorsReadsDisk(t *testing.T) {
	directoryCacheTestLock.Lock()
	defer directoryCacheTestLock.Unlock()
	clearDirectoryMemoryCache()

	var calls atomic.Int64
	ctx := cacheContext(t, "disk")

	first, err := ListAllConnectorsWithOptions(context.Background(), ctx, false, false,
		func(_ context.Context, _ string) (DirectoryListResponse, error) {
			calls.Add(1)
			return DirectoryListResponse{Apps: []DirectoryApp{directoryApp("alpha", "Alpha")}}, nil
		})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	clearDirectoryMemoryCache()

	second, ok := CachedDirectoryConnectors(ctx)
	if !ok {
		t.Fatalf("disk cache should load")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("fetch calls = %d, want 1", got)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("first != second")
	}
}

func TestCachedDirectoryConnectorsDropsStaleSchema(t *testing.T) {
	directoryCacheTestLock.Lock()
	defer directoryCacheTestLock.Unlock()
	clearDirectoryMemoryCache()

	ctx := cacheContext(t, "stale-schema")
	cachePath := ctx.CachePath()
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stale, _ := json.MarshalIndent(map[string]any{
		"schema_version": 0,
		"connectors":     []any{},
	}, "", "  ")
	if err := os.WriteFile(cachePath, stale, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, ok := CachedDirectoryConnectors(ctx); ok {
		t.Errorf("stale schema should not load")
	}
	if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stale cache file should be removed")
	}
}

func TestListDirectoryConnectorsPaginates(t *testing.T) {
	directoryCacheTestLock.Lock()
	defer directoryCacheTestLock.Unlock()

	var requested []string
	apps, err := listDirectoryConnectors(context.Background(),
		func(_ context.Context, path string) (DirectoryListResponse, error) {
			requested = append(requested, path)
			if path == "/connectors/directory/list?external_logos=true" {
				return DirectoryListResponse{
					Apps:      []DirectoryApp{directoryApp("alpha", "Alpha")},
					NextToken: strptr("page 2"),
				}, nil
			}
			return DirectoryListResponse{Apps: []DirectoryApp{directoryApp("beta", "Beta")}}, nil
		})
	if err != nil {
		t.Fatalf("listDirectoryConnectors: %v", err)
	}

	gotIDs := []string{apps[0].ID, apps[1].ID}
	if !reflect.DeepEqual(gotIDs, []string{"alpha", "beta"}) {
		t.Errorf("ids = %v, want [alpha beta]", gotIDs)
	}
	wantPaths := []string{
		"/connectors/directory/list?external_logos=true",
		"/connectors/directory/list?token=page%202&external_logos=true",
	}
	if !reflect.DeepEqual(requested, wantPaths) {
		t.Errorf("paths = %v, want %v", requested, wantPaths)
	}
}
