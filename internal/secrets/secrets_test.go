package secrets

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/sqlrush/codexgo/internal/keyring"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

func TestNewSecretName(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "valid", raw: "GITHUB_TOKEN", want: "GITHUB_TOKEN"},
		{name: "digits and underscores", raw: "A1_B2_C3", want: "A1_B2_C3"},
		{name: "trims whitespace", raw: "  TOKEN  ", want: "TOKEN"},
		{name: "empty", raw: "", wantErr: true},
		{name: "whitespace only", raw: "   ", wantErr: true},
		{name: "lowercase rejected", raw: "token", wantErr: true},
		{name: "hyphen rejected", raw: "API-KEY", wantErr: true},
		{name: "dot rejected", raw: "A.B", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewSecretName(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.AsStr() != tt.want {
				t.Fatalf("got %q want %q", got.AsStr(), tt.want)
			}
			if got.String() != tt.want {
				t.Fatalf("String() got %q want %q", got.String(), tt.want)
			}
		})
	}
}

func TestNewEnvironmentScope(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "valid", raw: "my-repo", want: "my-repo"},
		{name: "trims", raw: "  repo  ", want: "repo"},
		{name: "empty", raw: "", wantErr: true},
		{name: "whitespace", raw: "  ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope, err := NewEnvironmentScope(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			id, ok := scope.EnvironmentID()
			if !ok || id != tt.want {
				t.Fatalf("got (%q,%v) want %q", id, ok, tt.want)
			}
			if scope.Kind() != ScopeEnvironment {
				t.Fatalf("expected environment scope kind")
			}
		})
	}
}

func TestCanonicalKey(t *testing.T) {
	name, err := NewSecretName("API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	env, err := NewEnvironmentScope("repo")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		scope SecretScope
		want  string
	}{
		{name: "global", scope: GlobalScope(), want: "global/API_KEY"},
		{name: "environment", scope: env, want: "env/repo/API_KEY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.scope.CanonicalKey(name); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestGlobalScopeIsDefault(t *testing.T) {
	if (SecretScope{}).Kind() != ScopeGlobal {
		t.Fatalf("zero value should be global scope")
	}
	if !GlobalScope().Equal(SecretScope{}) {
		t.Fatalf("GlobalScope must equal zero value")
	}
}

func TestBackendKindJSON(t *testing.T) {
	data, err := BackendLocal.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"local"` {
		t.Fatalf("got %s want \"local\"", data)
	}
	var k BackendKind
	if err := k.UnmarshalJSON([]byte(`"local"`)); err != nil {
		t.Fatal(err)
	}
	if k != BackendLocal {
		t.Fatalf("got %v want BackendLocal", k)
	}
	if err := k.UnmarshalJSON([]byte(`"remote"`)); err == nil {
		t.Fatalf("expected error for unknown kind")
	}
}

func TestManagerRoundTripsLocalBackend(t *testing.T) {
	codexHome := t.TempDir()
	store := keyring.NewMemoryStore()
	manager := NewManagerWithKeyringStore(codexHome, BackendLocal, store)

	scope := GlobalScope()
	name, err := NewSecretName("GITHUB_TOKEN")
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Set(scope, name, "token-1"); err != nil {
		t.Fatal(err)
	}
	got, err := manager.Get(scope, name)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != "token-1" {
		t.Fatalf("got %v want token-1", got)
	}

	listed, err := manager.List(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("got %d entries want 1", len(listed))
	}
	if listed[0].Name.AsStr() != name.AsStr() {
		t.Fatalf("got name %q want %q", listed[0].Name.AsStr(), name.AsStr())
	}

	removed, err := manager.Delete(scope, name)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatalf("expected delete to report removed")
	}
	got, err = manager.Get(scope, name)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil after delete, got %v", *got)
	}
}

func TestEnvironmentIDFallbackHasCwdPrefix(t *testing.T) {
	dir := t.TempDir()
	envID := EnvironmentIDFromCwd(dir)

	abs, err := abspath.FromAbsolutePath(dir)
	if err != nil {
		t.Fatal(err)
	}
	canon, err := abs.Canonicalize()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(canon.Path()))
	hexDigest := hex.EncodeToString(sum[:])
	want := "cwd-" + hexDigest[:12]
	if envID != want {
		t.Fatalf("got %q want %q", envID, want)
	}
}

func TestSetFailsWhenKeyringIsUnavailable(t *testing.T) {
	codexHome := t.TempDir()
	store := keyring.NewMemoryStore()
	account := computeKeyringAccount(codexHome)
	store.SetError(account, errors.New("boom"))

	backend := NewLocalSecretsBackend(codexHome, store)
	name, err := NewSecretName("TEST_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	err = backend.Set(GlobalScope(), name, "secret-value")
	if err == nil {
		t.Fatalf("expected error when keyring load fails")
	}
	if !errStringContains(err, "failed to load secrets key from keyring") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func errStringContains(err error, sub string) bool {
	return err != nil && contains(err.Error(), sub)
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
