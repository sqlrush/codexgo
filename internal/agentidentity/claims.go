// Package agentidentity ports codex-rs/agent-identity: agent-identity JWT
// claims, key material generation, signing helpers, and the URL builders used
// to register agent tasks and fetch the agent-identity JWKS.
//
// It is a faithful Go port. The unverified-claims decode path (used by the
// login package to populate the on-disk agent_identity record) mirrors the Rust
// decode_agent_identity_jwt(jwt, /*jwks*/ None) behavior byte-for-byte: it
// splits the JWT into three non-empty parts and base64url-decodes the payload
// without verifying the signature. The verified path (with a JWKS) validates
// the RS256 signature, issuer, and audience via github.com/golang-jwt/jwt/v5.
package agentidentity

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// JWT validation constants, mirroring the Rust constants of the same name.
const (
	// JWTAudience is the required `aud` claim for a verified agent-identity JWT.
	JWTAudience = "codex-app-server"
	// JWTIssuer is the required `iss` claim for a verified agent-identity JWT.
	JWTIssuer = "https://chatgpt.com/codex-backend/agent-identity"
)

// JwtClaims are the claims carried by an Agent Identity JWT.
//
// Rust: AgentIdentityJwtClaims, derived Deserialize. All fields are required on
// the verified path; the unverified path tolerates missing fields per Go JSON
// semantics (they zero-value), matching how callers consume the record.
type JwtClaims struct {
	Iss                     string                `json:"iss"`
	Aud                     string                `json:"aud"`
	Iat                     int64                 `json:"iat"`
	Exp                     int64                 `json:"exp"`
	AgentRuntimeID          string                `json:"agent_runtime_id"`
	AgentPrivateKey         string                `json:"agent_private_key"`
	AccountID               string                `json:"account_id"`
	ChatgptUserID           string                `json:"chatgpt_user_id"`
	Email                   string                `json:"email"`
	PlanType                protocol.AuthPlanType `json:"plan_type"`
	ChatgptAccountIsFedramp bool                  `json:"chatgpt_account_is_fedramp"`
}

// ErrInvalidJWTFormat is returned when a JWT does not have three non-empty,
// dot-separated parts, matching the Rust "invalid agent identity JWT format".
var ErrInvalidJWTFormat = errors.New("invalid agent identity JWT format")

// jwtParts splits a compact JWT into its three parts, requiring exactly three
// non-empty segments. This mirrors the Rust split('.') logic, including the
// rejection of a fourth segment.
func jwtParts(jwt string) (header, payload, signature string, err error) {
	first, rest, ok := cut(jwt, '.')
	if !ok {
		return "", "", "", ErrInvalidJWTFormat
	}
	payload, sigRest, ok := cut(rest, '.')
	if !ok {
		return "", "", "", ErrInvalidJWTFormat
	}
	// sigRest must be a single non-empty segment with no further '.'.
	if sigRest == "" {
		return "", "", "", ErrInvalidJWTFormat
	}
	for i := 0; i < len(sigRest); i++ {
		if sigRest[i] == '.' {
			return "", "", "", ErrInvalidJWTFormat
		}
	}
	if first == "" || payload == "" {
		return "", "", "", ErrInvalidJWTFormat
	}
	return first, payload, sigRest, nil
}

// cut splits s around the first occurrence of sep, returning the text before and
// after, and whether sep was present. It is the std strings.Cut for a byte sep,
// inlined to avoid importing strings for a single call.
func cut(s string, sep byte) (before, after string, found bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

// decodeJWTPayload base64url-decodes (no padding) the payload segment of a JWT
// and unmarshals it into v. It mirrors decode_agent_identity_jwt_payload.
func decodeJWTPayload(jwt string, v any) error {
	_, payloadB64, _, err := jwtParts(jwt)
	if err != nil {
		return err
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return fmt.Errorf("agent identity JWT payload is not valid base64url: %w", err)
	}
	if err := json.Unmarshal(payloadBytes, v); err != nil {
		return fmt.Errorf("agent identity JWT payload is not valid JSON: %w", err)
	}
	return nil
}

// DecodeJwtClaims decodes the claims of an agent-identity JWT WITHOUT verifying
// its signature. This is the Rust decode_agent_identity_jwt(jwt, None) path:
// callers that need a verified decode must use Verifier with a fetched JWKS.
func DecodeJwtClaims(jwt string) (JwtClaims, error) {
	var claims JwtClaims
	if err := decodeJWTPayload(jwt, &claims); err != nil {
		return JwtClaims{}, err
	}
	return claims, nil
}
