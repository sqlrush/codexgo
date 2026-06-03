package login

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sqlrush/codexgo/internal/agentidentity"
	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/keyring"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// AuthDotJson is the expected structure of $CODEX_HOME/auth.json.
//
// Rust: storage::AuthDotJson. Field order and serde attributes are preserved:
//   - auth_mode: default, skip_serializing_if None
//   - OPENAI_API_KEY (renamed): Option<String>, no skip -> always emitted (null)
//   - tokens: default, skip_serializing_if None
//   - last_refresh: default, skip_serializing_if None
//   - agent_identity: default, skip_serializing_if None
type AuthDotJson struct {
	AuthMode      *appserverproto.AuthMode
	OpenAIAPIKey  *string
	Tokens        *TokenData
	LastRefresh   *time.Time
	AgentIdentity *string
}

// authDotJsonWire is the on-disk JSON form. Pointer fields with omitempty match
// the Rust skip_serializing_if = "Option::is_none". OPENAI_API_KEY has no
// omitempty because the Rust field lacks skip_serializing_if (always emitted).
type authDotJsonWire struct {
	AuthMode      *appserverproto.AuthMode `json:"auth_mode,omitempty"`
	OpenAIAPIKey  *string                  `json:"OPENAI_API_KEY"`
	Tokens        *TokenData               `json:"tokens,omitempty"`
	LastRefresh   *time.Time               `json:"last_refresh,omitempty"`
	AgentIdentity *string                  `json:"agent_identity,omitempty"`
}

// MarshalJSON encodes AuthDotJson in the codex on-disk shape.
func (a AuthDotJson) MarshalJSON() ([]byte, error) {
	return json.Marshal(authDotJsonWire{
		AuthMode:      a.AuthMode,
		OpenAIAPIKey:  a.OpenAIAPIKey,
		Tokens:        a.Tokens,
		LastRefresh:   a.LastRefresh,
		AgentIdentity: a.AgentIdentity,
	})
}

// UnmarshalJSON decodes AuthDotJson from the codex on-disk shape.
func (a *AuthDotJson) UnmarshalJSON(data []byte) error {
	var wire authDotJsonWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	a.AuthMode = wire.AuthMode
	a.OpenAIAPIKey = wire.OpenAIAPIKey
	a.Tokens = wire.Tokens
	a.LastRefresh = wire.LastRefresh
	a.AgentIdentity = wire.AgentIdentity
	return nil
}

// AgentIdentityAuthRecord is the verified agent-identity record derived from an
// agent-identity JWT. Rust: storage::AgentIdentityAuthRecord, snake_case serde.
type AgentIdentityAuthRecord struct {
	AgentRuntimeID          string            `json:"agent_runtime_id"`
	AgentPrivateKey         string            `json:"agent_private_key"`
	AccountID               string            `json:"account_id"`
	ChatgptUserID           string            `json:"chatgpt_user_id"`
	Email                   string            `json:"email"`
	PlanType                protocol.PlanType `json:"plan_type"`
	ChatgptAccountIsFedramp bool              `json:"chatgpt_account_is_fedramp"`
}

// agentIdentityAuthRecordFromClaims converts decoded JWT claims into a record,
// mirroring the Rust From<AgentIdentityJwtClaims> for AgentIdentityAuthRecord.
func agentIdentityAuthRecordFromClaims(claims agentidentity.JwtClaims) AgentIdentityAuthRecord {
	return AgentIdentityAuthRecord{
		AgentRuntimeID:          claims.AgentRuntimeID,
		AgentPrivateKey:         claims.AgentPrivateKey,
		AccountID:               claims.AccountID,
		ChatgptUserID:           claims.ChatgptUserID,
		Email:                   claims.Email,
		PlanType:                protocol.PlanTypeFromAuthPlanType(claims.PlanType),
		ChatgptAccountIsFedramp: claims.ChatgptAccountIsFedramp,
	}
}

// AgentIdentityAuthRecordFromJWT decodes an agent-identity JWT WITHOUT verifying
// its signature and returns the derived record. Mirrors the Rust
// AgentIdentityAuthRecord::from_agent_identity_jwt.
func AgentIdentityAuthRecordFromJWT(jwt string) (AgentIdentityAuthRecord, error) {
	claims, err := agentidentity.DecodeJwtClaims(jwt)
	if err != nil {
		return AgentIdentityAuthRecord{}, err
	}
	return agentIdentityAuthRecordFromClaims(claims), nil
}

// getAuthFile returns the auth.json path inside codexHome. Mirrors get_auth_file.
func getAuthFile(codexHome string) string {
	return filepath.Join(codexHome, "auth.json")
}

// deleteFileIfExists removes codexHome/auth.json if present, reporting whether a
// file was removed. Mirrors delete_file_if_exists.
func deleteFileIfExists(codexHome string) (bool, error) {
	authFile := getAuthFile(codexHome)
	err := os.Remove(authFile)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("delete auth file: %w", err)
	}
}

// authStorageBackend is the storage abstraction for auth.json. Mirrors the Rust
// AuthStorageBackend trait.
type authStorageBackend interface {
	load() (*AuthDotJson, error)
	save(auth *AuthDotJson) error
	delete() (bool, error)
}

// fileAuthStorage stores auth.json on disk with 0600 permissions. Mirrors
// FileAuthStorage.
type fileAuthStorage struct {
	codexHome string
}

func newFileAuthStorage(codexHome string) *fileAuthStorage {
	return &fileAuthStorage{codexHome: codexHome}
}

func (f *fileAuthStorage) tryReadAuthJSON(authFile string) (*AuthDotJson, error) {
	contents, err := os.ReadFile(authFile)
	if err != nil {
		return nil, err
	}
	var auth AuthDotJson
	if err := json.Unmarshal(contents, &auth); err != nil {
		return nil, fmt.Errorf("parse auth.json: %w", err)
	}
	return &auth, nil
}

func (f *fileAuthStorage) load() (*AuthDotJson, error) {
	authFile := getAuthFile(f.codexHome)
	auth, err := f.tryReadAuthJSON(authFile)
	switch {
	case err == nil:
		return auth, nil
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	default:
		return nil, err
	}
}

func (f *fileAuthStorage) save(auth *AuthDotJson) error {
	authFile := getAuthFile(f.codexHome)
	if parent := filepath.Dir(authFile); parent != "" {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return fmt.Errorf("create codex home: %w", err)
		}
	}
	// to_string_pretty produces 2-space-indented JSON; match that exactly.
	jsonData, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return fmt.Errorf("serialize auth.json: %w", err)
	}
	// O_TRUNC|O_WRONLY|O_CREATE with mode 0600, matching the Rust OpenOptions.
	file, err := os.OpenFile(authFile, os.O_TRUNC|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("open auth.json for write: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(jsonData); err != nil {
		return fmt.Errorf("write auth.json: %w", err)
	}
	return file.Sync()
}

func (f *fileAuthStorage) delete() (bool, error) {
	return deleteFileIfExists(f.codexHome)
}

// keyringService is the keyring service name used for CLI auth. Mirrors the Rust
// KEYRING_SERVICE constant.
const keyringService = "Codex Auth"

// computeStoreKey turns a codex_home path into a stable, short key string of the
// form "cli|{first16hex}". Mirrors compute_store_key: the path is canonicalized
// (falling back to the ORIGINAL, unmodified path string on failure), hashed with
// SHA-256, hex-encoded, and truncated to 16 characters.
//
// Rust's Path::canonicalize both makes the path absolute and resolves symlinks,
// and it fails for paths that do not exist, in which case the Rust code falls
// back to codex_home as-is. We reproduce that exactly: try Abs+EvalSymlinks
// (EvalSymlinks fails for nonexistent paths), and on any failure use the
// original string unchanged so the hash matches byte-for-byte.
func computeStoreKey(codexHome string) (string, error) {
	canonical := codexHome
	if abs, err := filepath.Abs(codexHome); err == nil {
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			canonical = resolved
		}
	}
	digest := sha256.Sum256([]byte(canonical))
	hexStr := fmt.Sprintf("%x", digest)
	truncated := hexStr
	if len(hexStr) >= 16 {
		truncated = hexStr[:16]
	}
	return "cli|" + truncated, nil
}

// keyringAuthStorage stores auth.json in the system keyring. Mirrors
// KeyringAuthStorage.
type keyringAuthStorage struct {
	codexHome    string
	keyringStore keyring.Store
}

func newKeyringAuthStorage(codexHome string, store keyring.Store) *keyringAuthStorage {
	return &keyringAuthStorage{codexHome: codexHome, keyringStore: store}
}

func (k *keyringAuthStorage) loadFromKeyring(key string) (*AuthDotJson, error) {
	serialized, err := k.keyringStore.Load(keyringService, key)
	if err != nil {
		return nil, fmt.Errorf("failed to load CLI auth from keyring: %w", err)
	}
	if serialized == nil {
		return nil, nil
	}
	var auth AuthDotJson
	if err := json.Unmarshal([]byte(*serialized), &auth); err != nil {
		return nil, fmt.Errorf("failed to deserialize CLI auth from keyring: %w", err)
	}
	return &auth, nil
}

func (k *keyringAuthStorage) saveToKeyring(key, value string) error {
	if err := k.keyringStore.Save(keyringService, key, value); err != nil {
		return fmt.Errorf("failed to write OAuth tokens to keyring: %w", err)
	}
	return nil
}

func (k *keyringAuthStorage) load() (*AuthDotJson, error) {
	key, err := computeStoreKey(k.codexHome)
	if err != nil {
		return nil, err
	}
	return k.loadFromKeyring(key)
}

func (k *keyringAuthStorage) save(auth *AuthDotJson) error {
	key, err := computeStoreKey(k.codexHome)
	if err != nil {
		return err
	}
	// Keyring storage uses compact (non-pretty) JSON, matching to_string.
	serialized, err := json.Marshal(auth)
	if err != nil {
		return fmt.Errorf("serialize auth for keyring: %w", err)
	}
	if err := k.saveToKeyring(key, string(serialized)); err != nil {
		return err
	}
	// Best-effort removal of any stale fallback file; ignore failure.
	_, _ = deleteFileIfExists(k.codexHome)
	return nil
}

func (k *keyringAuthStorage) delete() (bool, error) {
	key, err := computeStoreKey(k.codexHome)
	if err != nil {
		return false, err
	}
	keyringRemoved, err := k.keyringStore.Delete(keyringService, key)
	if err != nil {
		return false, fmt.Errorf("failed to delete auth from keyring: %w", err)
	}
	fileRemoved, err := deleteFileIfExists(k.codexHome)
	if err != nil {
		return false, err
	}
	return keyringRemoved || fileRemoved, nil
}

// autoAuthStorage prefers the keyring and falls back to file storage on error.
// Mirrors AutoAuthStorage.
type autoAuthStorage struct {
	keyringStorage *keyringAuthStorage
	fileStorage    *fileAuthStorage
}

func newAutoAuthStorage(codexHome string, store keyring.Store) *autoAuthStorage {
	return &autoAuthStorage{
		keyringStorage: newKeyringAuthStorage(codexHome, store),
		fileStorage:    newFileAuthStorage(codexHome),
	}
}

func (a *autoAuthStorage) load() (*AuthDotJson, error) {
	auth, err := a.keyringStorage.load()
	if err != nil {
		// Fall back to file storage; the keyring may be unavailable.
		return a.fileStorage.load()
	}
	if auth != nil {
		return auth, nil
	}
	return a.fileStorage.load()
}

func (a *autoAuthStorage) save(auth *AuthDotJson) error {
	if err := a.keyringStorage.save(auth); err != nil {
		return a.fileStorage.save(auth)
	}
	return nil
}

func (a *autoAuthStorage) delete() (bool, error) {
	// Keyring storage deletes from disk as well.
	return a.keyringStorage.delete()
}

// ephemeralAuthStore is the process-global in-memory store mapping store key ->
// AuthDotJson, mirroring the Rust EPHEMERAL_AUTH_STORE static.
var (
	ephemeralAuthMu    sync.Mutex
	ephemeralAuthStore = map[string]AuthDotJson{}
)

// ephemeralAuthStorage stores auth.json only in process memory. Mirrors
// EphemeralAuthStorage.
type ephemeralAuthStorage struct {
	codexHome string
}

func newEphemeralAuthStorage(codexHome string) *ephemeralAuthStorage {
	return &ephemeralAuthStorage{codexHome: codexHome}
}

func (e *ephemeralAuthStorage) load() (*AuthDotJson, error) {
	key, err := computeStoreKey(e.codexHome)
	if err != nil {
		return nil, err
	}
	ephemeralAuthMu.Lock()
	defer ephemeralAuthMu.Unlock()
	auth, ok := ephemeralAuthStore[key]
	if !ok {
		return nil, nil
	}
	copy := auth
	return &copy, nil
}

func (e *ephemeralAuthStorage) save(auth *AuthDotJson) error {
	key, err := computeStoreKey(e.codexHome)
	if err != nil {
		return err
	}
	ephemeralAuthMu.Lock()
	defer ephemeralAuthMu.Unlock()
	ephemeralAuthStore[key] = *auth
	return nil
}

func (e *ephemeralAuthStorage) delete() (bool, error) {
	key, err := computeStoreKey(e.codexHome)
	if err != nil {
		return false, err
	}
	ephemeralAuthMu.Lock()
	defer ephemeralAuthMu.Unlock()
	if _, ok := ephemeralAuthStore[key]; !ok {
		return false, nil
	}
	delete(ephemeralAuthStore, key)
	return true, nil
}

// createAuthStorage builds the storage backend for the given mode using the
// default system keyring. Mirrors create_auth_storage.
func createAuthStorage(codexHome string, mode config.AuthCredentialsStoreMode) authStorageBackend {
	return createAuthStorageWithKeyring(codexHome, mode, keyring.NewDefaultStore())
}

// createAuthStorageWithKeyring builds the storage backend, injecting a keyring
// store. Mirrors create_auth_storage_with_keyring_store.
func createAuthStorageWithKeyring(codexHome string, mode config.AuthCredentialsStoreMode, store keyring.Store) authStorageBackend {
	switch mode {
	case config.AuthCredentialsStoreFile:
		return newFileAuthStorage(codexHome)
	case config.AuthCredentialsStoreKeyring:
		return newKeyringAuthStorage(codexHome, store)
	case config.AuthCredentialsStoreAuto:
		return newAutoAuthStorage(codexHome, store)
	case config.AuthCredentialsStoreEphemeral:
		return newEphemeralAuthStorage(codexHome)
	default:
		return newFileAuthStorage(codexHome)
	}
}
