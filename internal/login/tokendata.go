// Package login ports codex-rs/login: the ChatGPT OAuth (PKCE) sign-in flow,
// the on-disk auth.json shape and its storage backends (file / keyring / auto /
// ephemeral), token refresh and revocation, and API-key/access-token auth.
//
// It is a faithful Go port. JSON and on-disk formats match codex byte-for-byte
// after key-order canonicalization: auth.json uses the same field names,
// rename_all/rename/skip_serializing_if semantics, and 0600 file mode as the
// Rust implementation.
package login

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// TokenData is the token bundle persisted under auth.json's "tokens" field.
//
// Rust: token_data::TokenData. The id_token field uses custom serde: it is
// SERIALIZED as the raw JWT string (raw_jwt) and DESERIALIZED by parsing the
// JWT's claims into IdTokenInfo. The other fields are plain strings, and
// account_id is omitted when null is not desired (it is Option<String> with no
// skip, so it is always emitted, as null when absent).
type TokenData struct {
	// IDToken is the flat info parsed from the JWT in auth.json.
	IDToken IdTokenInfo
	// AccessToken is a JWT.
	AccessToken string
	// RefreshToken is the OAuth refresh token.
	RefreshToken string
	// AccountID is the ChatGPT account/workspace id, if present.
	AccountID *string
}

// tokenDataWire is the on-disk JSON representation of TokenData. id_token is a
// bare JWT string (matching the Rust serialize_id_token); the other fields map
// directly. Field order matches the Rust struct declaration order.
type tokenDataWire struct {
	IDToken      string  `json:"id_token"`
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	AccountID    *string `json:"account_id"`
}

// MarshalJSON encodes TokenData, writing id_token as its raw JWT string.
func (t TokenData) MarshalJSON() ([]byte, error) {
	return json.Marshal(tokenDataWire{
		IDToken:      t.IDToken.RawJWT,
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		AccountID:    t.AccountID,
	})
}

// UnmarshalJSON decodes TokenData, parsing the id_token JWT into IdTokenInfo.
func (t *TokenData) UnmarshalJSON(data []byte) error {
	var wire tokenDataWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	info, err := ParseChatgptJWTClaims(wire.IDToken)
	if err != nil {
		return err
	}
	t.IDToken = info
	t.AccessToken = wire.AccessToken
	t.RefreshToken = wire.RefreshToken
	t.AccountID = wire.AccountID
	return nil
}

// IdTokenInfo is a flat subset of useful claims from the id_token JWT.
//
// Rust: token_data::IdTokenInfo, derived Serialize/Deserialize with snake_case
// field names. Note that this struct's *own* serde form (used nowhere on the
// auth.json happy path, since TokenData overrides id_token) is preserved for
// completeness via the json tags below.
type IdTokenInfo struct {
	Email                   *string                `json:"email"`
	ChatgptPlanType         *protocol.AuthPlanType `json:"chatgpt_plan_type"`
	ChatgptUserID           *string                `json:"chatgpt_user_id"`
	ChatgptAccountID        *string                `json:"chatgpt_account_id"`
	ChatgptAccountIsFedramp bool                   `json:"chatgpt_account_is_fedramp"`
	RawJWT                  string                 `json:"raw_jwt"`
}

// GetChatgptPlanType returns the display name of the plan type, if present.
// Mirrors Rust IdTokenInfo::get_chatgpt_plan_type.
func (i IdTokenInfo) GetChatgptPlanType() *string {
	if i.ChatgptPlanType == nil {
		return nil
	}
	var s string
	switch i.ChatgptPlanType.Kind {
	case protocol.AuthPlanTypeKnown:
		s = i.ChatgptPlanType.Known.DisplayName()
	default:
		s = i.ChatgptPlanType.Unknown
	}
	return &s
}

// GetChatgptPlanTypeRaw returns the raw wire value of the plan type, if present.
// Mirrors Rust IdTokenInfo::get_chatgpt_plan_type_raw.
func (i IdTokenInfo) GetChatgptPlanTypeRaw() *string {
	if i.ChatgptPlanType == nil {
		return nil
	}
	var s string
	switch i.ChatgptPlanType.Kind {
	case protocol.AuthPlanTypeKnown:
		s = i.ChatgptPlanType.Known.RawValue()
	default:
		s = i.ChatgptPlanType.Unknown
	}
	return &s
}

// IsWorkspaceAccount reports whether the plan type is a workspace tier.
// Mirrors Rust IdTokenInfo::is_workspace_account.
func (i IdTokenInfo) IsWorkspaceAccount() bool {
	if i.ChatgptPlanType == nil || i.ChatgptPlanType.Kind != protocol.AuthPlanTypeKnown {
		return false
	}
	return i.ChatgptPlanType.Known.IsWorkspaceAccount()
}

// IsFedrampAccount reports whether the selected workspace routes through the
// FedRAMP edge. Mirrors Rust IdTokenInfo::is_fedramp_account.
func (i IdTokenInfo) IsFedrampAccount() bool { return i.ChatgptAccountIsFedramp }

// ErrInvalidIDTokenFormat mirrors the Rust IdTokenInfoError::InvalidFormat
// message ("invalid ID token format").
var ErrInvalidIDTokenFormat = errors.New("invalid ID token format")

// idClaims mirrors the Rust IdClaims deserialize helper.
type idClaims struct {
	Email   *string        `json:"email"`
	Profile *profileClaims `json:"https://api.openai.com/profile"`
	Auth    *authClaims    `json:"https://api.openai.com/auth"`
}

type profileClaims struct {
	Email *string `json:"email"`
}

type authClaims struct {
	ChatgptPlanType         *protocol.AuthPlanType `json:"chatgpt_plan_type"`
	ChatgptUserID           *string                `json:"chatgpt_user_id"`
	UserID                  *string                `json:"user_id"`
	ChatgptAccountID        *string                `json:"chatgpt_account_id"`
	ChatgptAccountIsFedramp bool                   `json:"chatgpt_account_is_fedramp"`
}

type standardJwtClaims struct {
	Exp *int64 `json:"exp"`
}

// decodeJWTPayload base64url-decodes (no padding) the payload of a 3-part JWT
// and unmarshals it into v. It mirrors token_data::decode_jwt_payload,
// including the requirement that all three segments be non-empty.
func decodeJWTPayload(jwt string, v any) error {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return ErrInvalidIDTokenFormat
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("decode ID token payload: %w", err)
	}
	if err := json.Unmarshal(payloadBytes, v); err != nil {
		return fmt.Errorf("parse ID token payload: %w", err)
	}
	return nil
}

// ParseJWTExpiration returns the JWT's `exp` claim as a time.Time, or nil when
// absent. Mirrors token_data::parse_jwt_expiration.
func ParseJWTExpiration(jwt string) (*time.Time, error) {
	var claims standardJwtClaims
	if err := decodeJWTPayload(jwt, &claims); err != nil {
		return nil, err
	}
	if claims.Exp == nil {
		return nil, nil
	}
	t := time.Unix(*claims.Exp, 0).UTC()
	return &t, nil
}

// ParseChatgptJWTClaims parses a ChatGPT id_token JWT into IdTokenInfo. Mirrors
// token_data::parse_chatgpt_jwt_claims, including the email fallback to the
// profile claim and the chatgpt_user_id fallback to user_id.
func ParseChatgptJWTClaims(jwt string) (IdTokenInfo, error) {
	var claims idClaims
	if err := decodeJWTPayload(jwt, &claims); err != nil {
		return IdTokenInfo{}, err
	}

	email := claims.Email
	if email == nil && claims.Profile != nil {
		email = claims.Profile.Email
	}

	info := IdTokenInfo{
		Email:  email,
		RawJWT: jwt,
	}
	if claims.Auth != nil {
		info.ChatgptPlanType = claims.Auth.ChatgptPlanType
		info.ChatgptUserID = claims.Auth.ChatgptUserID
		if info.ChatgptUserID == nil {
			info.ChatgptUserID = claims.Auth.UserID
		}
		info.ChatgptAccountID = claims.Auth.ChatgptAccountID
		info.ChatgptAccountIsFedramp = claims.Auth.ChatgptAccountIsFedramp
	}
	return info, nil
}
