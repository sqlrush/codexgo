package agentidentity

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"math/big"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// signedRS256JWT builds a real RS256-signed JWT with the given kid and claims.
func signedRS256JWT(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

// jwksForKey produces a JWKSet containing the RSA public key under kid.
func jwksForKey(kid string, pub *rsa.PublicKey) *JWKSet {
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	return &JWKSet{Keys: []JWK{{Kty: "RSA", Kid: kid, Use: "sig", Alg: "RS256", N: n, E: e}}}
}

func validClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"iss":                        JWTIssuer,
		"aud":                        JWTAudience,
		"iat":                        time.Now().Add(-time.Minute).Unix(),
		"exp":                        time.Now().Add(time.Hour).Unix(),
		"agent_runtime_id":           "rt",
		"agent_private_key":          "pk",
		"account_id":                 "acct",
		"chatgpt_user_id":            "uid",
		"email":                      "agent@example.com",
		"plan_type":                  "pro",
		"chatgpt_account_is_fedramp": false,
	}
}

func TestVerifyJwtClaimsValid(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	token := signedRS256JWT(t, key, "test-key", validClaims())
	jwks := jwksForKey("test-key", &key.PublicKey)

	claims, err := VerifyJwtClaims(token, jwks)
	if err != nil {
		t.Fatalf("VerifyJwtClaims: %v", err)
	}
	if claims.AgentRuntimeID != "rt" || claims.Email != "agent@example.com" {
		t.Errorf("claims mismatch: %+v", claims)
	}
}

func TestVerifyJwtClaimsUntrustedKid(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	token := signedRS256JWT(t, key, "other-key", validClaims())
	jwks := jwksForKey("test-key", &key.PublicKey)
	if _, err := VerifyJwtClaims(token, jwks); err == nil {
		t.Errorf("expected error for untrusted kid")
	}
}

func TestVerifyJwtClaimsWrongSignature(t *testing.T) {
	signingKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	token := signedRS256JWT(t, signingKey, "test-key", validClaims())
	// JWKS advertises a different public key under the same kid.
	jwks := jwksForKey("test-key", &otherKey.PublicKey)
	if _, err := VerifyJwtClaims(token, jwks); err == nil {
		t.Errorf("expected signature verification failure")
	}
}

func TestVerifyJwtClaimsWrongAudience(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	claims := validClaims()
	claims["aud"] = "someone-else"
	token := signedRS256JWT(t, key, "test-key", claims)
	jwks := jwksForKey("test-key", &key.PublicKey)
	if _, err := VerifyJwtClaims(token, jwks); err == nil {
		t.Errorf("expected audience mismatch failure")
	}
}

func TestVerifyJwtClaimsWrongIssuer(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	claims := validClaims()
	claims["iss"] = "https://evil.example/issuer"
	token := signedRS256JWT(t, key, "test-key", claims)
	jwks := jwksForKey("test-key", &key.PublicKey)
	if _, err := VerifyJwtClaims(token, jwks); err == nil {
		t.Errorf("expected issuer mismatch failure")
	}
}

func TestVerifyJwtClaimsNilJWKSFallsBackToUnverified(t *testing.T) {
	// With no JWKS, VerifyJwtClaims must decode the payload without verification.
	jwt := jwtWithPayload(t, map[string]any{"agent_runtime_id": "rt-unverified"})
	claims, err := VerifyJwtClaims(jwt, nil)
	if err != nil {
		t.Fatalf("VerifyJwtClaims(nil jwks): %v", err)
	}
	if claims.AgentRuntimeID != "rt-unverified" {
		t.Errorf("AgentRuntimeID = %q", claims.AgentRuntimeID)
	}
}

func TestParseJWKSetRejectsInvalid(t *testing.T) {
	if _, err := ParseJWKSet([]byte("not json")); err == nil {
		t.Errorf("expected error parsing invalid JWKS")
	}
}

func TestJWKRSAPublicKeyRejectsNonRSA(t *testing.T) {
	jwk := JWK{Kty: "EC", Kid: "k"}
	if _, err := jwk.rsaPublicKey(); err == nil {
		t.Errorf("expected error for non-RSA key")
	}
}

func TestRustOSNameMapsDarwin(t *testing.T) {
	// rustOSName must never return Go's "darwin"; it maps to Rust's "macos".
	got := rustOSName()
	if got == "darwin" {
		t.Errorf("rustOSName returned %q, want Rust convention (macos)", got)
	}
}

// roundtrip ensures a JWK encodes a usable public key for verification.
func TestJWKRSAPublicKeyRoundTrip(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwks := jwksForKey("k", &key.PublicKey)
	jwk, ok := jwks.find("k")
	if !ok {
		t.Fatalf("find failed")
	}
	pub, err := jwk.rsaPublicKey()
	if err != nil {
		t.Fatalf("rsaPublicKey: %v", err)
	}
	if pub.N.Cmp(key.PublicKey.N) != 0 || pub.E != key.PublicKey.E {
		t.Errorf("public key mismatch")
	}
}
