package cloudreq

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/sqlrush/codexgo/internal/login"
)

// AuthProvider supplies the current auth and optional unauthorized recovery. It
// replaces the Rust `AuthManager` with the minimal surface this port needs.
type AuthProvider interface {
	// Auth returns the current auth, or nil when unauthenticated.
	Auth(ctx context.Context) *login.CodexAuth
	// RecoverUnauthorized attempts to refresh auth after a 401. It returns the
	// refreshed auth on success. permanent indicates the failure is fatal
	// (vs. a transient failure that should be retried). When no recovery is
	// possible it returns (nil, false, nil).
	RecoverUnauthorized(ctx context.Context) (refreshed *login.CodexAuth, permanent bool, err error)
}

// StaticAuthProvider is an AuthProvider with a fixed auth and no recovery. It is
// useful for callers (and tests) that already hold a resolved auth.
type StaticAuthProvider struct {
	// CurrentAuth is the auth returned by Auth.
	CurrentAuth *login.CodexAuth
}

// Auth returns the static auth.
func (p StaticAuthProvider) Auth(context.Context) *login.CodexAuth { return p.CurrentAuth }

// RecoverUnauthorized reports that no recovery is available.
func (StaticAuthProvider) RecoverUnauthorized(context.Context) (*login.CodexAuth, bool, error) {
	return nil, false, nil
}

// Service loads and caches cloud requirements. It mirrors the Rust
// `CloudRequirementsService`.
type Service struct {
	authProvider        AuthProvider
	fetcher             Fetcher
	requirementsBaseDir string
	cachePath           string
	timeout             time.Duration
	now                 func() time.Time
}

// NewService constructs a Service. It mirrors the Rust
// `CloudRequirementsService::new`.
func NewService(authProvider AuthProvider, fetcher Fetcher, codexHome string, timeout time.Duration) *Service {
	return &Service{
		authProvider:        authProvider,
		fetcher:             fetcher,
		requirementsBaseDir: codexHome,
		cachePath:           filepath.Join(codexHome, CacheFilename),
		timeout:             timeout,
		now:                 func() time.Time { return time.Now().UTC() },
	}
}

// Requirements is the loaded requirements result. Because the full requirements
// schema is not modeled in this port, the raw TOML contents are exposed
// alongside the parsed validity. Present is false when there are no requirements.
type Requirements struct {
	// Present is true when non-empty requirements were loaded.
	Present bool
	// Contents is the raw requirements TOML (empty when not present).
	Contents string
}

// FetchWithTimeout fetches requirements with the configured timeout, failing
// closed on timeout. It mirrors the Rust `fetch_with_timeout`.
func (s *Service) FetchWithTimeout(ctx context.Context) (Requirements, *LoadError) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	type result struct {
		req Requirements
		err *LoadError
	}
	ch := make(chan result, 1)
	go func() {
		req, err := s.fetch(ctx)
		ch <- result{req: req, err: err}
	}()

	select {
	case <-ctx.Done():
		return Requirements{}, newLoadError(
			LoadErrorTimeout,
			nil,
			timeoutMessage(s.timeout),
		)
	case r := <-ch:
		return r.req, r.err
	}
}

func timeoutMessage(timeout time.Duration) string {
	secs := int64(timeout / time.Second)
	return "timed out waiting for cloud requirements after " + strconv.FormatInt(secs, 10) + "s"
}

// fetch loads from cache when valid, else fetches with retries. It mirrors the
// Rust `fetch`.
func (s *Service) fetch(ctx context.Context) (Requirements, *LoadError) {
	auth := s.authProvider.Auth(ctx)
	if auth == nil {
		return Requirements{}, nil
	}
	if !eligibleAuth(auth) {
		return Requirements{}, nil
	}
	chatgptUserID, accountID := authIdentity(auth)

	if payload, err := s.loadCache(chatgptUserID, accountID); err == nil {
		return requirementsFromContents(payload.Contents), nil
	}

	return s.fetchWithRetries(ctx, auth)
}

// fetchWithRetries performs up to MaxAttempts fetches with backoff, handling
// unauthorized recovery and failing closed on exhaustion. It mirrors the Rust
// `fetch_with_retries`.
func (s *Service) fetchWithRetries(ctx context.Context, auth *login.CodexAuth) (Requirements, *LoadError) {
	attempt := 1
	var lastStatusCode *int

	for attempt <= MaxAttempts {
		contents, err := s.fetcher.FetchRequirements(ctx, auth)
		if err != nil {
			var fae *fetchAttemptError
			if !errors.As(err, &fae) {
				// Treat unexpected errors as retryable.
				fae = &fetchAttemptError{kind: fetchRetryable}
			}
			switch fae.kind {
			case fetchRetryable:
				lastStatusCode = fae.statusCode
				if attempt < MaxAttempts {
					if !sleepCtx(ctx, backoff(uint64(attempt))) {
						return Requirements{}, requestExhaustedError(lastStatusCode)
					}
				}
				attempt++
				continue
			case fetchUnauthorized:
				lastStatusCode = fae.statusCode
				refreshed, permanent, recErr := s.authProvider.RecoverUnauthorized(ctx)
				if recErr == nil && refreshed != nil {
					auth = refreshed
					continue
				}
				if recErr == nil && refreshed == nil && !permanent {
					// No recovery available at all -> fail closed (auth).
					return Requirements{}, newLoadError(LoadErrorAuth, fae.statusCode, authRecoveryFailedMessage)
				}
				if permanent {
					msg := authRecoveryFailedMessage
					if recErr != nil {
						msg = recErr.Error()
					}
					return Requirements{}, newLoadError(LoadErrorAuth, fae.statusCode, msg)
				}
				// Transient recovery failure: retry with backoff.
				if attempt < MaxAttempts {
					if !sleepCtx(ctx, backoff(uint64(attempt))) {
						return Requirements{}, requestExhaustedError(lastStatusCode)
					}
				}
				attempt++
				continue
			}
		}

		// Success: parse + cache.
		req, parseErr := parseRequirements(contents)
		if parseErr != nil {
			return Requirements{}, newLoadError(LoadErrorParse, nil, formatParseFailedMessage(parseErr))
		}

		chatgptUserID, accountID := authIdentity(auth)
		_ = s.saveCache(chatgptUserID, accountID, contents)
		return req, nil
	}

	return Requirements{}, requestExhaustedError(lastStatusCode)
}

func requestExhaustedError(statusCode *int) *LoadError {
	return newLoadError(LoadErrorRequestFailed, statusCode, loadFailedMessage)
}

// parseRequirements validates the TOML and reports presence. It mirrors the Rust
// `parse_cloud_requirements`: empty/comment-only contents yield "not present",
// invalid TOML yields an error.
func parseRequirements(contents *string) (Requirements, error) {
	if contents == nil {
		return Requirements{}, nil
	}
	if isEmptyRequirements(*contents) {
		return Requirements{}, nil
	}
	var probe map[string]any
	if err := toml.Unmarshal([]byte(*contents), &probe); err != nil {
		return Requirements{}, err
	}
	if len(probe) == 0 {
		return Requirements{}, nil
	}
	return Requirements{Present: true, Contents: *contents}, nil
}

func requirementsFromContents(contents *string) Requirements {
	req, err := parseRequirements(contents)
	if err != nil {
		return Requirements{}
	}
	return req
}

// loadCache reads and validates the cache, mirroring the Rust `load_cache`.
func (s *Service) loadCache(chatgptUserID, accountID *string) (cacheSignedPayload, *cacheLoadError) {
	if chatgptUserID == nil || accountID == nil {
		return cacheSignedPayload{}, &cacheLoadError{status: cacheAuthIdentityIncomplete}
	}

	bytesData, err := os.ReadFile(s.cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return cacheSignedPayload{}, &cacheLoadError{status: cacheFileNotFound}
		}
		return cacheSignedPayload{}, &cacheLoadError{status: cacheReadFailed, detail: err.Error()}
	}

	var file cacheFile
	if err := unmarshalStrictish(bytesData, &file); err != nil {
		return cacheSignedPayload{}, &cacheLoadError{status: cacheParseFailed, detail: err.Error()}
	}
	payloadBytes, err := cachePayloadBytes(file.SignedPayload)
	if err != nil {
		return cacheSignedPayload{}, &cacheLoadError{status: cacheParseFailed, detail: "failed to serialize cache payload"}
	}
	if !verifyCacheSignature(payloadBytes, file.Signature) {
		return cacheSignedPayload{}, &cacheLoadError{status: cacheSignatureInvalid}
	}

	if file.SignedPayload.ChatgptUserID == nil || file.SignedPayload.AccountID == nil {
		return cacheSignedPayload{}, &cacheLoadError{status: cacheIdentityIncomplete}
	}
	if *file.SignedPayload.ChatgptUserID != *chatgptUserID || *file.SignedPayload.AccountID != *accountID {
		return cacheSignedPayload{}, &cacheLoadError{status: cacheIdentityMismatch}
	}
	if !file.SignedPayload.ExpiresAt.After(s.now()) {
		return cacheSignedPayload{}, &cacheLoadError{status: cacheExpired}
	}

	return file.SignedPayload, nil
}

// saveCache writes a signed cache file, mirroring the Rust `save_cache`.
func (s *Service) saveCache(chatgptUserID, accountID, contents *string) error {
	now := s.now()
	expiresAt := now.Add(CacheTTL)
	signedPayload := cacheSignedPayload{
		CachedAt:      now,
		ExpiresAt:     expiresAt,
		ChatgptUserID: chatgptUserID,
		AccountID:     accountID,
		Contents:      contents,
	}
	payloadBytes, err := cachePayloadBytes(signedPayload)
	if err != nil {
		return err
	}
	serialized, err := marshalPretty(cacheFile{
		SignedPayload: signedPayload,
		Signature:     signCachePayload(payloadBytes),
	})
	if err != nil {
		return err
	}
	if parent := filepath.Dir(s.cachePath); parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(s.cachePath, serialized, 0o644)
}

// CachePath returns the cache file path (used by tests).
func (s *Service) CachePath() string { return s.cachePath }

// SetNow overrides the time source (used by tests).
func (s *Service) SetNow(now func() time.Time) { s.now = now }

// backoff mirrors the Rust `codex_core::util::backoff` with the same constants
// (200ms initial delay, factor 2, 0.9-1.1 jitter).
func backoff(attempt uint64) time.Duration {
	const initialDelayMs = 200.0
	const factor = 2.0
	exp := math.Pow(factor, float64(saturatingSub(attempt, 1)))
	base := initialDelayMs * exp
	jitter := 0.9 + rand.Float64()*0.2
	return time.Duration(base*jitter) * time.Millisecond
}

func saturatingSub(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}

// sleepCtx sleeps for d unless ctx is cancelled first. It returns false if the
// context was cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
