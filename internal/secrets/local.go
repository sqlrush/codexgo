package secrets

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"filippo.io/age"

	"github.com/sqlrush/codexgo/internal/keyring"
)

// secretsVersion is the current on-disk schema version. It mirrors codex's
// `SECRETS_VERSION`.
const secretsVersion uint8 = 1

// localSecretsFilename is the file name (under `<codex_home>/secrets/`) of the
// age-encrypted local secrets store. It mirrors codex's
// `LOCAL_SECRETS_FILENAME`.
const localSecretsFilename = "local.age"

// secretsFile is the decrypted, JSON-serialized representation of the local
// secrets store. It mirrors codex's `SecretsFile`. The secrets map keys are
// canonical keys (see [SecretScope.CanonicalKey]).
//
// To match codex's serde + BTreeMap representation, the JSON object always uses
// the field order {version, secrets} and the secrets object's keys are sorted.
type secretsFile struct {
	version uint8
	secrets map[string]string
}

// newEmptySecretsFile returns an empty store at the current schema version.
func newEmptySecretsFile() secretsFile {
	return secretsFile{version: secretsVersion, secrets: map[string]string{}}
}

// MarshalJSON serializes the file with a fixed field order and sorted secret
// keys, matching codex's serde(Serialize) of a struct containing a BTreeMap.
func (f secretsFile) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"version":`)
	versionBytes, err := json.Marshal(f.version)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize secrets file version: %w", err)
	}
	buf.Write(versionBytes)
	buf.WriteString(`,"secrets":{`)

	keys := make([]string, 0, len(f.secrets))
	for k := range f.secrets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		keyBytes, err := json.Marshal(k)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize secrets key: %w", err)
		}
		buf.Write(keyBytes)
		buf.WriteByte(':')
		valBytes, err := json.Marshal(f.secrets[k])
		if err != nil {
			return nil, fmt.Errorf("failed to serialize secrets value: %w", err)
		}
		buf.Write(valBytes)
	}
	buf.WriteString("}}")
	return buf.Bytes(), nil
}

// secretsFileWire mirrors the wire shape for deserialization.
type secretsFileWire struct {
	Version uint8             `json:"version"`
	Secrets map[string]string `json:"secrets"`
}

// UnmarshalJSON parses the JSON representation, tolerating a missing secrets
// object (treated as empty) to match serde's `#[derive(Default)]` semantics.
func (f *secretsFile) UnmarshalJSON(data []byte) error {
	var wire secretsFileWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	f.version = wire.Version
	if wire.Secrets == nil {
		f.secrets = map[string]string{}
	} else {
		f.secrets = wire.Secrets
	}
	return nil
}

// LocalSecretsBackend is the production [Backend] implementation. It stores
// secrets in an age-encrypted file under `<codex_home>/secrets/local.age`, with
// the encryption passphrase persisted in the system keyring. It mirrors codex's
// `LocalSecretsBackend`.
type LocalSecretsBackend struct {
	codexHome    string
	keyringStore keyring.Store
}

// NewLocalSecretsBackend constructs a [LocalSecretsBackend] for codexHome using
// the supplied keyring store. It mirrors codex's `LocalSecretsBackend::new`.
func NewLocalSecretsBackend(codexHome string, store keyring.Store) *LocalSecretsBackend {
	return &LocalSecretsBackend{codexHome: codexHome, keyringStore: store}
}

// Set stores value for (scope, name). It rejects an empty value, matching codex.
func (b *LocalSecretsBackend) Set(scope SecretScope, name SecretName, value string) error {
	if value == "" {
		return fmt.Errorf("secret value must not be empty")
	}
	canonicalKey := scope.CanonicalKey(name)
	file, err := b.loadFile()
	if err != nil {
		return err
	}
	file.secrets[canonicalKey] = value
	return b.saveFile(file)
}

// Get returns the stored value for (scope, name) or (nil, nil) when absent.
func (b *LocalSecretsBackend) Get(scope SecretScope, name SecretName) (*string, error) {
	canonicalKey := scope.CanonicalKey(name)
	file, err := b.loadFile()
	if err != nil {
		return nil, err
	}
	v, ok := file.secrets[canonicalKey]
	if !ok {
		return nil, nil
	}
	value := v
	return &value, nil
}

// Delete removes the value for (scope, name) and reports whether one existed.
func (b *LocalSecretsBackend) Delete(scope SecretScope, name SecretName) (bool, error) {
	canonicalKey := scope.CanonicalKey(name)
	file, err := b.loadFile()
	if err != nil {
		return false, err
	}
	if _, ok := file.secrets[canonicalKey]; !ok {
		return false, nil
	}
	delete(file.secrets, canonicalKey)
	if err := b.saveFile(file); err != nil {
		return false, err
	}
	return true, nil
}

// List returns all stored entries, optionally filtered to scopeFilter. Invalid
// canonical keys are skipped (matching codex, which warns and continues).
// Entries are returned in sorted canonical-key order for determinism.
func (b *LocalSecretsBackend) List(scopeFilter *SecretScope) ([]ListEntry, error) {
	file, err := b.loadFile()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(file.secrets))
	for k := range file.secrets {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	entries := make([]ListEntry, 0, len(keys))
	for _, canonicalKey := range keys {
		entry, ok := parseCanonicalKey(canonicalKey)
		if !ok {
			// Skip invalid canonical secret keys, matching codex's behavior.
			continue
		}
		if scopeFilter != nil && !entry.Scope.Equal(*scopeFilter) {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// secretsDir returns `<codex_home>/secrets`.
func (b *LocalSecretsBackend) secretsDir() string {
	return filepath.Join(b.codexHome, "secrets")
}

// secretsPath returns `<codex_home>/secrets/local.age`.
func (b *LocalSecretsBackend) secretsPath() string {
	return filepath.Join(b.secretsDir(), localSecretsFilename)
}

// loadFile reads and decrypts the secrets file, returning an empty store when no
// file exists. It rejects schema versions newer than supported, matching codex.
func (b *LocalSecretsBackend) loadFile() (secretsFile, error) {
	path := b.secretsPath()
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return newEmptySecretsFile(), nil
		}
		return secretsFile{}, fmt.Errorf("failed to read secrets file at %s: %w", path, err)
	}

	ciphertext, err := os.ReadFile(path)
	if err != nil {
		return secretsFile{}, fmt.Errorf("failed to read secrets file at %s: %w", path, err)
	}
	passphrase, err := b.loadOrCreatePassphrase()
	if err != nil {
		return secretsFile{}, err
	}
	plaintext, err := decryptWithPassphrase(ciphertext, passphrase)
	if err != nil {
		return secretsFile{}, err
	}
	var parsed secretsFile
	if err := json.Unmarshal(plaintext, &parsed); err != nil {
		return secretsFile{}, fmt.Errorf(
			"failed to deserialize decrypted secrets file at %s: %w", path, err)
	}
	if parsed.version == 0 {
		parsed.version = secretsVersion
	}
	if parsed.version > secretsVersion {
		return secretsFile{}, fmt.Errorf(
			"secrets file version %d is newer than supported version %d",
			parsed.version, secretsVersion)
	}
	return parsed, nil
}

// saveFile encrypts and atomically writes the secrets file.
func (b *LocalSecretsBackend) saveFile(file secretsFile) error {
	dir := b.secretsDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create secrets dir %s: %w", dir, err)
	}

	passphrase, err := b.loadOrCreatePassphrase()
	if err != nil {
		return err
	}
	plaintext, err := json.Marshal(file)
	if err != nil {
		return fmt.Errorf("failed to serialize secrets file: %w", err)
	}
	ciphertext, err := encryptWithPassphrase(plaintext, passphrase)
	if err != nil {
		return err
	}
	return writeFileAtomically(b.secretsPath(), ciphertext)
}

// loadOrCreatePassphrase returns the keyring-stored passphrase for this
// codexHome, generating and persisting a new high-entropy one when none exists.
func (b *LocalSecretsBackend) loadOrCreatePassphrase() (string, error) {
	account := computeKeyringAccount(b.codexHome)
	loaded, err := b.keyringStore.Load(keyringService, account)
	if err != nil {
		return "", fmt.Errorf(
			"failed to load secrets key from keyring for %s: %w", account, keyringMessage(err))
	}
	if loaded != nil {
		return *loaded, nil
	}

	// Generate a high-entropy key and persist it in the OS keyring. This keeps
	// secrets out of plaintext config while remaining fully local/offline.
	generated, err := generatePassphrase()
	if err != nil {
		return "", err
	}
	if err := b.keyringStore.Save(keyringService, account, generated); err != nil {
		return "", fmt.Errorf("failed to persist secrets key in keyring: %w", keyringMessage(err))
	}
	return generated, nil
}

// keyringMessage unwraps a keyring [keyring.StoreError] to its underlying
// message-bearing error so error strings match codex's
// `anyhow::anyhow!(err.message())`, which discards the StoreError wrapper.
func keyringMessage(err error) error {
	var storeErr *keyring.StoreError
	if errors.As(err, &storeErr) {
		return errors.New(storeErr.Message())
	}
	return err
}

// writeFileAtomically writes contents to path via a uniquely named temp file and
// an atomic rename, matching codex's `write_file_atomically`. It never leaves
// temp files behind on failure.
func writeFileAtomically(path string, contents []byte) error {
	dir := filepath.Dir(path)
	if dir == "" {
		return fmt.Errorf("failed to compute parent directory for secrets file at %s", path)
	}
	nonce := time.Now().UnixNano()
	tmpPath := filepath.Join(dir, fmt.Sprintf(".%s.tmp-%d-%d", localSecretsFilename, os.Getpid(), nonce))

	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("failed to create temp secrets file at %s: %w", tmpPath, err)
	}
	if _, err := tmpFile.Write(contents); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write temp secrets file at %s: %w", tmpPath, err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to sync temp secrets file at %s: %w", tmpPath, err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp secrets file at %s: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf(
			"failed to atomically replace secrets file at %s with %s: %w", path, tmpPath, err)
	}
	return nil
}

// generatePassphrase generates a base64-encoded 32-byte random passphrase,
// matching codex's `generate_passphrase`.
func generatePassphrase() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("failed to generate random secrets key: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(b[:])
	// Wipe the raw key material now that it is encoded.
	for i := range b {
		b[i] = 0
	}
	return encoded, nil
}

// encryptWithPassphrase encrypts plaintext with an age scrypt recipient derived
// from passphrase, matching codex's `encrypt_with_passphrase`.
func encryptWithPassphrase(plaintext []byte, passphrase string) ([]byte, error) {
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt secrets file: %w", err)
	}
	var out bytes.Buffer
	w, err := age.Encrypt(&out, recipient)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt secrets file: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("failed to encrypt secrets file: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("failed to encrypt secrets file: %w", err)
	}
	return out.Bytes(), nil
}

// decryptWithPassphrase decrypts ciphertext with an age scrypt identity derived
// from passphrase, matching codex's `decrypt_with_passphrase`.
func decryptWithPassphrase(ciphertext []byte, passphrase string) ([]byte, error) {
	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt secrets file: %w", err)
	}
	r, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt secrets file: %w", err)
	}
	plaintext, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt secrets file: %w", err)
	}
	return plaintext, nil
}

// parseCanonicalKey parses a canonical map key back into a [ListEntry], or
// returns ok=false when the key is malformed. It mirrors codex's
// `parse_canonical_key`.
func parseCanonicalKey(canonicalKey string) (ListEntry, bool) {
	parts := splitAll(canonicalKey, '/')
	if len(parts) == 0 {
		return ListEntry{}, false
	}
	switch parts[0] {
	case "global":
		if len(parts) != 2 {
			return ListEntry{}, false
		}
		name, err := NewSecretName(parts[1])
		if err != nil {
			return ListEntry{}, false
		}
		return ListEntry{Scope: GlobalScope(), Name: name}, true
	case "env":
		if len(parts) != 3 {
			return ListEntry{}, false
		}
		name, err := NewSecretName(parts[2])
		if err != nil {
			return ListEntry{}, false
		}
		scope, err := NewEnvironmentScope(parts[1])
		if err != nil {
			return ListEntry{}, false
		}
		return ListEntry{Scope: scope, Name: name}, true
	default:
		return ListEntry{}, false
	}
}

// splitAll splits s on sep, mirroring Rust's `str::split`, which yields an empty
// trailing element for a trailing separator and never collapses runs. This
// matches codex's iterator-based `parse_canonical_key`, which counts the exact
// number of segments.
func splitAll(s string, sep byte) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
