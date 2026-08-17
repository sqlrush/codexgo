package mcp

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// OAuthTokenResponse mirrors the JSON shape of the oauth2 crate's
// StandardTokenResponse used by the reference rmcp client. The keyring/file
// blobs persisted by codex serialize this exact shape, so the field names,
// ordering rules, and skip behavior must match for byte-for-byte compatibility.
//
//   - access_token: always present.
//   - token_type: always present; lowercase (for example "bearer").
//   - expires_in: optional number of seconds; omitted when absent.
//   - refresh_token: optional; omitted when absent.
//   - scope: optional space-delimited scope string; omitted when absent.
//
// Extra vendor fields are preserved verbatim through Extra so unknown keys
// round-trip unchanged.
type OAuthTokenResponse struct {
	AccessToken  string  `json:"access_token"`
	TokenType    string  `json:"token_type"`
	ExpiresIn    *uint64 `json:"expires_in,omitempty"`
	RefreshToken *string `json:"refresh_token,omitempty"`
	// Scopes holds the parsed scope list. On the wire scopes are a single
	// space-delimited "scope" string; an empty list omits the key.
	Scopes []string `json:"-"`
	// Extra preserves any additional vendor fields not modeled above.
	Extra map[string]json.RawMessage `json:"-"`
}

// MarshalJSON emits the oauth2 StandardTokenResponse wire shape, joining scopes
// into a single space-delimited "scope" string and flattening Extra.
func (r OAuthTokenResponse) MarshalJSON() ([]byte, error) {
	out := map[string]json.RawMessage{}
	for k, v := range r.Extra {
		out[k] = v
	}
	access, err := json.Marshal(r.AccessToken)
	if err != nil {
		return nil, err
	}
	out["access_token"] = access
	tokenType, err := json.Marshal(r.TokenType)
	if err != nil {
		return nil, err
	}
	out["token_type"] = tokenType
	if r.ExpiresIn != nil {
		v, err := json.Marshal(*r.ExpiresIn)
		if err != nil {
			return nil, err
		}
		out["expires_in"] = v
	}
	if r.RefreshToken != nil {
		v, err := json.Marshal(*r.RefreshToken)
		if err != nil {
			return nil, err
		}
		out["refresh_token"] = v
	}
	if len(r.Scopes) > 0 {
		v, err := json.Marshal(strings.Join(r.Scopes, " "))
		if err != nil {
			return nil, err
		}
		out["scope"] = v
	}
	return marshalSortedObject(out)
}

// UnmarshalJSON reads the oauth2 StandardTokenResponse wire shape, splitting the
// space-delimited "scope" string into Scopes and capturing unknown keys in Extra.
func (r *OAuthTokenResponse) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := OAuthTokenResponse{Extra: map[string]json.RawMessage{}}
	for key, value := range raw {
		switch key {
		case "access_token":
			if err := json.Unmarshal(value, &out.AccessToken); err != nil {
				return err
			}
		case "token_type":
			if err := json.Unmarshal(value, &out.TokenType); err != nil {
				return err
			}
		case "expires_in":
			var v uint64
			if err := json.Unmarshal(value, &v); err != nil {
				return err
			}
			out.ExpiresIn = &v
		case "refresh_token":
			var v string
			if err := json.Unmarshal(value, &v); err != nil {
				return err
			}
			out.RefreshToken = &v
		case "scope":
			var v string
			if err := json.Unmarshal(value, &v); err != nil {
				return err
			}
			out.Scopes = splitScopes(v)
		default:
			out.Extra[key] = value
		}
	}
	if len(out.Extra) == 0 {
		out.Extra = nil
	}
	*r = out
	return nil
}

// StoredOAuthTokens is the persisted credential record for an MCP server. It is a
// faithful port of the reference StoredOAuthTokens struct, including field names
// and the optional expires_at (epoch milliseconds).
type StoredOAuthTokens struct {
	ServerName    string             `json:"server_name"`
	URL           string             `json:"url"`
	ClientID      string             `json:"client_id"`
	TokenResponse OAuthTokenResponse `json:"token_response"`
	ExpiresAt     *uint64            `json:"expires_at,omitempty"`
}

// AccessToken returns the bearer access token, used when falling back to header
// authentication.
func (s StoredOAuthTokens) AccessToken() string {
	return s.TokenResponse.AccessToken
}

// ComputeExpiresAtMillis derives an absolute expiry (epoch milliseconds) from the
// token's expires_in relative to now, mirroring the reference
// compute_expires_at_millis. It returns nil when expires_in is absent.
func (r OAuthTokenResponse) ComputeExpiresAtMillis(now time.Time) *uint64 {
	if r.ExpiresIn == nil {
		return nil
	}
	expiry := now.Add(time.Duration(*r.ExpiresIn) * time.Second)
	millis := expiry.UnixMilli()
	if millis < 0 {
		zero := uint64(0)
		return &zero
	}
	v := uint64(millis)
	return &v
}

// splitScopes splits a space-delimited scope string into its parts, dropping
// empty fields.
func splitScopes(scope string) []string {
	parts := strings.Fields(scope)
	if len(parts) == 0 {
		return nil
	}
	return parts
}

// marshalSortedObject serializes a map with keys in sorted order so encoded
// output is deterministic. Codex canonicalizes key order before byte comparison,
// so deterministic ordering keeps round-trips stable.
func marshalSortedObject(obj map[string]json.RawMessage) ([]byte, error) {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		keyJSON, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		sb.Write(keyJSON)
		sb.WriteByte(':')
		sb.Write(obj[k])
	}
	sb.WriteByte('}')
	return []byte(sb.String()), nil
}
