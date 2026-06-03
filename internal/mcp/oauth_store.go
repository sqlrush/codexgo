package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/keyring"
)

// keyringService is the keyring service name under which MCP OAuth credentials
// are stored. It matches the reference KEYRING_SERVICE constant exactly so
// credentials written by codex are readable here and vice versa.
const keyringService = "Codex MCP Credentials"

// fallbackFilename is the on-disk credential store filename within CODEX_HOME,
// used when the keyring is unavailable. It matches the reference constant.
const fallbackFilename = ".credentials.json"

// mcpServerType is the constant "type" value baked into the store-key payload.
const mcpServerType = "http"

// refreshSkewMillis is the lead time before expiry at which a token is considered
// in need of refresh, matching the reference REFRESH_SKEW_MILLIS.
const refreshSkewMillis uint64 = 30_000

// OAuthStore persists MCP OAuth tokens to the system keyring with a CODEX_HOME
// file fallback, faithfully porting the reference oauth.rs storage behavior. The
// store mode selects the backend: Auto prefers keyring then file, Keyring uses
// only the keyring, File uses only the fallback file.
type OAuthStore struct {
	keyring   keyring.Store
	codexHome string
}

// NewOAuthStore builds a store using the system keyring and the given CODEX_HOME
// for the fallback file. When codexHome is empty it is resolved from the
// environment on demand.
func NewOAuthStore(codexHome string) *OAuthStore {
	return &OAuthStore{keyring: keyring.NewDefaultStore(), codexHome: codexHome}
}

// NewOAuthStoreWith builds a store with an explicit keyring backend, primarily
// for tests. A nil keyring uses the system keyring.
func NewOAuthStoreWith(store keyring.Store, codexHome string) *OAuthStore {
	if store == nil {
		store = keyring.NewDefaultStore()
	}
	return &OAuthStore{keyring: store, codexHome: codexHome}
}

// Load returns the stored tokens for (serverName, url) under the given store
// mode, or (nil, nil) when none exist. Loaded tokens have their expires_in
// recomputed from the persisted expires_at, matching the reference.
func (s *OAuthStore) Load(serverName, url string, mode config.OAuthCredentialsStoreMode) (*StoredOAuthTokens, error) {
	switch mode {
	case config.OAuthCredentialsStoreFile:
		return s.loadFromFile(serverName, url)
	case config.OAuthCredentialsStoreKeyring:
		tokens, err := s.loadFromKeyring(serverName, url)
		if err != nil {
			return nil, fmt.Errorf("mcp: failed to read OAuth tokens from keyring: %w", err)
		}
		return tokens, nil
	default: // Auto
		tokens, err := s.loadFromKeyring(serverName, url)
		if err == nil && tokens != nil {
			return tokens, nil
		}
		// Missing or keyring failure: fall back to the file.
		return s.loadFromFile(serverName, url)
	}
}

// Has reports whether tokens exist for (serverName, url) under the given mode.
func (s *OAuthStore) Has(serverName, url string, mode config.OAuthCredentialsStoreMode) (bool, error) {
	tokens, err := s.Load(serverName, url, mode)
	if err != nil {
		return false, err
	}
	return tokens != nil, nil
}

// Save persists tokens under the given store mode. Under Auto the keyring is
// preferred with a file fallback; a successful keyring write removes any stale
// fallback entry.
func (s *OAuthStore) Save(tokens StoredOAuthTokens, mode config.OAuthCredentialsStoreMode) error {
	switch mode {
	case config.OAuthCredentialsStoreFile:
		return s.saveToFile(tokens)
	case config.OAuthCredentialsStoreKeyring:
		return s.saveToKeyring(tokens)
	default: // Auto
		if err := s.saveToKeyring(tokens); err != nil {
			return s.saveToFile(tokens)
		}
		return nil
	}
}

// Delete removes tokens for (serverName, url) from both the keyring and the
// fallback file, reporting whether anything was removed. It mirrors the
// reference delete_oauth_tokens_from_keyring_and_file behavior, including the
// keyring-error propagation rules per store mode.
func (s *OAuthStore) Delete(serverName, url string, mode config.OAuthCredentialsStoreMode) (bool, error) {
	key, err := computeStoreKey(serverName, url)
	if err != nil {
		return false, err
	}

	keyringRemoved, kerr := s.keyring.Delete(keyringService, key)
	if kerr != nil {
		switch mode {
		case config.OAuthCredentialsStoreAuto, config.OAuthCredentialsStoreKeyring:
			return false, fmt.Errorf("mcp: failed to delete OAuth tokens from keyring: %w", kerr)
		default:
			keyringRemoved = false
		}
	}

	fileRemoved, ferr := s.deleteFromFile(key)
	if ferr != nil {
		return false, ferr
	}
	return keyringRemoved || fileRemoved, nil
}

// loadFromKeyring reads and deserializes tokens from the keyring, recomputing
// expires_in from the stored expires_at.
func (s *OAuthStore) loadFromKeyring(serverName, url string) (*StoredOAuthTokens, error) {
	key, err := computeStoreKey(serverName, url)
	if err != nil {
		return nil, err
	}
	serialized, err := s.keyring.Load(keyringService, key)
	if err != nil {
		return nil, err
	}
	if serialized == nil {
		return nil, nil
	}
	var tokens StoredOAuthTokens
	if err := json.Unmarshal([]byte(*serialized), &tokens); err != nil {
		return nil, fmt.Errorf("mcp: failed to deserialize OAuth tokens from keyring: %w", err)
	}
	refreshExpiresInFromTimestamp(&tokens, time.Now())
	return &tokens, nil
}

// saveToKeyring serializes and stores tokens in the keyring, removing any stale
// fallback file entry on success.
func (s *OAuthStore) saveToKeyring(tokens StoredOAuthTokens) error {
	serialized, err := json.Marshal(tokens)
	if err != nil {
		return fmt.Errorf("mcp: failed to serialize OAuth tokens: %w", err)
	}
	key, err := computeStoreKey(tokens.ServerName, tokens.URL)
	if err != nil {
		return err
	}
	if err := s.keyring.Save(keyringService, key, string(serialized)); err != nil {
		return fmt.Errorf("mcp: failed to write OAuth tokens to keyring: %w", err)
	}
	// Best effort: remove a stale fallback entry.
	_, _ = s.deleteFromFile(key)
	return nil
}

// refreshExpiresInFromTimestamp recomputes the token's expires_in from its
// persisted expires_at relative to now. A past expiry clears expires_in. This
// mirrors the reference refresh_expires_in_from_timestamp.
func refreshExpiresInFromTimestamp(tokens *StoredOAuthTokens, now time.Time) {
	if tokens.ExpiresAt == nil {
		return
	}
	seconds, ok := expiresInFromTimestamp(*tokens.ExpiresAt, now)
	if !ok {
		tokens.TokenResponse.ExpiresIn = nil
		return
	}
	v := seconds
	tokens.TokenResponse.ExpiresIn = &v
}

// expiresInFromTimestamp converts an absolute expiry (epoch ms) into remaining
// seconds relative to now, reporting false when already expired.
func expiresInFromTimestamp(expiresAt uint64, now time.Time) (uint64, bool) {
	nowMs := uint64(now.UnixMilli())
	if expiresAt <= nowMs {
		return 0, false
	}
	return (expiresAt - nowMs) / 1000, true
}

// TokenNeedsRefresh reports whether a token with the given absolute expiry
// (epoch ms) is within the refresh skew window of expiring, relative to now.
func TokenNeedsRefresh(expiresAt *uint64, now time.Time) bool {
	if expiresAt == nil {
		return false
	}
	nowMs := uint64(now.UnixMilli())
	return nowMs+refreshSkewMillis >= *expiresAt
}

// computeStoreKey derives the storage key for (serverName, serverURL). It hashes
// the canonical JSON payload {"type","url","headers"} with SHA-256, takes the
// first 16 hex characters, and returns "serverName|hash". This matches the
// reference compute_store_key byte-for-byte.
func computeStoreKey(serverName, serverURL string) (string, error) {
	// The payload is serialized with serde_json::Map, which preserves insertion
	// order; the reference inserts type, url, headers in that order. We
	// reproduce that exact ordering rather than sorting.
	payload := `{"type":` + mustJSONString(mcpServerType) +
		`,"url":` + mustJSONString(serverURL) +
		`,"headers":{}}`
	sum := sha256.Sum256([]byte(payload))
	full := hex.EncodeToString(sum[:])
	truncated := full[:16]
	return serverName + "|" + truncated, nil
}

// mustJSONString JSON-encodes s as a string literal. Encoding a string never
// fails, so the result is returned directly.
func mustJSONString(s string) string {
	encoded, _ := json.Marshal(s)
	return string(encoded)
}

// fallbackTokenEntry is one record in the fallback credentials file. Field names
// and serialization match the reference FallbackTokenEntry exactly: expires_at
// and refresh_token use serde `default` only (no skip), so they serialize as
// `null` when absent; scopes always serializes as an array.
type fallbackTokenEntry struct {
	ServerName   string   `json:"server_name"`
	ServerURL    string   `json:"server_url"`
	ClientID     string   `json:"client_id"`
	AccessToken  string   `json:"access_token"`
	ExpiresAt    *uint64  `json:"expires_at"`
	RefreshToken *string  `json:"refresh_token"`
	Scopes       []string `json:"scopes"`
}

// fallbackFilePath returns the path to the fallback credentials file within
// CODEX_HOME.
func (s *OAuthStore) fallbackFilePath() (string, error) {
	home := s.codexHome
	if home == "" {
		resolved, err := config.FindCodexHome()
		if err != nil {
			return "", fmt.Errorf("mcp: resolve CODEX_HOME: %w", err)
		}
		home = resolved
	}
	return filepath.Join(home, fallbackFilename), nil
}

// readFallbackFile reads and parses the fallback credentials file, returning nil
// when it does not exist.
func (s *OAuthStore) readFallbackFile() (map[string]fallbackTokenEntry, error) {
	path, err := s.fallbackFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("mcp: failed to read credentials file at %s: %w", path, err)
	}
	var store map[string]fallbackTokenEntry
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("mcp: failed to parse credentials file at %s: %w", path, err)
	}
	return store, nil
}

// writeFallbackFile writes the fallback credentials file with 0600 permissions,
// removing it entirely when the store is empty. Keys are sorted so the encoding
// is deterministic, matching the reference BTreeMap serialization.
func (s *OAuthStore) writeFallbackFile(store map[string]fallbackTokenEntry) error {
	path, err := s.fallbackFilePath()
	if err != nil {
		return err
	}
	if len(store) == 0 {
		if _, statErr := os.Stat(path); statErr == nil {
			return os.Remove(path)
		}
		return nil
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("mcp: create credentials dir: %w", err)
		}
	}
	serialized, err := marshalFallbackStore(store)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, serialized, 0o600); err != nil {
		return fmt.Errorf("mcp: write credentials file: %w", err)
	}
	return nil
}

// marshalFallbackStore serializes the fallback store with keys in sorted order,
// matching the reference BTreeMap ordering.
func marshalFallbackStore(store map[string]fallbackTokenEntry) ([]byte, error) {
	keys := make([]string, 0, len(store))
	for k := range store {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		keyJSON, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		valueJSON, err := json.Marshal(store[k])
		if err != nil {
			return nil, err
		}
		sb.Write(keyJSON)
		sb.WriteByte(':')
		sb.Write(valueJSON)
	}
	sb.WriteByte('}')
	return []byte(sb.String()), nil
}

// loadFromFile reads tokens for (serverName, url) from the fallback file,
// reconstructing the token response and recomputing expires_in.
func (s *OAuthStore) loadFromFile(serverName, url string) (*StoredOAuthTokens, error) {
	store, err := s.readFallbackFile()
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, nil
	}
	key, err := computeStoreKey(serverName, url)
	if err != nil {
		return nil, err
	}
	for _, entry := range store {
		entryKey, err := computeStoreKey(entry.ServerName, entry.ServerURL)
		if err != nil {
			return nil, err
		}
		if entryKey != key {
			continue
		}
		tokenResponse := OAuthTokenResponse{
			AccessToken:  entry.AccessToken,
			TokenType:    "bearer",
			RefreshToken: entry.RefreshToken,
			Scopes:       entry.Scopes,
		}
		tokens := StoredOAuthTokens{
			ServerName:    entry.ServerName,
			URL:           entry.ServerURL,
			ClientID:      entry.ClientID,
			TokenResponse: tokenResponse,
			ExpiresAt:     entry.ExpiresAt,
		}
		refreshExpiresInFromTimestamp(&tokens, time.Now())
		return &tokens, nil
	}
	return nil, nil
}

// saveToFile writes tokens into the fallback file.
func (s *OAuthStore) saveToFile(tokens StoredOAuthTokens) error {
	key, err := computeStoreKey(tokens.ServerName, tokens.URL)
	if err != nil {
		return err
	}
	store, err := s.readFallbackFile()
	if err != nil {
		return err
	}
	if store == nil {
		store = map[string]fallbackTokenEntry{}
	}
	expiresAt := tokens.ExpiresAt
	if expiresAt == nil {
		expiresAt = tokens.TokenResponse.ComputeExpiresAtMillis(time.Now())
	}
	// scopes serializes as an array (never null), matching the reference
	// FallbackTokenEntry whose `scopes` is a plain Vec.
	scopes := tokens.TokenResponse.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	store[key] = fallbackTokenEntry{
		ServerName:   tokens.ServerName,
		ServerURL:    tokens.URL,
		ClientID:     tokens.ClientID,
		AccessToken:  tokens.TokenResponse.AccessToken,
		ExpiresAt:    expiresAt,
		RefreshToken: tokens.TokenResponse.RefreshToken,
		Scopes:       scopes,
	}
	return s.writeFallbackFile(store)
}

// deleteFromFile removes the entry under key from the fallback file, reporting
// whether anything was removed.
func (s *OAuthStore) deleteFromFile(key string) (bool, error) {
	store, err := s.readFallbackFile()
	if err != nil {
		return false, err
	}
	if store == nil {
		return false, nil
	}
	if _, ok := store[key]; !ok {
		return false, nil
	}
	delete(store, key)
	if err := s.writeFallbackFile(store); err != nil {
		return false, err
	}
	return true, nil
}
