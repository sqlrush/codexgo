package connectors

import (
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// connectorDirectoryDiskCacheSchemaVersion is the on-disk cache schema version;
// entries with a different version are discarded. Rust:
// CONNECTOR_DIRECTORY_DISK_CACHE_SCHEMA_VERSION.
const connectorDirectoryDiskCacheSchemaVersion uint8 = 1

// connectorDirectoryDiskCacheDir is the cache subdirectory under codex_home.
// Rust: CONNECTOR_DIRECTORY_DISK_CACHE_DIR.
const connectorDirectoryDiskCacheDir = "cache/codex_app_directory"

// ConnectorDirectoryCacheContext locates the disk cache for a given account
// directory key. Rust: ConnectorDirectoryCacheContext.
type ConnectorDirectoryCacheContext struct {
	codexHome string
	cacheKey  ConnectorDirectoryCacheKey
}

// NewConnectorDirectoryCacheContext builds a cache context for a codex_home and
// cache key. Rust: ConnectorDirectoryCacheContext::new.
func NewConnectorDirectoryCacheContext(codexHome string, cacheKey ConnectorDirectoryCacheKey) ConnectorDirectoryCacheContext {
	return ConnectorDirectoryCacheContext{codexHome: codexHome, cacheKey: cacheKey}
}

// CacheKey returns the context's cache key.
func (c ConnectorDirectoryCacheContext) CacheKey() ConnectorDirectoryCacheKey {
	return c.cacheKey
}

// CachePath returns the JSON cache file path derived from the SHA1 of the
// serialized cache key. Rust: ConnectorDirectoryCacheContext::cache_path.
func (c ConnectorDirectoryCacheContext) CachePath() string {
	cacheKeyJSON, err := json.Marshal(c.cacheKey)
	if err != nil {
		// Rust uses unwrap_or_default(): an empty string on serialization error.
		cacheKeyJSON = []byte("")
	}
	cacheKeyHash := sha1Hex(string(cacheKeyJSON))
	return filepath.Join(c.codexHome, connectorDirectoryDiskCacheDir, cacheKeyHash+".json")
}

// cachedConnectorDirectoryDiskLoadKind discriminates a disk-load outcome.
type cachedConnectorDirectoryDiskLoadKind int

const (
	diskLoadHit cachedConnectorDirectoryDiskLoadKind = iota
	diskLoadMissing
	diskLoadInvalid
)

// cachedConnectorDirectoryDiskLoad is the result of attempting a disk-cache
// load. Rust: CachedConnectorDirectoryDiskLoad.
type cachedConnectorDirectoryDiskLoad struct {
	kind       cachedConnectorDirectoryDiskLoadKind
	connectors []appserverproto.AppInfo
}

// connectorDirectoryDiskCache is the serialized disk-cache envelope. Rust:
// ConnectorDirectoryDiskCache.
type connectorDirectoryDiskCache struct {
	SchemaVersion uint8                    `json:"schema_version"`
	Connectors    []appserverproto.AppInfo `json:"connectors"`
}

// loadCachedDirectoryConnectorsFromDisk reads, validates, and parses the disk
// cache. A parse failure or schema mismatch removes the file and returns
// Invalid. Rust: load_cached_directory_connectors_from_disk.
func loadCachedDirectoryConnectorsFromDisk(cacheContext ConnectorDirectoryCacheContext) cachedConnectorDirectoryDiskLoad {
	cachePath := cacheContext.CachePath()
	bytes, err := os.ReadFile(cachePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cachedConnectorDirectoryDiskLoad{kind: diskLoadMissing}
		}
		return cachedConnectorDirectoryDiskLoad{kind: diskLoadInvalid}
	}
	var cache connectorDirectoryDiskCache
	if err := json.Unmarshal(bytes, &cache); err != nil {
		_ = os.Remove(cachePath)
		return cachedConnectorDirectoryDiskLoad{kind: diskLoadInvalid}
	}
	if cache.SchemaVersion != connectorDirectoryDiskCacheSchemaVersion {
		_ = os.Remove(cachePath)
		return cachedConnectorDirectoryDiskLoad{kind: diskLoadInvalid}
	}
	return cachedConnectorDirectoryDiskLoad{kind: diskLoadHit, connectors: cache.Connectors}
}

// writeCachedDirectoryConnectorsToDisk persists connectors to the disk cache,
// silently giving up on any IO/serialization failure. Rust:
// write_cached_directory_connectors_to_disk.
func writeCachedDirectoryConnectorsToDisk(cacheContext ConnectorDirectoryCacheContext, connectors []appserverproto.AppInfo) {
	cachePath := cacheContext.CachePath()
	if parent := filepath.Dir(cachePath); parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return
		}
	}
	bytes, err := marshalIndentForCache(connectorDirectoryDiskCache{
		SchemaVersion: connectorDirectoryDiskCacheSchemaVersion,
		Connectors:    connectors,
	})
	if err != nil {
		return
	}
	_ = os.WriteFile(cachePath, bytes, 0o644)
}

// marshalIndentForCache mirrors serde_json::to_vec_pretty: two-space indent.
func marshalIndentForCache(v any) ([]byte, error) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("connectors: encode disk cache: %w", err)
	}
	return out, nil
}

// sha1Hex returns the lowercase hex SHA1 of the input. Rust: sha1_hex.
func sha1Hex(value string) string {
	sum := sha1.Sum([]byte(value))
	return fmt.Sprintf("%x", sum)
}
