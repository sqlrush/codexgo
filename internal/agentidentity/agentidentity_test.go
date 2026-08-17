package agentidentity

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// jwtWithPayload builds an unsigned-style JWT (header.payload.sig) whose payload
// is the JSON encoding of payload, mirroring the Rust test helper.
func jwtWithPayload(t *testing.T, payload map[string]any) string {
	t.Helper()
	enc := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	headerB64 := enc([]byte(`{"alg":"none","typ":"JWT"}`))
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return headerB64 + "." + enc(payloadBytes) + "." + enc([]byte("sig"))
}

func TestDecodeJwtClaims(t *testing.T) {
	jwt := jwtWithPayload(t, map[string]any{
		"iss":                        JWTIssuer,
		"aud":                        JWTAudience,
		"iat":                        1700000000,
		"exp":                        4000000000,
		"agent_runtime_id":           "agent-runtime-id",
		"agent_private_key":          "private-key",
		"account_id":                 "account-id",
		"chatgpt_user_id":            "user-id",
		"email":                      "user@example.com",
		"plan_type":                  "pro",
		"chatgpt_account_is_fedramp": false,
	})

	claims, err := DecodeJwtClaims(jwt)
	if err != nil {
		t.Fatalf("DecodeJwtClaims: %v", err)
	}
	if claims.AgentRuntimeID != "agent-runtime-id" {
		t.Errorf("AgentRuntimeID = %q", claims.AgentRuntimeID)
	}
	if claims.Email != "user@example.com" {
		t.Errorf("Email = %q", claims.Email)
	}
	if claims.PlanType.Kind != protocol.AuthPlanTypeKnown || claims.PlanType.Known != protocol.KnownPlanPro {
		t.Errorf("PlanType = %+v, want Known(pro)", claims.PlanType)
	}
}

func TestDecodeJwtClaimsMapsRawPlanAliases(t *testing.T) {
	jwt := jwtWithPayload(t, map[string]any{
		"plan_type": "hc",
	})
	claims, err := DecodeJwtClaims(jwt)
	if err != nil {
		t.Fatalf("DecodeJwtClaims: %v", err)
	}
	if claims.PlanType.Kind != protocol.AuthPlanTypeKnown || claims.PlanType.Known != protocol.KnownPlanEnterprise {
		t.Errorf("PlanType = %+v, want Known(enterprise)", claims.PlanType)
	}
}

func TestDecodeJwtClaimsInvalidFormat(t *testing.T) {
	cases := []string{"not-a-jwt", "a.b", "a..c", ".b.c", "a.b.", "a.b.c.d"}
	for _, jwt := range cases {
		if _, err := DecodeJwtClaims(jwt); err == nil {
			t.Errorf("DecodeJwtClaims(%q) expected error", jwt)
		}
	}
}

func TestJWKSURL(t *testing.T) {
	cases := []struct {
		base string
		want string
	}{
		{"https://chatgpt.com/backend-api", "https://chatgpt.com/backend-api/wham/agent-identities/jwks"},
		{"https://chatgpt.com/backend-api/", "https://chatgpt.com/backend-api/wham/agent-identities/jwks"},
		{"http://localhost:8080/api/codex", "http://localhost:8080/api/codex/agent-identities/jwks"},
		{"http://localhost:8080/api/codex/", "http://localhost:8080/api/codex/agent-identities/jwks"},
	}
	for _, c := range cases {
		if got := JWKSURL(c.base); got != c.want {
			t.Errorf("JWKSURL(%q) = %q, want %q", c.base, got, c.want)
		}
	}
}

func TestURLBuilders(t *testing.T) {
	if got := AgentRegistrationURL("https://x/"); got != "https://x/v1/agent/register" {
		t.Errorf("AgentRegistrationURL = %q", got)
	}
	if got := AgentTaskRegistrationURL("https://x/", "rt"); got != "https://x/v1/agent/rt/task/register" {
		t.Errorf("AgentTaskRegistrationURL = %q", got)
	}
	if got := AgentIdentityBiscuitURL("https://x/"); got != "https://x/authenticate_app_v2" {
		t.Errorf("AgentIdentityBiscuitURL = %q", got)
	}
}

func TestRequestIDFormat(t *testing.T) {
	id, err := RequestID()
	if err != nil {
		t.Fatalf("RequestID: %v", err)
	}
	if !strings.HasPrefix(id, "codex-agent-identity-") {
		t.Errorf("RequestID = %q, missing prefix", id)
	}
}

func TestGenerateAndSignRoundTrip(t *testing.T) {
	material, err := GenerateKeyMaterial()
	if err != nil {
		t.Fatalf("GenerateKeyMaterial: %v", err)
	}
	if !strings.HasPrefix(material.PublicKeySSH, "ssh-ed25519 ") {
		t.Errorf("PublicKeySSH = %q", material.PublicKeySSH)
	}

	key := Key{AgentRuntimeID: "agent-1", PrivateKeyPKCS8Base64: material.PrivateKeyPKCS8Base64}
	sig, err := SignTaskRegistrationPayload(key, "2024-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("SignTaskRegistrationPayload: %v", err)
	}

	// Verify the signature against the public key.
	der, err := base64.StdEncoding.DecodeString(material.PrivateKeyPKCS8Base64)
	if err != nil {
		t.Fatalf("decode pkcs8: %v", err)
	}
	priv, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		t.Fatalf("parse pkcs8: %v", err)
	}
	edPriv := priv.(ed25519.PrivateKey)
	sigBytes, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		t.Fatalf("decode sig: %v", err)
	}
	payload := []byte("agent-1:2024-01-01T00:00:00Z")
	if !ed25519.Verify(edPriv.Public().(ed25519.PublicKey), payload, sigBytes) {
		t.Errorf("signature did not verify")
	}
}

func TestPublicKeySSHFromPrivateKey(t *testing.T) {
	material, err := GenerateKeyMaterial()
	if err != nil {
		t.Fatalf("GenerateKeyMaterial: %v", err)
	}
	derived, err := PublicKeySSHFromPrivateKeyPKCS8Base64(material.PrivateKeyPKCS8Base64)
	if err != nil {
		t.Fatalf("PublicKeySSHFromPrivateKeyPKCS8Base64: %v", err)
	}
	if derived != material.PublicKeySSH {
		t.Errorf("derived = %q, want %q", derived, material.PublicKeySSH)
	}
}

func TestBuildBillOfMaterials(t *testing.T) {
	bom := BuildBillOfMaterials("1.2.3", "vscode")
	if bom.AgentHarnessID != "codex-app" {
		t.Errorf("AgentHarnessID = %q, want codex-app", bom.AgentHarnessID)
	}
	bom = BuildBillOfMaterials("1.2.3", "cli")
	if bom.AgentHarnessID != "codex-cli" {
		t.Errorf("AgentHarnessID = %q, want codex-cli", bom.AgentHarnessID)
	}
	if !strings.HasPrefix(bom.RunningLocation, "cli-") {
		t.Errorf("RunningLocation = %q", bom.RunningLocation)
	}
}
