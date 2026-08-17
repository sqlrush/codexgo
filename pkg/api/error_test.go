package api

import (
	"errors"
	"testing"

	"github.com/sqlrush/codexgo/pkg/client"
)

func TestAPIErrorMessages(t *testing.T) {
	tests := []struct {
		err  *APIError
		want string
	}{
		{&APIError{Kind: APIErrorAPI, Status: 500, Message: "boom"}, "api error 500: boom"},
		{&APIError{Kind: APIErrorStream, Message: "x"}, "stream error: x"},
		{&APIError{Kind: APIErrorContextWindowExceeded}, "context window exceeded"},
		{&APIError{Kind: APIErrorQuotaExceeded}, "quota exceeded"},
		{&APIError{Kind: APIErrorUsageNotIncluded}, "usage not included"},
		{&APIError{Kind: APIErrorRetryable, Message: "later"}, "retryable error: later"},
		{&APIError{Kind: APIErrorRateLimit, Message: "rl"}, "rate limit: rl"},
		{&APIError{Kind: APIErrorInvalidRequest, Message: "bad"}, "invalid request: bad"},
		{&APIError{Kind: APIErrorCyberPolicy, Message: "cyber"}, "cyber policy: cyber"},
		{&APIError{Kind: APIErrorServerOverloaded}, "server overloaded"},
	}
	for _, tt := range tests {
		if got := tt.err.Error(); got != tt.want {
			t.Fatalf("Error() = %q, want %q", got, tt.want)
		}
	}
}

func TestAPIErrorUnwrapsTransport(t *testing.T) {
	te := client.NewTimeoutError()
	err := NewTransportError(te)
	if !errors.Is(err, te) {
		t.Fatalf("expected errors.Is to find wrapped transport error")
	}
	var target *client.TransportError
	if !errors.As(err, &target) || target.Kind != client.TransportErrorTimeout {
		t.Fatalf("expected errors.As to extract transport error")
	}
}
