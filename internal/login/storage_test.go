package login

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/keyring"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// TestAuthDotJsonSerializeShape verifies the on-disk JSON shape and serde
// semantics: auth_mode and the skip-if-none fields are omitted when nil, while
// OPENAI_API_KEY is always emitted (as null when absent).
func TestAuthDotJsonSerializeShape(t *testing.T) {
	apiMode := appserverproto.AuthModeApiKey
	key := "sk-test"
	auth := AuthDotJson{AuthMode: &apiMode, OpenAIAPIKey: &key}
	data, err := json.Marshal(auth)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(data)
	for _, want := range []string{`"auth_mode":"apikey"`, `"OPENAI_API_KEY":"sk-test"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	for _, absent := range []string{"tokens", "last_refresh", "agent_identity"} {
		if strings.Contains(got, absent) {
			t.Errorf("unexpected field %q in %q", absent, got)
		}
	}

	// Empty auth: only OPENAI_API_KEY (null) is emitted.
	empty := AuthDotJson{}
	data, err = json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal empty: %v", err)
	}
	if string(data) != `{"OPENAI_API_KEY":null}` {
		t.Errorf("empty auth json = %s, want {\"OPENAI_API_KEY\":null}", data)
	}
}

func TestFileAuthStorageRoundTrip(t *testing.T) {
	home := t.TempDir()
	mode := appserverproto.AuthModeChatgpt
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	jwt := fakeJWT(t, map[string]any{"email": "user@example.com"})
	info, err := ParseChatgptJWTClaims(jwt)
	if err != nil {
		t.Fatalf("parse jwt: %v", err)
	}
	auth := &AuthDotJson{
		AuthMode:    &mode,
		Tokens:      &TokenData{IDToken: info, AccessToken: "acc", RefreshToken: "ref"},
		LastRefresh: &ts,
	}

	if err := SaveAuth(home, auth, config.AuthCredentialsStoreFile); err != nil {
		t.Fatalf("SaveAuth: %v", err)
	}

	// File must be 0600 and pretty-printed (2-space indent).
	fi, err := os.Stat(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("auth.json perm = %o, want 600", perm)
	}
	raw, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(raw), "\n  \"") {
		t.Errorf("auth.json not pretty-printed: %q", raw)
	}

	loaded, err := LoadAuthDotJson(home, config.AuthCredentialsStoreFile)
	if err != nil {
		t.Fatalf("LoadAuthDotJson: %v", err)
	}
	if loaded == nil || loaded.Tokens == nil || loaded.Tokens.AccessToken != "acc" {
		t.Fatalf("loaded mismatch: %+v", loaded)
	}
	if loaded.LastRefresh == nil || !loaded.LastRefresh.Equal(ts) {
		t.Errorf("last_refresh = %v, want %v", loaded.LastRefresh, ts)
	}

	// Logout removes the file.
	removed, err := Logout(home, config.AuthCredentialsStoreFile)
	if err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if !removed {
		t.Errorf("Logout reported nothing removed")
	}
	if again, _ := Logout(home, config.AuthCredentialsStoreFile); again {
		t.Errorf("second Logout should report false")
	}
}

func TestLoadAuthDotJsonMissingReturnsNil(t *testing.T) {
	home := t.TempDir()
	loaded, err := LoadAuthDotJson(home, config.AuthCredentialsStoreFile)
	if err != nil {
		t.Fatalf("LoadAuthDotJson: %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil for missing auth.json, got %+v", loaded)
	}
}

func TestKeyringStorageRoundTrip(t *testing.T) {
	home := t.TempDir()
	store := keyring.NewMemoryStore()
	storage := createAuthStorageWithKeyring(home, config.AuthCredentialsStoreKeyring, store)

	key := "sk-keyring"
	apiMode := appserverproto.AuthModeApiKey
	auth := &AuthDotJson{AuthMode: &apiMode, OpenAIAPIKey: &key}
	if err := storage.save(auth); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Keyring stores compact (non-pretty) JSON under "Codex Auth"/store key.
	storeKey, err := computeStoreKey(home)
	if err != nil {
		t.Fatalf("computeStoreKey: %v", err)
	}
	if !strings.HasPrefix(storeKey, "cli|") || len(storeKey) != len("cli|")+16 {
		t.Errorf("store key shape = %q", storeKey)
	}
	saved, ok := store.SavedValue(storeKey)
	if !ok {
		t.Fatalf("nothing saved under %q", storeKey)
	}
	if strings.Contains(saved, "\n") {
		t.Errorf("keyring value should be compact json: %q", saved)
	}

	loaded, err := storage.load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil || loaded.OpenAIAPIKey == nil || *loaded.OpenAIAPIKey != key {
		t.Fatalf("loaded mismatch: %+v", loaded)
	}

	removed, err := storage.delete()
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !removed {
		t.Errorf("delete reported false")
	}
}

func TestEphemeralStorageIsolatedPerHome(t *testing.T) {
	homeA := t.TempDir()
	homeB := t.TempDir()
	key := "sk-ephemeral"
	apiMode := appserverproto.AuthModeApiKey
	auth := &AuthDotJson{AuthMode: &apiMode, OpenAIAPIKey: &key}

	if err := SaveAuth(homeA, auth, config.AuthCredentialsStoreEphemeral); err != nil {
		t.Fatalf("SaveAuth: %v", err)
	}
	loadedA, err := LoadAuthDotJson(homeA, config.AuthCredentialsStoreEphemeral)
	if err != nil {
		t.Fatalf("load A: %v", err)
	}
	if loadedA == nil || loadedA.OpenAIAPIKey == nil || *loadedA.OpenAIAPIKey != key {
		t.Fatalf("home A mismatch: %+v", loadedA)
	}
	loadedB, err := LoadAuthDotJson(homeB, config.AuthCredentialsStoreEphemeral)
	if err != nil {
		t.Fatalf("load B: %v", err)
	}
	if loadedB != nil {
		t.Errorf("home B should be empty: %+v", loadedB)
	}
	// Ephemeral store must not touch disk.
	if _, statErr := os.Stat(filepath.Join(homeA, "auth.json")); !os.IsNotExist(statErr) {
		t.Errorf("ephemeral storage unexpectedly wrote auth.json")
	}

	if _, err := Logout(homeA, config.AuthCredentialsStoreEphemeral); err != nil {
		t.Fatalf("Logout A: %v", err)
	}
}

func TestComputeStoreKeyStable(t *testing.T) {
	home := t.TempDir()
	a, err := computeStoreKey(home)
	if err != nil {
		t.Fatalf("computeStoreKey: %v", err)
	}
	b, err := computeStoreKey(home)
	if err != nil {
		t.Fatalf("computeStoreKey: %v", err)
	}
	if a != b {
		t.Errorf("store key not stable: %q vs %q", a, b)
	}
}

func TestResolvedModeDefaults(t *testing.T) {
	// No auth_mode, with API key -> apikey.
	key := "sk"
	if got := (AuthDotJson{OpenAIAPIKey: &key}).resolvedMode(); got != appserverproto.AuthModeApiKey {
		t.Errorf("resolvedMode (api key) = %q, want apikey", got)
	}
	// No auth_mode, no key -> chatgpt.
	if got := (AuthDotJson{}).resolvedMode(); got != appserverproto.AuthModeChatgpt {
		t.Errorf("resolvedMode (empty) = %q, want chatgpt", got)
	}
	// Explicit mode wins.
	mode := appserverproto.AuthModeAgentIdentity
	if got := (AuthDotJson{AuthMode: &mode}).resolvedMode(); got != appserverproto.AuthModeAgentIdentity {
		t.Errorf("resolvedMode (explicit) = %q, want agentIdentity", got)
	}
}

func TestStorageModeForcesEphemeralForExternalTokens(t *testing.T) {
	external := appserverproto.AuthModeChatgptAuthTokens
	auth := AuthDotJson{AuthMode: &external}
	if got := auth.storageMode(config.AuthCredentialsStoreFile); got != config.AuthCredentialsStoreEphemeral {
		t.Errorf("storageMode (external tokens) = %q, want ephemeral", got)
	}
	chat := appserverproto.AuthModeChatgpt
	auth = AuthDotJson{AuthMode: &chat}
	if got := auth.storageMode(config.AuthCredentialsStoreFile); got != config.AuthCredentialsStoreFile {
		t.Errorf("storageMode (chatgpt) = %q, want file", got)
	}
}

func TestAgentIdentityRecordFromJWT(t *testing.T) {
	jwt := fakeJWT(t, map[string]any{
		"agent_runtime_id":           "rt-1",
		"agent_private_key":          "pk",
		"account_id":                 "acct",
		"chatgpt_user_id":            "uid",
		"email":                      "agent@example.com",
		"plan_type":                  "pro",
		"chatgpt_account_is_fedramp": true,
	})
	record, err := AgentIdentityAuthRecordFromJWT(jwt)
	if err != nil {
		t.Fatalf("AgentIdentityAuthRecordFromJWT: %v", err)
	}
	if record.AgentRuntimeID != "rt-1" || record.AccountID != "acct" || record.Email != "agent@example.com" {
		t.Errorf("record mismatch: %+v", record)
	}
	if !record.ChatgptAccountIsFedramp {
		t.Errorf("expected fedramp true")
	}
	if record.PlanType != protocol.PlanTypePro {
		t.Errorf("plan type = %v, want pro", record.PlanType)
	}
}
