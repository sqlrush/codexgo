package mcp

import (
	"encoding/json"
	"testing"
	"time"
)

func TestOAuthTokenResponseRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string // canonical re-encoding
	}{
		{
			name: "minimal",
			in:   `{"access_token":"a","token_type":"bearer"}`,
			want: `{"access_token":"a","token_type":"bearer"}`,
		},
		{
			name: "full",
			in:   `{"access_token":"a","token_type":"bearer","expires_in":3600,"refresh_token":"r","scope":"read write"}`,
			want: `{"access_token":"a","expires_in":3600,"refresh_token":"r","scope":"read write","token_type":"bearer"}`,
		},
		{
			name: "preserves extra vendor fields",
			in:   `{"access_token":"a","token_type":"bearer","vendor_x":42}`,
			want: `{"access_token":"a","token_type":"bearer","vendor_x":42}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var tok OAuthTokenResponse
			if err := json.Unmarshal([]byte(tc.in), &tok); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			out, err := json.Marshal(tok)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(out) != tc.want {
				t.Fatalf("got %s want %s", out, tc.want)
			}
		})
	}
}

func TestOAuthTokenResponseScopeSplit(t *testing.T) {
	t.Parallel()
	var tok OAuthTokenResponse
	if err := json.Unmarshal([]byte(`{"access_token":"a","token_type":"bearer","scope":"  read   write  "}`), &tok); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(tok.Scopes) != 2 || tok.Scopes[0] != "read" || tok.Scopes[1] != "write" {
		t.Fatalf("scopes=%v", tok.Scopes)
	}
}

func TestComputeExpiresAtMillis(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_000_000)
	t.Run("nil expires_in", func(t *testing.T) {
		t.Parallel()
		tok := OAuthTokenResponse{AccessToken: "a", TokenType: "bearer"}
		if got := tok.ComputeExpiresAtMillis(now); got != nil {
			t.Fatalf("want nil, got %v", *got)
		}
	})
	t.Run("with expires_in", func(t *testing.T) {
		t.Parallel()
		in := uint64(60)
		tok := OAuthTokenResponse{AccessToken: "a", TokenType: "bearer", ExpiresIn: &in}
		got := tok.ComputeExpiresAtMillis(now)
		if got == nil {
			t.Fatal("want non-nil")
		}
		want := uint64(1_000_000 + 60_000)
		if *got != want {
			t.Fatalf("got %d want %d", *got, want)
		}
	})
}

func TestTokenNeedsRefresh(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_000_000)
	tests := []struct {
		name      string
		expiresAt *uint64
		want      bool
	}{
		{name: "nil never refreshes", expiresAt: nil, want: false},
		{name: "far future no refresh", expiresAt: u64(1_000_000 + 100_000), want: false},
		{name: "within skew window", expiresAt: u64(1_000_000 + 10_000), want: true},
		{name: "already expired", expiresAt: u64(900_000), want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := TokenNeedsRefresh(tc.expiresAt, now); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestRefreshExpiresInFromTimestamp(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_000_000)
	t.Run("future expiry recomputes seconds", func(t *testing.T) {
		t.Parallel()
		tok := StoredOAuthTokens{ExpiresAt: u64(1_000_000 + 30_000)}
		refreshExpiresInFromTimestamp(&tok, now)
		if tok.TokenResponse.ExpiresIn == nil || *tok.TokenResponse.ExpiresIn != 30 {
			t.Fatalf("expires_in=%v", tok.TokenResponse.ExpiresIn)
		}
	})
	t.Run("past expiry clears", func(t *testing.T) {
		t.Parallel()
		seven := uint64(7)
		tok := StoredOAuthTokens{
			ExpiresAt:     u64(900_000),
			TokenResponse: OAuthTokenResponse{ExpiresIn: &seven},
		}
		refreshExpiresInFromTimestamp(&tok, now)
		if tok.TokenResponse.ExpiresIn != nil {
			t.Fatalf("want nil expires_in, got %d", *tok.TokenResponse.ExpiresIn)
		}
	})
}

func u64(v uint64) *uint64 { return &v }
