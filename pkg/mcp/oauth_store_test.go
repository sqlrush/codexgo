package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/keyring"
)

func sampleTokens() StoredOAuthTokens {
	in := uint64(3600)
	rt := "refresh-me"
	return StoredOAuthTokens{
		ServerName: "srv",
		URL:        "https://example.com/mcp",
		ClientID:   "client-123",
		TokenResponse: OAuthTokenResponse{
			AccessToken:  "access-abc",
			TokenType:    "bearer",
			ExpiresIn:    &in,
			RefreshToken: &rt,
			Scopes:       []string{"read", "write"},
		},
	}
}

func TestComputeStoreKeyDeterministic(t *testing.T) {
	t.Parallel()
	k1, err := computeStoreKey("srv", "https://example.com/mcp")
	if err != nil {
		t.Fatalf("computeStoreKey: %v", err)
	}
	k2, _ := computeStoreKey("srv", "https://example.com/mcp")
	if k1 != k2 {
		t.Fatalf("non-deterministic: %q vs %q", k1, k2)
	}
	// Format: "<server>|<16 hex chars>". The hash is over the canonical payload
	// {"type":"http","url":<url>,"headers":{}} in insertion order (preserve_order
	// is enabled in the reference workspace), giving this exact prefix.
	want := "srv|9a70b85417749d5c"
	if k1 != want {
		t.Fatalf("store key=%q want %q", k1, want)
	}
	// A different URL must yield a different hash.
	k3, _ := computeStoreKey("srv", "https://other.example/mcp")
	if k3 == k1 {
		t.Fatalf("expected different key for different url")
	}
}

func TestOAuthStoreKeyringRoundTrip(t *testing.T) {
	t.Parallel()
	mem := keyring.NewMemoryStore()
	store := NewOAuthStoreWith(mem, t.TempDir())
	tok := sampleTokens()

	if err := store.Save(tok, config.OAuthCredentialsStoreKeyring); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load(tok.ServerName, tok.URL, config.OAuthCredentialsStoreKeyring)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Fatal("expected loaded tokens")
	}
	assertTokensMatch(t, *got, tok)

	has, err := store.Has(tok.ServerName, tok.URL, config.OAuthCredentialsStoreKeyring)
	if err != nil || !has {
		t.Fatalf("Has=%v err=%v", has, err)
	}

	removed, err := store.Delete(tok.ServerName, tok.URL, config.OAuthCredentialsStoreKeyring)
	if err != nil || !removed {
		t.Fatalf("Delete removed=%v err=%v", removed, err)
	}
	after, err := store.Load(tok.ServerName, tok.URL, config.OAuthCredentialsStoreKeyring)
	if err != nil {
		t.Fatalf("Load after delete: %v", err)
	}
	if after != nil {
		t.Fatalf("expected nil after delete, got %+v", after)
	}
}

func TestOAuthStoreFileRoundTrip(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	mem := keyring.NewMemoryStore()
	store := NewOAuthStoreWith(mem, home)
	tok := sampleTokens()

	if err := store.Save(tok, config.OAuthCredentialsStoreFile); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(home, fallbackFilename)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat credentials file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("credentials file perm=%o want 600", perm)
	}

	// The fallback file must serialize the documented FallbackTokenEntry shape,
	// with expires_at and refresh_token present (computed from expires_in) and
	// scopes as a JSON array.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var parsed map[string]fallbackTokenEntry
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parse file: %v", err)
	}
	key, _ := computeStoreKey(tok.ServerName, tok.URL)
	entry, ok := parsed[key]
	if !ok {
		t.Fatalf("missing entry for key %q in %s", key, raw)
	}
	if entry.AccessToken != "access-abc" || entry.ClientID != "client-123" {
		t.Fatalf("entry fields wrong: %+v", entry)
	}
	if entry.ExpiresAt == nil {
		t.Fatal("expected computed expires_at")
	}
	if len(entry.Scopes) != 2 {
		t.Fatalf("scopes=%v", entry.Scopes)
	}

	got, err := store.Load(tok.ServerName, tok.URL, config.OAuthCredentialsStoreFile)
	if err != nil || got == nil {
		t.Fatalf("Load: got=%v err=%v", got, err)
	}
	assertTokensMatch(t, *got, tok)

	removed, err := store.Delete(tok.ServerName, tok.URL, config.OAuthCredentialsStoreFile)
	if err != nil || !removed {
		t.Fatalf("Delete removed=%v err=%v", removed, err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("expected credentials file removed when empty, stat err=%v", statErr)
	}
}

func TestOAuthStoreAutoFallsBackToFile(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	mem := keyring.NewMemoryStore()
	store := NewOAuthStoreWith(mem, home)
	tok := sampleTokens()

	// Force keyring writes to fail; Auto must fall back to the file.
	key, _ := computeStoreKey(tok.ServerName, tok.URL)
	mem.SetError(key, errInjected)

	if err := store.Save(tok, config.OAuthCredentialsStoreAuto); err != nil {
		t.Fatalf("Save (auto): %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, fallbackFilename)); statErr != nil {
		t.Fatalf("expected fallback file to be written: %v", statErr)
	}

	got, err := store.Load(tok.ServerName, tok.URL, config.OAuthCredentialsStoreAuto)
	if err != nil || got == nil {
		t.Fatalf("Load (auto): got=%v err=%v", got, err)
	}
	assertTokensMatch(t, *got, tok)
}

func TestOAuthStoreLoadMissingReturnsNil(t *testing.T) {
	t.Parallel()
	store := NewOAuthStoreWith(keyring.NewMemoryStore(), t.TempDir())
	got, err := store.Load("nope", "https://nope.example", config.OAuthCredentialsStoreAuto)
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for missing, got %+v", got)
	}
}

// assertTokensMatch compares the identity-bearing fields, ignoring the
// recomputed expires_in (which depends on wall-clock time).
func assertTokensMatch(t *testing.T, got, want StoredOAuthTokens) {
	t.Helper()
	if got.ServerName != want.ServerName || got.URL != want.URL || got.ClientID != want.ClientID {
		t.Fatalf("identity mismatch: got %+v want %+v", got, want)
	}
	if got.TokenResponse.AccessToken != want.TokenResponse.AccessToken {
		t.Fatalf("access token mismatch: %q vs %q", got.TokenResponse.AccessToken, want.TokenResponse.AccessToken)
	}
	if (got.TokenResponse.RefreshToken == nil) != (want.TokenResponse.RefreshToken == nil) {
		t.Fatalf("refresh token presence mismatch")
	}
	if len(got.TokenResponse.Scopes) != len(want.TokenResponse.Scopes) {
		t.Fatalf("scopes mismatch: %v vs %v", got.TokenResponse.Scopes, want.TokenResponse.Scopes)
	}
}

var errInjected = errInjectedErr{}

type errInjectedErr struct{}

func (errInjectedErr) Error() string { return "injected keyring failure" }
