package cloudreq

import (
	"context"

	"github.com/sqlrush/codexgo/internal/backendclient"
	"github.com/sqlrush/codexgo/internal/login"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// fetchAttemptKind categorizes a fetch attempt failure.
type fetchAttemptKind int

const (
	// fetchRetryable indicates a retryable failure.
	fetchRetryable fetchAttemptKind = iota
	// fetchUnauthorized indicates an HTTP 401.
	fetchUnauthorized
)

// fetchAttemptError describes a failed fetch attempt. It mirrors the Rust
// `FetchAttemptError` (Retryable / Unauthorized variants).
type fetchAttemptError struct {
	kind       fetchAttemptKind
	statusCode *int
	message    string
}

func (e *fetchAttemptError) Error() string {
	if e.message != "" {
		return e.message
	}
	if e.kind == fetchUnauthorized {
		return "unauthorized"
	}
	return "retryable fetch error"
}

// Fetcher fetches the raw requirements contents for an account. It mirrors the
// Rust `RequirementsFetcher` trait. Returning (nil, nil) means "no requirements".
type Fetcher interface {
	// FetchRequirements returns the raw TOML contents, or nil when there are
	// none, or a *fetchAttemptError on failure.
	FetchRequirements(ctx context.Context, auth *login.CodexAuth) (*string, error)
}

// BackendFetcher fetches requirements from the Codex backend. It mirrors the
// Rust `BackendRequirementsFetcher`.
type BackendFetcher struct {
	// BaseURL is the backend base URL.
	BaseURL string
	// UserAgent is the User-Agent header value to send.
	UserAgent string
}

// NewBackendFetcher constructs a BackendFetcher.
func NewBackendFetcher(baseURL, userAgent string) *BackendFetcher {
	return &BackendFetcher{BaseURL: baseURL, UserAgent: userAgent}
}

// FetchRequirements fetches the managed requirements file, mapping HTTP failures
// to retryable/unauthorized fetch errors. It mirrors the Rust
// `BackendRequirementsFetcher::fetch_requirements`.
func (f *BackendFetcher) FetchRequirements(ctx context.Context, auth *login.CodexAuth) (*string, error) {
	client := backendclient.FromAuth(f.BaseURL, auth, f.UserAgent)
	resp, reqErr := client.GetConfigRequirementsFile(ctx)
	if reqErr != nil {
		var status *int
		if code := reqErr.Status(); code != 0 {
			status = &code
		}
		if reqErr.IsUnauthorized() {
			return nil, &fetchAttemptError{
				kind:       fetchUnauthorized,
				statusCode: status,
				message:    reqErr.Error(),
			}
		}
		return nil, &fetchAttemptError{kind: fetchRetryable, statusCode: status}
	}
	if resp.Contents == nil {
		return nil, nil
	}
	return resp.Contents, nil
}

// eligibleAuth reports whether cloud requirements apply to the given auth:
// Business-like or Enterprise plan on the Codex backend. It mirrors the Rust
// `cloud_requirements_eligible_auth`.
func eligibleAuth(auth *login.CodexAuth) bool {
	if auth == nil {
		return false
	}
	plan := auth.AccountPlanType()
	if plan == nil {
		return false
	}
	return auth.UsesCodexBackend() &&
		(plan.IsBusinessLike() || *plan == protocol.PlanTypeEnterprise)
}

// authIdentity returns the (chatgpt_user_id, account_id) pair, mirroring the
// Rust `auth_identity`.
func authIdentity(auth *login.CodexAuth) (*string, *string) {
	return auth.GetChatgptUserID(), auth.GetAccountID()
}
