package login

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// fakeJWT builds a header.payload.sig JWT whose payload is JSON, mirroring the
// Rust token_data test helper.
func fakeJWT(t *testing.T, payload map[string]any) string {
	t.Helper()
	enc := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	headerB64 := enc([]byte(`{"alg":"none","typ":"JWT"}`))
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return headerB64 + "." + enc(payloadBytes) + "." + enc([]byte("sig"))
}

func strp(s string) *string { return &s }

func TestParseChatgptJWTClaims(t *testing.T) {
	cases := []struct {
		name         string
		payload      map[string]any
		wantEmail    *string
		wantPlanDisp *string
		wantWorkspc  bool
		wantFedramp  bool
		wantAccount  *string
	}{
		{
			name: "email and pro plan",
			payload: map[string]any{
				"email":                       "user@example.com",
				"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "pro"},
			},
			wantEmail:    strp("user@example.com"),
			wantPlanDisp: strp("Pro"),
		},
		{
			name: "go plan",
			payload: map[string]any{
				"email":                       "user@example.com",
				"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "go"},
			},
			wantEmail:    strp("user@example.com"),
			wantPlanDisp: strp("Go"),
		},
		{
			name: "hc maps to enterprise workspace",
			payload: map[string]any{
				"email":                       "user@example.com",
				"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "hc"},
			},
			wantEmail:    strp("user@example.com"),
			wantPlanDisp: strp("Enterprise"),
			wantWorkspc:  true,
		},
		{
			name:      "missing fields",
			payload:   map[string]any{"sub": "123"},
			wantEmail: nil,
		},
		{
			name: "fedramp account",
			payload: map[string]any{
				"email": "user@example.com",
				"https://api.openai.com/auth": map[string]any{
					"chatgpt_account_id":         "account-fed",
					"chatgpt_account_is_fedramp": true,
				},
			},
			wantEmail:   strp("user@example.com"),
			wantFedramp: true,
			wantAccount: strp("account-fed"),
		},
		{
			name: "email fallback to profile",
			payload: map[string]any{
				"https://api.openai.com/profile": map[string]any{"email": "profile@example.com"},
			},
			wantEmail: strp("profile@example.com"),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			info, err := ParseChatgptJWTClaims(fakeJWT(t, c.payload))
			if err != nil {
				t.Fatalf("ParseChatgptJWTClaims: %v", err)
			}
			if !eqStrPtr(info.Email, c.wantEmail) {
				t.Errorf("Email = %v, want %v", derefStr(info.Email), derefStr(c.wantEmail))
			}
			if !eqStrPtr(info.GetChatgptPlanType(), c.wantPlanDisp) {
				t.Errorf("plan display = %v, want %v", derefStr(info.GetChatgptPlanType()), derefStr(c.wantPlanDisp))
			}
			if info.IsWorkspaceAccount() != c.wantWorkspc {
				t.Errorf("IsWorkspaceAccount = %v, want %v", info.IsWorkspaceAccount(), c.wantWorkspc)
			}
			if info.IsFedrampAccount() != c.wantFedramp {
				t.Errorf("IsFedrampAccount = %v, want %v", info.IsFedrampAccount(), c.wantFedramp)
			}
			if !eqStrPtr(info.ChatgptAccountID, c.wantAccount) {
				t.Errorf("ChatgptAccountID = %v, want %v", derefStr(info.ChatgptAccountID), derefStr(c.wantAccount))
			}
		})
	}
}

func TestParseJWTExpiration(t *testing.T) {
	jwt := fakeJWT(t, map[string]any{"exp": int64(1700000000)})
	exp, err := ParseJWTExpiration(jwt)
	if err != nil {
		t.Fatalf("ParseJWTExpiration: %v", err)
	}
	if exp == nil || !exp.Equal(time.Unix(1700000000, 0).UTC()) {
		t.Errorf("exp = %v, want 1700000000", exp)
	}

	missing := fakeJWT(t, map[string]any{"sub": "123"})
	exp, err = ParseJWTExpiration(missing)
	if err != nil {
		t.Fatalf("ParseJWTExpiration (missing): %v", err)
	}
	if exp != nil {
		t.Errorf("exp = %v, want nil", exp)
	}

	if _, err := ParseJWTExpiration("not-a-jwt"); err == nil {
		t.Errorf("ParseJWTExpiration(not-a-jwt) expected error")
	}
}

func TestTokenDataJSONRoundTrip(t *testing.T) {
	jwt := fakeJWT(t, map[string]any{
		"email":                       "user@example.com",
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct"},
	})
	info, err := ParseChatgptJWTClaims(jwt)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	td := TokenData{
		IDToken:      info,
		AccessToken:  "access",
		RefreshToken: "refresh",
		AccountID:    strp("acct"),
	}
	data, err := json.Marshal(td)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// id_token must serialize as the raw JWT string.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	var idTokenStr string
	if err := json.Unmarshal(raw["id_token"], &idTokenStr); err != nil {
		t.Fatalf("id_token not a string: %v", err)
	}
	if idTokenStr != jwt {
		t.Errorf("id_token = %q, want raw jwt", idTokenStr)
	}

	var back TokenData
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal back: %v", err)
	}
	if back.AccessToken != "access" || back.RefreshToken != "refresh" {
		t.Errorf("round trip tokens mismatch: %+v", back)
	}
	if back.IDToken.RawJWT != jwt {
		t.Errorf("round trip raw jwt mismatch")
	}
}

func TestPlanTypeRawValueIsUnknownPreserved(t *testing.T) {
	jwt := fakeJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "mystery"},
	})
	info, err := ParseChatgptJWTClaims(jwt)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	raw := info.GetChatgptPlanTypeRaw()
	if raw == nil || *raw != "mystery" {
		t.Errorf("raw = %v, want mystery", derefStr(raw))
	}
	if info.ChatgptPlanType == nil || info.ChatgptPlanType.Kind != protocol.AuthPlanTypeUnknown {
		t.Errorf("expected unknown plan kind")
	}
}

func eqStrPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func derefStr(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}
