// Package secrets is a faithful Go port of codex's `secrets` crate. It provides
// an age-encrypted, local key/value store for named secrets scoped either
// globally or to a particular environment (typically a git repository).
//
// The on-disk format, storage location (`<codex_home>/secrets/local.age`),
// canonical map keys, and OS-keyring passphrase derivation all match codex so
// the two implementations are drop-in compatible.
//
// The central abstraction is [Backend] (codex's `SecretsBackend` trait).
// [LocalSecretsBackend] is the production implementation backed by an
// age-encrypted file whose passphrase lives in the system keyring (via
// internal/keyring). [Manager] (codex's `SecretsManager`) wraps a backend.
package secrets

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/internal/brand"
	"github.com/sqlrush/codexgo/internal/gitutils"
	"github.com/sqlrush/codexgo/internal/keyring"
	syskeyring "github.com/sqlrush/codexgo/internal/keyring/system"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// keyringService is the service name used for all keyring entries. It mirrors
// the `KEYRING_SERVICE` constant in codex, with codexgo's own service name so
// the two products' keychain entries never collide.
const keyringService = brand.KeyringSecretsService

// SecretName is a validated secret identifier. Valid names are non-empty and
// contain only ASCII uppercase letters, digits, or underscores (after trimming
// surrounding whitespace). It mirrors codex's `SecretName` newtype.
//
// The zero value is not valid; construct values through [NewSecretName].
type SecretName struct {
	value string
}

// NewSecretName validates raw and returns a [SecretName]. Surrounding
// whitespace is trimmed. It returns an error when the trimmed value is empty or
// contains characters outside A-Z, 0-9, or '_', matching codex's
// `SecretName::new`.
func NewSecretName(raw string) (SecretName, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return SecretName{}, fmt.Errorf("secret name must not be empty")
	}
	for _, ch := range trimmed {
		if !isUpperAlnumUnderscore(ch) {
			return SecretName{}, fmt.Errorf("secret name must contain only A-Z, 0-9, or _")
		}
	}
	return SecretName{value: trimmed}, nil
}

// isUpperAlnumUnderscore reports whether ch is an ASCII uppercase letter, an
// ASCII digit, or an underscore.
func isUpperAlnumUnderscore(ch rune) bool {
	switch {
	case ch >= 'A' && ch <= 'Z':
		return true
	case ch >= '0' && ch <= '9':
		return true
	case ch == '_':
		return true
	default:
		return false
	}
}

// String returns the secret name, mirroring codex's `Display`.
func (n SecretName) String() string {
	return n.value
}

// AsStr returns the secret name, mirroring codex's `SecretName::as_str`.
func (n SecretName) AsStr() string {
	return n.value
}

// ScopeKind identifies which variant of [SecretScope] is in use.
type ScopeKind int

const (
	// ScopeGlobal is the global scope, shared across all environments.
	ScopeGlobal ScopeKind = iota
	// ScopeEnvironment is a scope tied to a specific environment identifier.
	ScopeEnvironment
)

// SecretScope identifies the scope of a secret: either global or tied to a
// specific environment identifier. It mirrors codex's `SecretScope` enum.
//
// The zero value is the global scope. Construct environment scopes through
// [NewEnvironmentScope].
type SecretScope struct {
	kind          ScopeKind
	environmentID string
}

// GlobalScope returns the global [SecretScope].
func GlobalScope() SecretScope {
	return SecretScope{kind: ScopeGlobal}
}

// NewEnvironmentScope validates environmentID and returns an environment-scoped
// [SecretScope]. Surrounding whitespace is trimmed and an empty identifier is
// rejected, matching codex's `SecretScope::environment`.
func NewEnvironmentScope(environmentID string) (SecretScope, error) {
	trimmed := strings.TrimSpace(environmentID)
	if trimmed == "" {
		return SecretScope{}, fmt.Errorf("environment id must not be empty")
	}
	return SecretScope{kind: ScopeEnvironment, environmentID: trimmed}, nil
}

// Kind reports which scope variant this value represents.
func (s SecretScope) Kind() ScopeKind {
	return s.kind
}

// EnvironmentID returns the environment identifier and true when the scope is an
// environment scope; otherwise it returns ("", false).
func (s SecretScope) EnvironmentID() (string, bool) {
	if s.kind == ScopeEnvironment {
		return s.environmentID, true
	}
	return "", false
}

// Equal reports whether two scopes are identical.
func (s SecretScope) Equal(other SecretScope) bool {
	return s.kind == other.kind && s.environmentID == other.environmentID
}

// CanonicalKey returns the stable, env-safe identifier used as the on-disk map
// key for (scope, name). It mirrors codex's `SecretScope::canonical_key`:
//
//   - Global:      "global/<NAME>"
//   - Environment: "env/<environment_id>/<NAME>"
func (s SecretScope) CanonicalKey(name SecretName) string {
	switch s.kind {
	case ScopeEnvironment:
		return fmt.Sprintf("env/%s/%s", s.environmentID, name.AsStr())
	default:
		return fmt.Sprintf("global/%s", name.AsStr())
	}
}

// ListEntry pairs a scope with a secret name. It mirrors codex's
// `SecretListEntry` and is returned by [Backend.List].
type ListEntry struct {
	Scope SecretScope
	Name  SecretName
}

// BackendKind selects which secrets backend [Manager] uses. It mirrors codex's
// `SecretsBackendKind`. It serializes as a lowercase string ("local").
type BackendKind int

const (
	// BackendLocal selects the age-encrypted local file backend. It is the
	// default and currently the only variant.
	BackendLocal BackendKind = iota
)

// MarshalJSON renders the backend kind as a lowercase string, matching codex's
// `#[serde(rename_all = "lowercase")]` representation.
func (k BackendKind) MarshalJSON() ([]byte, error) {
	switch k {
	case BackendLocal:
		return []byte(`"local"`), nil
	default:
		return nil, fmt.Errorf("unknown secrets backend kind: %d", int(k))
	}
}

// UnmarshalJSON parses a lowercase backend-kind string, matching codex.
func (k *BackendKind) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case `"local"`:
		*k = BackendLocal
		return nil
	default:
		return fmt.Errorf("unknown secrets backend kind: %s", string(data))
	}
}

// Backend is the storage abstraction for secrets. It mirrors codex's
// `SecretsBackend` trait. Implementations must be safe for concurrent use.
type Backend interface {
	// Set stores value for (scope, name), overwriting any existing value.
	Set(scope SecretScope, name SecretName, value string) error
	// Get returns the stored value for (scope, name). When no value exists it
	// returns (nil, nil).
	Get(scope SecretScope, name SecretName) (*string, error)
	// Delete removes the value for (scope, name) and reports whether one
	// existed.
	Delete(scope SecretScope, name SecretName) (bool, error)
	// List returns all stored entries, optionally filtered to a single scope.
	// A nil filter returns every entry.
	List(scopeFilter *SecretScope) ([]ListEntry, error)
}

// Manager wraps a [Backend] and exposes the same operations. It mirrors codex's
// `SecretsManager`. Manager is safe for concurrent use when its backend is.
type Manager struct {
	backend Backend
}

// NewManager constructs a [Manager] for the given codexHome and backend kind,
// using the system keyring for passphrase storage. It mirrors codex's
// `SecretsManager::new`.
func NewManager(codexHome string, kind BackendKind) *Manager {
	return NewManagerWithKeyringStore(codexHome, kind, syskeyring.NewDefaultStore())
}

// NewManagerWithKeyringStore constructs a [Manager] using the supplied keyring
// store. It mirrors codex's `SecretsManager::new_with_keyring_store` and is the
// primary seam for tests.
func NewManagerWithKeyringStore(codexHome string, kind BackendKind, store keyring.Store) *Manager {
	var backend Backend
	switch kind {
	case BackendLocal:
		backend = NewLocalSecretsBackend(codexHome, store)
	default:
		backend = NewLocalSecretsBackend(codexHome, store)
	}
	return &Manager{backend: backend}
}

// Set stores value for (scope, name).
func (m *Manager) Set(scope SecretScope, name SecretName, value string) error {
	return m.backend.Set(scope, name, value)
}

// Get returns the stored value for (scope, name) or (nil, nil) when absent.
func (m *Manager) Get(scope SecretScope, name SecretName) (*string, error) {
	return m.backend.Get(scope, name)
}

// Delete removes the value for (scope, name) and reports whether one existed.
func (m *Manager) Delete(scope SecretScope, name SecretName) (bool, error) {
	return m.backend.Delete(scope, name)
}

// List returns all stored entries, optionally filtered to a single scope.
func (m *Manager) List(scopeFilter *SecretScope) ([]ListEntry, error) {
	return m.backend.List(scopeFilter)
}

// EnvironmentIDFromCwd derives a stable environment identifier for cwd. When cwd
// is inside a git repository, the repository directory name is used. Otherwise a
// "cwd-<hash>" identifier derived from the canonical path is returned. It
// mirrors codex's `environment_id_from_cwd`.
func EnvironmentIDFromCwd(cwd string) string {
	if root, ok := gitutils.GetGitRepoRoot(cwd); ok {
		name := strings.TrimSpace(baseName(root))
		if name != "" {
			return name
		}
	}

	canonical := canonicalizeOrSelf(cwd)
	sum := sha256.Sum256([]byte(canonical))
	hexDigest := hex.EncodeToString(sum[:])
	short := hexDigest
	if len(short) > 12 {
		short = short[:12]
	}
	return "cwd-" + short
}

// computeKeyringAccount derives the keyring account name for a given codexHome.
// It mirrors codex's `compute_keyring_account`: "secrets|<first 16 hex chars of
// sha256(canonical codex_home)>".
func computeKeyringAccount(codexHome string) string {
	canonical := canonicalizeOrSelf(codexHome)
	sum := sha256.Sum256([]byte(canonical))
	hexDigest := hex.EncodeToString(sum[:])
	short := hexDigest
	if len(short) > 16 {
		short = short[:16]
	}
	return "secrets|" + short
}

// canonicalizeOrSelf returns the canonicalized form of path, falling back to the
// input when canonicalization fails. It mirrors codex's use of
// `Path::canonicalize().unwrap_or_else(|_| to_path_buf())` followed by
// `to_string_lossy`.
func canonicalizeOrSelf(path string) string {
	abs, err := abspath.FromAbsolutePath(path)
	if err != nil {
		return path
	}
	canon, err := abs.Canonicalize()
	if err != nil {
		return path
	}
	return canon.Path()
}

// baseName returns the final path component of p, mirroring Rust's
// `Path::file_name`.
func baseName(p string) string {
	trimmed := strings.TrimRight(p, "/\\")
	if trimmed == "" {
		return p
	}
	idx := strings.LastIndexAny(trimmed, "/\\")
	if idx < 0 {
		return trimmed
	}
	return trimmed[idx+1:]
}
