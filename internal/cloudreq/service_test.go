package cloudreq

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	appserverproto "github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/login"
	"github.com/sqlrush/codexgo/pkg/config"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// businessAuth builds a managed-ChatGPT auth on a Business plan with identity.
func businessAuth(t *testing.T, plan protocol.KnownPlan, userID, accountID string) *login.CodexAuth {
	t.Helper()
	mode := appserverproto.AuthModeChatgpt
	planType := protocol.KnownAuthPlanType(plan)
	uid := userID
	acct := accountID
	lastRefresh := time.Now().UTC()
	authJSON := &login.AuthDotJson{
		AuthMode: &mode,
		Tokens: &login.TokenData{
			IDToken: login.IdTokenInfo{
				ChatgptPlanType: &planType,
				ChatgptUserID:   &uid,
			},
			AccessToken: "access-token",
			AccountID:   &acct,
		},
		LastRefresh: &lastRefresh,
	}
	auth, err := login.FromAuthDotJson(context.Background(), nil, t.TempDir(), authJSON, config.AuthCredentialsStoreFile, nil)
	if err != nil {
		t.Fatalf("FromAuthDotJson: %v", err)
	}
	return &auth
}

// sequenceFetcher returns queued responses in order.
type sequenceFetcher struct {
	responses []fetchResult
	calls     int
}

type fetchResult struct {
	contents *string
	err      error
}

func (f *sequenceFetcher) FetchRequirements(_ context.Context, _ *login.CodexAuth) (*string, error) {
	idx := f.calls
	f.calls++
	if idx >= len(f.responses) {
		return nil, nil
	}
	r := f.responses[idx]
	return r.contents, r.err
}

func strptr(s string) *string { return &s }

func TestEligibleAuth(t *testing.T) {
	tests := []struct {
		name string
		plan protocol.KnownPlan
		want bool
	}{
		{"business", protocol.KnownPlanBusiness, true},
		{"enterprise", protocol.KnownPlanEnterprise, true},
		{"enterprise_cbp", protocol.KnownPlanEnterpriseCbpUsageBased, true},
		{"pro", protocol.KnownPlanPro, false},
		{"team", protocol.KnownPlanTeam, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := businessAuth(t, tt.plan, "u1", "a1")
			if got := eligibleAuth(auth); got != tt.want {
				t.Errorf("eligibleAuth(%s) = %v, want %v", tt.plan, got, tt.want)
			}
		})
	}
	t.Run("nil_auth", func(t *testing.T) {
		if eligibleAuth(nil) {
			t.Error("eligibleAuth(nil) should be false")
		}
	})
}

func TestFetchSkipsIneligibleAndNil(t *testing.T) {
	home := t.TempDir()
	svc := NewService(StaticAuthProvider{CurrentAuth: nil}, &sequenceFetcher{}, home, Timeout)
	req, err := svc.fetch(context.Background())
	if err != nil || req.Present {
		t.Errorf("nil auth: req=%+v err=%v", req, err)
	}

	proAuth := businessAuth(t, protocol.KnownPlanPro, "u", "a")
	svc2 := NewService(StaticAuthProvider{CurrentAuth: proAuth}, &sequenceFetcher{}, home, Timeout)
	req2, err2 := svc2.fetch(context.Background())
	if err2 != nil || req2.Present {
		t.Errorf("pro auth: req=%+v err=%v", req2, err2)
	}
}

func TestFetchWithRetriesSucceedsAfterRetry(t *testing.T) {
	home := t.TempDir()
	auth := businessAuth(t, protocol.KnownPlanBusiness, "u1", "a1")
	fetcher := &sequenceFetcher{responses: []fetchResult{
		{err: &fetchAttemptError{kind: fetchRetryable}},
		{contents: strptr("allowed_approval_policies = [\"never\"]")},
	}}
	svc := NewService(StaticAuthProvider{CurrentAuth: auth}, fetcher, home, Timeout)
	req, lerr := svc.FetchWithTimeout(context.Background())
	if lerr != nil {
		t.Fatalf("FetchWithTimeout: %v", lerr)
	}
	if !req.Present {
		t.Errorf("requirements not present: %+v", req)
	}
	if fetcher.calls != 2 {
		t.Errorf("calls = %d, want 2", fetcher.calls)
	}
	// A cache file should have been written.
	if _, err := os.Stat(filepath.Join(home, CacheFilename)); err != nil {
		t.Errorf("cache file not written: %v", err)
	}
}

func TestFetchWithRetriesExhausts(t *testing.T) {
	home := t.TempDir()
	auth := businessAuth(t, protocol.KnownPlanEnterprise, "u1", "a1")
	code := 500
	resp := []fetchResult{}
	for i := 0; i < MaxAttempts; i++ {
		resp = append(resp, fetchResult{err: &fetchAttemptError{kind: fetchRetryable, statusCode: &code}})
	}
	fetcher := &sequenceFetcher{responses: resp}
	svc := NewService(StaticAuthProvider{CurrentAuth: auth}, fetcher, home, Timeout)
	_, lerr := svc.fetch(context.Background())
	if lerr == nil {
		t.Fatal("expected fail-closed error")
	}
	if lerr.Code != LoadErrorRequestFailed {
		t.Errorf("code = %v, want RequestFailed", lerr.Code)
	}
	if lerr.Message != loadFailedMessage {
		t.Errorf("message = %q", lerr.Message)
	}
	if fetcher.calls != MaxAttempts {
		t.Errorf("calls = %d, want %d", fetcher.calls, MaxAttempts)
	}
}

func TestFetchUnauthorizedFailsClosed(t *testing.T) {
	home := t.TempDir()
	auth := businessAuth(t, protocol.KnownPlanBusiness, "u1", "a1")
	code := 401
	fetcher := &sequenceFetcher{responses: []fetchResult{
		{err: &fetchAttemptError{kind: fetchUnauthorized, statusCode: &code, message: "401"}},
	}}
	svc := NewService(StaticAuthProvider{CurrentAuth: auth}, fetcher, home, Timeout)
	_, lerr := svc.fetch(context.Background())
	if lerr == nil || lerr.Code != LoadErrorAuth {
		t.Fatalf("expected auth error, got %v", lerr)
	}
	if lerr.Message != authRecoveryFailedMessage {
		t.Errorf("message = %q", lerr.Message)
	}
}

func TestFetchWithTimeoutFailsClosed(t *testing.T) {
	home := t.TempDir()
	auth := businessAuth(t, protocol.KnownPlanBusiness, "u1", "a1")
	// A fetcher that blocks until context cancellation.
	blocking := blockingFetcher{}
	svc := NewService(StaticAuthProvider{CurrentAuth: auth}, blocking, home, 50*time.Millisecond)
	_, lerr := svc.FetchWithTimeout(context.Background())
	if lerr == nil || lerr.Code != LoadErrorTimeout {
		t.Fatalf("expected timeout error, got %v", lerr)
	}
	if lerr.Message != "timed out waiting for cloud requirements after 0s" {
		t.Errorf("message = %q", lerr.Message)
	}
}

type blockingFetcher struct{}

func (blockingFetcher) FetchRequirements(ctx context.Context, _ *login.CodexAuth) (*string, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestParseRequirements(t *testing.T) {
	tests := []struct {
		name        string
		contents    *string
		wantPresent bool
		wantErr     bool
	}{
		{"nil", nil, false, false},
		{"empty", strptr("   "), false, false},
		{"comment_only", strptr("# just a comment"), false, false},
		{"invalid_toml", strptr("not = ["), false, true},
		{"valid", strptr("allowed_approval_policies = [\"never\"]"), true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := parseRequirements(tt.contents)
			if tt.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if req.Present != tt.wantPresent {
				t.Errorf("present = %v, want %v", req.Present, tt.wantPresent)
			}
		})
	}
}

func TestParseErrorMessage(t *testing.T) {
	home := t.TempDir()
	auth := businessAuth(t, protocol.KnownPlanBusiness, "u1", "a1")
	fetcher := &sequenceFetcher{responses: []fetchResult{
		{contents: strptr("not = [")},
	}}
	svc := NewService(StaticAuthProvider{CurrentAuth: auth}, fetcher, home, Timeout)
	_, lerr := svc.fetch(context.Background())
	if lerr == nil || lerr.Code != LoadErrorParse {
		t.Fatalf("expected parse error, got %v", lerr)
	}
	if got := lerr.Message; len(got) < len(parseFailedMessage) || got[:len(parseFailedMessage)] != parseFailedMessage {
		t.Errorf("message = %q", got)
	}
}
