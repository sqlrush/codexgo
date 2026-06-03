package cloudreq

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sqlrush/codexgo/internal/login"
	"github.com/sqlrush/codexgo/internal/protocol"
)

func TestBackendFetcherMapsStatuses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		status     int
		body       string
		wantKind   fetchAttemptKind
		wantNil    bool
		wantString *string
	}{
		{name: "ok_with_contents", status: 200, body: `{"contents":"x = 1"}`, wantString: strptr("x = 1")},
		{name: "ok_no_contents", status: 200, body: `{}`, wantNil: true},
		{name: "unauthorized", status: 401, body: "no", wantKind: fetchUnauthorized},
		{name: "server_error", status: 500, body: "boom", wantKind: fetchRetryable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.status == 200 {
					w.Header().Set("Content-Type", "application/json")
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			fetcher := NewBackendFetcher(srv.URL, "codex-go/1.0")
			auth := businessAuth(t, protocol.KnownPlanBusiness, "u", "a")
			contents, err := fetcher.FetchRequirements(context.Background(), auth)

			if tt.status == 200 {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if tt.wantNil {
					if contents != nil {
						t.Errorf("contents = %v, want nil", contents)
					}
					return
				}
				if contents == nil || *contents != *tt.wantString {
					t.Errorf("contents = %v, want %v", contents, tt.wantString)
				}
				return
			}

			var fae *fetchAttemptError
			if !errors.As(err, &fae) {
				t.Fatalf("error type = %T (%v)", err, err)
			}
			if fae.kind != tt.wantKind {
				t.Errorf("kind = %v, want %v", fae.kind, tt.wantKind)
			}
			if fae.statusCode == nil || *fae.statusCode != tt.status {
				t.Errorf("statusCode = %v, want %d", fae.statusCode, tt.status)
			}
		})
	}
}

func TestAuthIdentity(t *testing.T) {
	t.Parallel()
	auth := businessAuth(t, protocol.KnownPlanBusiness, "user-x", "acct-y")
	uid, acct := authIdentity(auth)
	if uid == nil || *uid != "user-x" {
		t.Errorf("chatgpt_user_id = %v", uid)
	}
	if acct == nil || *acct != "acct-y" {
		t.Errorf("account_id = %v", acct)
	}
}

func TestStaticAuthProvider(t *testing.T) {
	t.Parallel()
	auth := login.FromAPIKey("sk")
	p := StaticAuthProvider{CurrentAuth: &auth}
	if p.Auth(context.Background()) != &auth {
		t.Error("Auth should return the static auth")
	}
	refreshed, permanent, err := p.RecoverUnauthorized(context.Background())
	if refreshed != nil || permanent || err != nil {
		t.Errorf("RecoverUnauthorized = (%v,%v,%v)", refreshed, permanent, err)
	}
}
