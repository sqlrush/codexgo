package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/config"
)

// formRecorder captures the last request form seen by the fake token server.
type formRecorder struct{ Form url.Values }

// fakeTokenServer serves the OAuth2 token endpoint; it records the last form.
func fakeTokenServer(t *testing.T, status int, body string) (*httptest.Server, *formRecorder) {
	t.Helper()
	rec := &formRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		rec.Form = r.Form
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func TestRefreshTokensSuccess(t *testing.T) {
	srv, rec := fakeTokenServer(t, http.StatusOK,
		`{"access_token":"new-access","token_type":"Bearer","expires_in":3600,"refresh_token":"new-refresh"}`)

	got, err := RefreshTokens(context.Background(), srv.Client(), srv.URL, "client-1", "old-refresh")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got.AccessToken != "new-access" || got.RefreshToken == nil || *got.RefreshToken != "new-refresh" {
		t.Fatalf("refreshed = %+v", got)
	}
	if rec.Form.Get("grant_type") != "refresh_token" || rec.Form.Get("refresh_token") != "old-refresh" ||
		rec.Form.Get("client_id") != "client-1" {
		t.Fatalf("request form = %v", rec.Form)
	}
}

func TestRefreshTokensPreservesRefreshToken(t *testing.T) {
	// Server omits refresh_token → the prior one is preserved.
	srv, _ := fakeTokenServer(t, http.StatusOK,
		`{"access_token":"a2","token_type":"Bearer","expires_in":3600}`)
	got, err := RefreshTokens(context.Background(), srv.Client(), srv.URL, "c", "keep-me")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got.RefreshToken == nil || *got.RefreshToken != "keep-me" {
		t.Fatalf("refresh token not preserved: %+v", got.RefreshToken)
	}
}

func TestRefreshTokensErrors(t *testing.T) {
	if _, err := RefreshTokens(context.Background(), http.DefaultClient, "http://x", "c", ""); err != ErrNoRefreshToken {
		t.Fatalf("empty refresh token: %v", err)
	}
	srv, _ := fakeTokenServer(t, http.StatusUnauthorized, `{"error":"invalid_grant"}`)
	if _, err := RefreshTokens(context.Background(), srv.Client(), srv.URL, "c", "rt"); err == nil {
		t.Fatal("4xx accepted")
	}
}

func TestRefreshIfNeededSkipsFreshToken(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	future := uint64(now.Add(time.Hour).UnixMilli())
	tokens := StoredOAuthTokens{ExpiresAt: &future}

	got, refreshed, err := RefreshIfNeeded(context.Background(), nil, tokens, config.OAuthCredentialsStoreAuto,
		func(context.Context, string) (string, error) {
			t.Fatal("resolver called for fresh token")
			return "", nil
		},
		now)
	if err != nil || refreshed {
		t.Fatalf("fresh token refreshed: %v %v", refreshed, err)
	}
	_ = got
}

func TestRefreshIfNeededNoRefreshToken(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	past := uint64(now.Add(-time.Hour).UnixMilli())
	tokens := StoredOAuthTokens{ExpiresAt: &past} // expired, no refresh token

	_, refreshed, err := RefreshIfNeeded(context.Background(), nil, tokens, config.OAuthCredentialsStoreAuto,
		func(context.Context, string) (string, error) { return "", nil }, now)
	if refreshed || err != ErrNoRefreshToken {
		t.Fatalf("want ErrNoRefreshToken, got refreshed=%v err=%v", refreshed, err)
	}
}
