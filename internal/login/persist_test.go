package login

import (
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

func TestPersistTokensUpdatesFields(t *testing.T) {
	fixedNow := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	restore := now
	now = func() time.Time { return fixedNow }
	t.Cleanup(func() { now = restore })

	home := t.TempDir()
	storage := newEphemeralAuthStorage(home)
	mode := appserverproto.AuthModeChatgpt
	idJWT := fakeJWT(t, map[string]any{"email": "old@example.com"})
	info, err := ParseChatgptJWTClaims(idJWT)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	seed := &AuthDotJson{
		AuthMode: &mode,
		Tokens:   &TokenData{IDToken: info, AccessToken: "old-acc", RefreshToken: "old-ref"},
	}
	if err := storage.save(seed); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	newID := fakeJWT(t, map[string]any{"email": "new@example.com"})
	newAccess := "new-acc"
	newRefresh := "new-ref"
	updated, err := persistTokens(storage, &newID, &newAccess, &newRefresh)
	if err != nil {
		t.Fatalf("persistTokens: %v", err)
	}
	if updated.Tokens.AccessToken != "new-acc" || updated.Tokens.RefreshToken != "new-ref" {
		t.Errorf("tokens not updated: %+v", updated.Tokens)
	}
	if updated.Tokens.IDToken.Email == nil || *updated.Tokens.IDToken.Email != "new@example.com" {
		t.Errorf("id token not updated: %v", updated.Tokens.IDToken.Email)
	}
	if updated.LastRefresh == nil || !updated.LastRefresh.Equal(fixedNow) {
		t.Errorf("last_refresh = %v, want %v", updated.LastRefresh, fixedNow)
	}
}

func TestPersistTokensKeepsUnchangedFields(t *testing.T) {
	restore := now
	now = func() time.Time { return time.Unix(1700000000, 0).UTC() }
	t.Cleanup(func() { now = restore })

	home := t.TempDir()
	storage := newEphemeralAuthStorage(home)
	mode := appserverproto.AuthModeChatgpt
	idJWT := fakeJWT(t, map[string]any{"email": "keep@example.com"})
	info, err := ParseChatgptJWTClaims(idJWT)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := storage.save(&AuthDotJson{
		AuthMode: &mode,
		Tokens:   &TokenData{IDToken: info, AccessToken: "keep-acc", RefreshToken: "keep-ref"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Only update the access token; refresh + id token must persist.
	newAccess := "rotated-acc"
	updated, err := persistTokens(storage, nil, &newAccess, nil)
	if err != nil {
		t.Fatalf("persistTokens: %v", err)
	}
	if updated.Tokens.AccessToken != "rotated-acc" {
		t.Errorf("access token = %q", updated.Tokens.AccessToken)
	}
	if updated.Tokens.RefreshToken != "keep-ref" {
		t.Errorf("refresh token changed: %q", updated.Tokens.RefreshToken)
	}
	if updated.Tokens.IDToken.Email == nil || *updated.Tokens.IDToken.Email != "keep@example.com" {
		t.Errorf("id token changed: %v", updated.Tokens.IDToken.Email)
	}
}

func TestPersistTokensMissingAuthErrors(t *testing.T) {
	storage := newEphemeralAuthStorage(t.TempDir())
	if _, err := persistTokens(storage, nil, nil, nil); err == nil {
		t.Errorf("expected ErrTokenDataUnavailable for empty storage")
	}
}
