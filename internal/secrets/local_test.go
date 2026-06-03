package secrets

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sqlrush/codexgo/internal/keyring"
)

func TestLoadFileRejectsNewerSchemaVersions(t *testing.T) {
	codexHome := t.TempDir()
	store := keyring.NewMemoryStore()
	backend := NewLocalSecretsBackend(codexHome, store)

	file := secretsFile{version: secretsVersion + 1, secrets: map[string]string{}}
	if err := backend.saveFile(file); err != nil {
		t.Fatal(err)
	}

	_, err := backend.loadFile()
	if err == nil {
		t.Fatalf("expected error for newer schema version")
	}
	if !errStringContains(err, "newer than supported version") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSaveFileDoesNotLeaveTempFiles(t *testing.T) {
	codexHome := t.TempDir()
	store := keyring.NewMemoryStore()
	backend := NewLocalSecretsBackend(codexHome, store)

	scope := GlobalScope()
	name, err := NewSecretName("TEST_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Set(scope, name, "one"); err != nil {
		t.Fatal(err)
	}
	if err := backend.Set(scope, name, "two"); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(backend.secretsDir())
	if err != nil {
		t.Fatal(err)
	}
	var filenames []string
	for _, e := range entries {
		filenames = append(filenames, e.Name())
	}
	if len(filenames) != 1 || filenames[0] != localSecretsFilename {
		t.Fatalf("got files %v want [%s]", filenames, localSecretsFilename)
	}

	got, err := backend.Get(scope, name)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != "two" {
		t.Fatalf("got %v want two", got)
	}
}

func TestLocalBackendEnvironmentScopeRoundTrip(t *testing.T) {
	codexHome := t.TempDir()
	store := keyring.NewMemoryStore()
	backend := NewLocalSecretsBackend(codexHome, store)

	envA, err := NewEnvironmentScope("repo-a")
	if err != nil {
		t.Fatal(err)
	}
	envB, err := NewEnvironmentScope("repo-b")
	if err != nil {
		t.Fatal(err)
	}
	name, err := NewSecretName("API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	global := GlobalScope()

	if err := backend.Set(global, name, "g"); err != nil {
		t.Fatal(err)
	}
	if err := backend.Set(envA, name, "a"); err != nil {
		t.Fatal(err)
	}
	if err := backend.Set(envB, name, "b"); err != nil {
		t.Fatal(err)
	}

	all, err := backend.List(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("got %d entries want 3", len(all))
	}

	filtered, err := backend.List(&envA)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 {
		t.Fatalf("got %d filtered entries want 1", len(filtered))
	}
	id, ok := filtered[0].Scope.EnvironmentID()
	if !ok || id != "repo-a" {
		t.Fatalf("got filtered scope (%q,%v) want repo-a", id, ok)
	}

	gotA, err := backend.Get(envA, name)
	if err != nil {
		t.Fatal(err)
	}
	if gotA == nil || *gotA != "a" {
		t.Fatalf("got %v want a", gotA)
	}
}

func TestSetRejectsEmptyValue(t *testing.T) {
	codexHome := t.TempDir()
	backend := NewLocalSecretsBackend(codexHome, keyring.NewMemoryStore())
	name, err := NewSecretName("X")
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Set(GlobalScope(), name, ""); err == nil {
		t.Fatalf("expected error for empty value")
	}
}

func TestGetMissingReturnsNil(t *testing.T) {
	codexHome := t.TempDir()
	backend := NewLocalSecretsBackend(codexHome, keyring.NewMemoryStore())
	name, err := NewSecretName("MISSING")
	if err != nil {
		t.Fatal(err)
	}
	got, err := backend.Get(GlobalScope(), name)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil for missing secret, got %v", *got)
	}
}

func TestDeleteMissingReturnsFalse(t *testing.T) {
	codexHome := t.TempDir()
	backend := NewLocalSecretsBackend(codexHome, keyring.NewMemoryStore())
	name, err := NewSecretName("MISSING")
	if err != nil {
		t.Fatal(err)
	}
	removed, err := backend.Delete(GlobalScope(), name)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatalf("expected delete of missing secret to report false")
	}
}

func TestParseCanonicalKey(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		wantOK     bool
		wantGlobal bool
		wantEnv    string
		wantName   string
	}{
		{name: "global", key: "global/API_KEY", wantOK: true, wantGlobal: true, wantName: "API_KEY"},
		{name: "env", key: "env/repo/TOKEN", wantOK: true, wantEnv: "repo", wantName: "TOKEN"},
		{name: "global trailing", key: "global/A/B", wantOK: false},
		{name: "global only", key: "global", wantOK: false},
		{name: "env missing name", key: "env/repo", wantOK: false},
		{name: "env trailing", key: "env/repo/A/B", wantOK: false},
		{name: "unknown prefix", key: "other/X", wantOK: false},
		{name: "invalid name", key: "global/lower", wantOK: false},
		{name: "empty", key: "", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := parseCanonicalKey(tt.key)
			if ok != tt.wantOK {
				t.Fatalf("ok got %v want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if tt.wantGlobal && entry.Scope.Kind() != ScopeGlobal {
				t.Fatalf("expected global scope")
			}
			if tt.wantEnv != "" {
				id, _ := entry.Scope.EnvironmentID()
				if id != tt.wantEnv {
					t.Fatalf("env id got %q want %q", id, tt.wantEnv)
				}
			}
			if entry.Name.AsStr() != tt.wantName {
				t.Fatalf("name got %q want %q", entry.Name.AsStr(), tt.wantName)
			}
		})
	}
}

func TestSecretsFileJSONFormat(t *testing.T) {
	file := secretsFile{
		version: 1,
		secrets: map[string]string{
			"global/B": "2",
			"global/A": "1",
			"env/r/C":  "3",
		},
	}
	data, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	// Keys must be sorted and version first.
	want := `{"version":1,"secrets":{"env/r/C":"3","global/A":"1","global/B":"2"}}`
	if string(data) != want {
		t.Fatalf("got %s want %s", data, want)
	}

	var round secretsFile
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatal(err)
	}
	if round.version != 1 || len(round.secrets) != 3 || round.secrets["global/A"] != "1" {
		t.Fatalf("round trip mismatch: %+v", round)
	}
}

func TestSecretsFileUnmarshalMissingSecrets(t *testing.T) {
	var f secretsFile
	if err := json.Unmarshal([]byte(`{"version":1}`), &f); err != nil {
		t.Fatal(err)
	}
	if f.secrets == nil {
		t.Fatalf("expected non-nil secrets map")
	}
}

func TestLoadFilePersistsAcrossInstances(t *testing.T) {
	codexHome := t.TempDir()
	store := keyring.NewMemoryStore()

	name, err := NewSecretName("PERSIST")
	if err != nil {
		t.Fatal(err)
	}
	b1 := NewLocalSecretsBackend(codexHome, store)
	if err := b1.Set(GlobalScope(), name, "value"); err != nil {
		t.Fatal(err)
	}

	// A fresh backend instance sharing the same keyring decrypts the file.
	b2 := NewLocalSecretsBackend(codexHome, store)
	got, err := b2.Get(GlobalScope(), name)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != "value" {
		t.Fatalf("got %v want value", got)
	}

	// Confirm the on-disk file exists and is non-empty ciphertext.
	path := filepath.Join(codexHome, "secrets", localSecretsFilename)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatalf("expected non-empty secrets file")
	}
}

func TestKeyringMessageUnwrapsStoreError(t *testing.T) {
	// Produce a real *keyring.StoreError by injecting an error into a
	// MemoryStore and triggering a Load failure.
	store := keyring.NewMemoryStore()
	store.SetError("acct", errors.New("inner failure"))
	_, wrapped := store.Load("svc", "acct")
	if wrapped == nil {
		t.Fatalf("expected error from store.Load")
	}
	msg := keyringMessage(wrapped)
	if msg.Error() != "inner failure" {
		t.Fatalf("got %q want inner failure", msg.Error())
	}
	// A plain error passes through unchanged.
	plain := errors.New("plain")
	if keyringMessage(plain).Error() != "plain" {
		t.Fatalf("plain error should pass through")
	}
}
