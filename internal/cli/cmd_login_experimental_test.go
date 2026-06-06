package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/login"
)

// TestParseLoginArgsExperimentalFlags verifies that the experimental issuer and
// client-id flags are parsed in both the space-separated and inline (`=`) forms.
func TestParseLoginArgsExperimentalFlags(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantIssuer string
		wantClient string
	}{
		{
			name:       "space separated",
			args:       []string{"--device-auth", "--experimental_issuer", "https://issuer.example", "--experimental_client-id", "client-xyz"},
			wantIssuer: "https://issuer.example",
			wantClient: "client-xyz",
		},
		{
			name:       "inline equals",
			args:       []string{"--experimental_issuer=https://issuer2.example", "--experimental_client-id=client-abc"},
			wantIssuer: "https://issuer2.example",
			wantClient: "client-abc",
		},
		{
			name:       "issuer only",
			args:       []string{"--experimental_issuer", "https://issuer3.example"},
			wantIssuer: "https://issuer3.example",
			wantClient: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLoginArgs(tc.args)
			if err != nil {
				t.Fatalf("parseLoginArgs(%v): %v", tc.args, err)
			}
			if got.issuerBaseURL != tc.wantIssuer {
				t.Errorf("issuerBaseURL = %q, want %q", got.issuerBaseURL, tc.wantIssuer)
			}
			if got.clientID != tc.wantClient {
				t.Errorf("clientID = %q, want %q", got.clientID, tc.wantClient)
			}
		})
	}
}

// TestParseLoginArgsExperimentalMissingValue verifies a value-bearing flag with
// no following value is rejected.
func TestParseLoginArgsExperimentalMissingValue(t *testing.T) {
	if _, err := parseLoginArgs([]string{"--experimental_issuer"}); err == nil {
		t.Fatal("expected error for --experimental_issuer with no value")
	}
}

// TestDeviceCodeServerOptionsOverrides verifies the overrides flow into the
// device-code ServerOptions: an empty override falls back to the defaults, while
// non-empty overrides replace the issuer and client id.
func TestDeviceCodeServerOptionsOverrides(t *testing.T) {
	cfg := loadedConfig{CodexHome: t.TempDir()}

	defaults := deviceCodeServerOptions(cfg, "", "")
	if defaults.Issuer != login.DefaultIssuer {
		t.Errorf("default issuer = %q, want %q", defaults.Issuer, login.DefaultIssuer)
	}
	if defaults.ClientID != login.ClientID {
		t.Errorf("default client id = %q, want %q", defaults.ClientID, login.ClientID)
	}

	overridden := deviceCodeServerOptions(cfg, "https://issuer.example", "client-xyz")
	if overridden.Issuer != "https://issuer.example" {
		t.Errorf("overridden issuer = %q, want %q", overridden.Issuer, "https://issuer.example")
	}
	if overridden.ClientID != "client-xyz" {
		t.Errorf("overridden client id = %q, want %q", overridden.ClientID, "client-xyz")
	}
}

// TestOverriddenIssuerReachesDeviceCodeEndpoint verifies that an overridden
// issuer base URL is used for the device-code endpoint requests: a stub issuer
// server records the usercode request, including the overridden client id in the
// body.
func TestOverriddenIssuerReachesDeviceCodeEndpoint(t *testing.T) {
	var gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		buf, _ := io.ReadAll(r.Body)
		gotBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user_code":"ABCD-EFGH","device_auth_id":"dev-1","interval":"5"}`))
	}))
	defer server.Close()

	cfg := loadedConfig{CodexHome: t.TempDir()}
	opts := deviceCodeServerOptions(cfg, server.URL, "client-xyz")

	dc, err := login.RequestDeviceCode(context.Background(), server.Client(), opts)
	if err != nil {
		t.Fatalf("RequestDeviceCode: %v", err)
	}
	if dc.UserCode != "ABCD-EFGH" {
		t.Errorf("user code = %q, want ABCD-EFGH", dc.UserCode)
	}
	if want := "/api/accounts/deviceauth/usercode"; gotPath != want {
		t.Errorf("usercode path = %q, want %q (issuer override not applied)", gotPath, want)
	}
	if !strings.Contains(gotBody, `"client-xyz"`) {
		t.Errorf("usercode body = %q, want client id override client-xyz", gotBody)
	}
	// The verification URL is derived from the overridden issuer.
	if want := server.URL + "/codex/device"; dc.VerificationURL != want {
		t.Errorf("verification url = %q, want %q", dc.VerificationURL, want)
	}
}

// TestOverriddenIssuerReachesAuthorizeURL verifies that an overridden issuer is
// used when building the browser authorize URL (BuildAuthorizeURL), which is the
// shared URL builder for the login server flow.
func TestOverriddenIssuerReachesAuthorizeURL(t *testing.T) {
	pkce, err := login.GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}
	url := login.BuildAuthorizeURL("https://issuer.example", "client-xyz", "http://localhost:1455/auth/callback", pkce, "state-123", nil)
	if !strings.HasPrefix(url, "https://issuer.example/oauth/authorize?") {
		t.Errorf("authorize url = %q, want prefix https://issuer.example/oauth/authorize?", url)
	}
	if !strings.Contains(url, "client_id=client-xyz") {
		t.Errorf("authorize url = %q, want client_id=client-xyz", url)
	}
}
