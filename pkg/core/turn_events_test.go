package core

// Tests for turn-event helpers: the model-facing error message extraction that
// surfaces upstream HTTP bodies the way codex does.

import (
	"fmt"
	"testing"

	"github.com/sqlrush/codexgo/internal/client"
)

// TestModelFacingErrorMessage asserts HTTP transport failures surface the
// upstream response body verbatim (the codex UnexpectedStatus display) while
// other errors keep their full message.
func TestModelFacingErrorMessage(t *testing.T) {
	body := `{"error":{"message":"boom"}}`
	httpErr := client.NewHTTPError(400, "http://x/v1/responses", nil, body)
	wrapped := fmt.Errorf("core: model stream failed: %w", fmt.Errorf("core: responses stream request: %w", httpErr))
	if got := modelFacingErrorMessage(wrapped); got != body {
		t.Errorf("http error message = %q, want upstream body", got)
	}

	plain := fmt.Errorf("core: something else broke")
	if got := modelFacingErrorMessage(plain); got != plain.Error() {
		t.Errorf("plain error message = %q, want full text", got)
	}
}
