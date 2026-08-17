package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sqlrush/codexgo/pkg/client"
)

// AuthErrorKind discriminates the variants of AuthError, mirroring the Rust
// `AuthError` enum.
type AuthErrorKind int

const (
	// AuthErrorBuild is a request auth-build error (non-transient).
	AuthErrorBuild AuthErrorKind = iota
	// AuthErrorTransient is a transient auth error.
	AuthErrorTransient
)

// AuthError is returned while applying authentication to an outbound request. It
// mirrors the Rust `AuthError`.
type AuthError struct {
	Kind    AuthErrorKind
	Message string
}

// Error implements the error interface, matching the Rust Display formatting.
func (e *AuthError) Error() string {
	switch e.Kind {
	case AuthErrorBuild:
		return fmt.Sprintf("request auth build error: %s", e.Message)
	case AuthErrorTransient:
		return fmt.Sprintf("transient auth error: %s", e.Message)
	default:
		return "auth error"
	}
}

// ToTransportError converts an AuthError into a client.TransportError, mirroring
// the Rust `From<AuthError> for TransportError`.
func (e *AuthError) ToTransportError() *client.TransportError {
	switch e.Kind {
	case AuthErrorBuild:
		return client.NewBuildError(e.Message)
	default:
		return client.NewNetworkError(e.Message)
	}
}

// AuthProvider applies authentication to API requests. It mirrors the Rust
// `AuthProvider` trait.
//
// Header-only providers implement AddAuthHeaders; providers that sign complete
// requests (for example AWS SigV4) implement ApplyAuth to inspect the final URL,
// headers, and body bytes before the transport sends the request.
type AuthProvider interface {
	// AddAuthHeaders adds any auth headers that are available without request
	// body access. Implementations must not block.
	AddAuthHeaders(headers http.Header)

	// ApplyAuth applies auth to a complete outbound request and returns the
	// request to send. Implementations may return a modified copy. On error the
	// request must not be sent.
	ApplyAuth(ctx context.Context, req client.Request) (client.Request, *AuthError)
}

// ToAuthHeaders returns the auth headers an AuthProvider would add, mirroring the
// Rust default `to_auth_headers`.
func ToAuthHeaders(auth AuthProvider) http.Header {
	headers := http.Header{}
	auth.AddAuthHeaders(headers)
	return headers
}

// defaultApplyAuth implements the Rust default `apply_auth`: it clones the
// request, adds the provider's auth headers, and returns the copy. The caller's
// request is never mutated.
func defaultApplyAuth(auth AuthProvider, req client.Request) client.Request {
	out := req.WithCompression(req.Compression) // returns a header-cloned copy
	auth.AddAuthHeaders(out.Headers)
	return out
}

// NoOpAuth is an AuthProvider that adds no authentication. It mirrors a header
// provider that contributes nothing.
type NoOpAuth struct{}

// AddAuthHeaders adds nothing.
func (NoOpAuth) AddAuthHeaders(http.Header) {}

// ApplyAuth returns the request unchanged.
func (a NoOpAuth) ApplyAuth(_ context.Context, req client.Request) (client.Request, *AuthError) {
	return defaultApplyAuth(a, req), nil
}

// BearerAuth is an AuthProvider that attaches a static bearer token via the
// Authorization header.
type BearerAuth struct {
	Token string
}

// NewBearerAuth builds a BearerAuth with the given token.
func NewBearerAuth(token string) BearerAuth {
	return BearerAuth{Token: token}
}

// AddAuthHeaders sets the Authorization header to "Bearer <token>".
func (a BearerAuth) AddAuthHeaders(headers http.Header) {
	if a.Token == "" {
		return
	}
	headers.Set("Authorization", "Bearer "+a.Token)
}

// ApplyAuth returns a copy of the request with the bearer header applied.
func (a BearerAuth) ApplyAuth(_ context.Context, req client.Request) (client.Request, *AuthError) {
	return defaultApplyAuth(a, req), nil
}

// AuthHeaderTelemetry reports whether an Authorization header was attached. It
// mirrors the Rust `AuthHeaderTelemetry`.
type AuthHeaderTelemetry struct {
	Attached bool
	Name     string
}

// AuthHeaderTelemetryFor inspects an AuthProvider's headers, mirroring the Rust
// `auth_header_telemetry`.
func AuthHeaderTelemetryFor(auth AuthProvider) AuthHeaderTelemetry {
	headers := http.Header{}
	auth.AddAuthHeaders(headers)
	if headers.Get("Authorization") != "" {
		return AuthHeaderTelemetry{Attached: true, Name: "authorization"}
	}
	return AuthHeaderTelemetry{Attached: false}
}

// compile-time assertions that the built-in providers satisfy AuthProvider.
var (
	_ AuthProvider = NoOpAuth{}
	_ AuthProvider = BearerAuth{}
)
