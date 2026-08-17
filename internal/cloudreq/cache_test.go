package cloudreq

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func TestSignAndVerifyCacheRoundtrip(t *testing.T) {
	t.Parallel()
	payload := cacheSignedPayload{
		CachedAt:      time.Unix(1700000000, 0).UTC(),
		ExpiresAt:     time.Unix(1700001800, 0).UTC(),
		ChatgptUserID: strptr("user-1"),
		AccountID:     strptr("acct-1"),
		Contents:      strptr("allowed_approval_policies = [\"never\"]"),
	}
	bytesData, err := cachePayloadBytes(payload)
	if err != nil {
		t.Fatalf("cachePayloadBytes: %v", err)
	}
	sig := signCachePayload(bytesData)
	if sig == "" {
		t.Fatal("empty signature")
	}
	if !verifyCacheSignature(bytesData, sig) {
		t.Error("signature should verify")
	}
	if verifyCacheSignature(bytesData, "AAAA") {
		t.Error("garbage signature should not verify")
	}
	// Tampering with payload invalidates the signature.
	tampered := append([]byte(nil), bytesData...)
	tampered[0] ^= 0xFF
	if verifyCacheSignature(tampered, sig) {
		t.Error("tampered payload should not verify")
	}
}

func TestSaveAndLoadCacheRoundtrip(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	svc := NewService(StaticAuthProvider{}, &sequenceFetcher{}, home, Timeout)
	contents := "allowed_approval_policies = [\"never\"]"
	if err := svc.saveCache(strptr("user-1"), strptr("acct-1"), &contents); err != nil {
		t.Fatalf("saveCache: %v", err)
	}

	// Verify on-disk shape has signed_payload + signature.
	raw, err := os.ReadFile(filepath.Join(home, CacheFilename))
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	var file map[string]json.RawMessage
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("unmarshal cache: %v", err)
	}
	if _, ok := file["signed_payload"]; !ok {
		t.Error("missing signed_payload")
	}
	if _, ok := file["signature"]; !ok {
		t.Error("missing signature")
	}

	payload, lerr := svc.loadCache(strptr("user-1"), strptr("acct-1"))
	if lerr != nil {
		t.Fatalf("loadCache: %v", lerr)
	}
	if payload.Contents == nil || *payload.Contents != contents {
		t.Errorf("loaded contents = %v", payload.Contents)
	}
}

func TestLoadCacheStatuses(t *testing.T) {
	t.Parallel()

	t.Run("identity_incomplete", func(t *testing.T) {
		t.Parallel()
		svc := NewService(StaticAuthProvider{}, &sequenceFetcher{}, t.TempDir(), Timeout)
		_, lerr := svc.loadCache(nil, strptr("a"))
		if lerr == nil || lerr.status != cacheAuthIdentityIncomplete {
			t.Fatalf("status = %v", lerr)
		}
	})

	t.Run("file_not_found", func(t *testing.T) {
		t.Parallel()
		svc := NewService(StaticAuthProvider{}, &sequenceFetcher{}, t.TempDir(), Timeout)
		_, lerr := svc.loadCache(strptr("u"), strptr("a"))
		if lerr == nil || lerr.status != cacheFileNotFound {
			t.Fatalf("status = %v", lerr)
		}
	})

	t.Run("signature_invalid", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		bad := cacheFile{
			SignedPayload: cacheSignedPayload{
				CachedAt:      time.Now().UTC(),
				ExpiresAt:     time.Now().UTC().Add(time.Hour),
				ChatgptUserID: strptr("u"),
				AccountID:     strptr("a"),
			},
			Signature: "AAAA",
		}
		data, _ := json.MarshalIndent(bad, "", "  ")
		if err := os.WriteFile(filepath.Join(home, CacheFilename), data, 0o644); err != nil {
			t.Fatal(err)
		}
		svc := NewService(StaticAuthProvider{}, &sequenceFetcher{}, home, Timeout)
		_, lerr := svc.loadCache(strptr("u"), strptr("a"))
		if lerr == nil || lerr.status != cacheSignatureInvalid {
			t.Fatalf("status = %v", lerr)
		}
	})

	t.Run("identity_mismatch", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		svc := NewService(StaticAuthProvider{}, &sequenceFetcher{}, home, Timeout)
		contents := "allowed_approval_policies = [\"never\"]"
		if err := svc.saveCache(strptr("user-1"), strptr("acct-1"), &contents); err != nil {
			t.Fatal(err)
		}
		_, lerr := svc.loadCache(strptr("other-user"), strptr("acct-1"))
		if lerr == nil || lerr.status != cacheIdentityMismatch {
			t.Fatalf("status = %v", lerr)
		}
	})

	t.Run("expired", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		svc := NewService(StaticAuthProvider{}, &sequenceFetcher{}, home, Timeout)
		// Save with a now in the past so expires_at is also in the past.
		past := time.Now().UTC().Add(-2 * CacheTTL)
		svc.SetNow(func() time.Time { return past })
		contents := "allowed_approval_policies = [\"never\"]"
		if err := svc.saveCache(strptr("u"), strptr("a"), &contents); err != nil {
			t.Fatal(err)
		}
		svc.SetNow(func() time.Time { return time.Now().UTC() })
		_, lerr := svc.loadCache(strptr("u"), strptr("a"))
		if lerr == nil || lerr.status != cacheExpired {
			t.Fatalf("status = %v", lerr)
		}
	})
}

func TestFetchUsesValidCache(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	auth := businessAuth(t, protocol.KnownPlanBusiness, "u1", "a1")
	// Pre-populate a valid cache for this identity.
	pre := NewService(StaticAuthProvider{CurrentAuth: auth}, &sequenceFetcher{}, home, Timeout)
	contents := "allowed_approval_policies = [\"never\"]"
	if err := pre.saveCache(strptr("u1"), strptr("a1"), &contents); err != nil {
		t.Fatal(err)
	}
	// A fetcher that would fail if called.
	fetcher := &sequenceFetcher{responses: []fetchResult{
		{err: &fetchAttemptError{kind: fetchRetryable}},
	}}
	svc := NewService(StaticAuthProvider{CurrentAuth: auth}, fetcher, home, Timeout)
	req, lerr := svc.fetch(context.Background())
	if lerr != nil {
		t.Fatalf("fetch: %v", lerr)
	}
	if !req.Present {
		t.Errorf("requirements should be present from cache: %+v", req)
	}
	if fetcher.calls != 0 {
		t.Errorf("fetcher should not be called when cache is valid, calls = %d", fetcher.calls)
	}
}
