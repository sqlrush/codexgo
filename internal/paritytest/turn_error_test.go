package paritytest

// Error-path parity: when the model endpoint returns a non-retryable API error,
// codexgo must surface the SAME terminal error behavior as codex — same exit code
// and the same kind of error event in the exec --json stream. Robust differential:
// asserts codexgo == codex regardless of the exact mapping, and flags a genuine
// divergence if they differ. Env-gated on CODEX_PARITY_BIN.

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// newErrorServer returns a server that answers /responses with a non-retryable
// HTTP 400 + an OpenAI-style error body (4xx other than 429 is not retried, so the
// turn fails deterministically on the first request).
func newErrorServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), "responses") {
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"parity boom","type":"invalid_request_error","code":"bad_parity"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"models":[]}`)
	}))
}

// runErrorTurn runs the binary's exec --json against srv, tolerating (and
// returning) a non-zero exit code instead of failing the test.
func runErrorTurn(t *testing.T, who, bin, serverURL string) (string, int) {
	t.Helper()
	home := t.TempDir()
	work := t.TempDir()
	writeToolCallConfig(t, home, serverURL)

	cmd := exec.Command(bin, "exec", "--json", "--skip-git-repo-check", "-C", work, toolTurnPrompt)
	cmd.Env = append(os.Environ(),
		"CODEX_HOME="+home,
		fakeEnvKey+"="+fakeAPIKey,
		"OPENAI_API_KEY=",
		"CODEX_API_KEY=",
		"CODEX_ACCESS_TOKEN=",
	)
	cmd.Stdin = bytes.NewReader(nil)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("%s exec (error turn) failed to run: %v", who, err)
	}
	return stdout.String(), code
}

// TestParityTurnError asserts codexgo surfaces the same exit code and the same
// normalized error event(s) as codex when the API returns a non-retryable error.
func TestParityTurnError(t *testing.T) {
	refBin := referenceBin(t)
	cgoBin := buildCodexgo(t)

	run := func(who, bin string) ([]map[string]any, int) {
		srv := newErrorServer(t)
		defer srv.Close()
		out, code := runErrorTurn(t, who, bin, srv.URL)
		return parseJSONL(t, who, out), code
	}
	refEvents, refCode := run("codex", refBin)
	cgoEvents, cgoCode := run("codexgo", cgoBin)

	// Both must fail with a non-zero exit, and the SAME code.
	if refCode == 0 {
		t.Fatalf("codex unexpectedly exited 0 on API error")
	}
	if cgoCode != refCode {
		t.Errorf("exit code mismatch on API error: codex=%d codexgo=%d", refCode, cgoCode)
	}

	// Both must emit at least one terminal error event.
	hasError := func(evs []map[string]any) bool {
		for _, ev := range evs {
			if tag, _ := ev["type"].(string); strings.Contains(tag, "error") {
				return true
			}
		}
		return false
	}
	if !hasError(refEvents) {
		t.Fatalf("codex emitted no error event:\n%v", refEvents)
	}
	if !hasError(cgoEvents) {
		t.Errorf("codexgo emitted no error event (codex did):\n%v", cgoEvents)
	}

	// Both must emit the SAME terminal event type. codex emits turn.failed on a
	// failed turn; codexgo previously emitted turn.completed (now fixed).
	if a, b := terminalEventType(refEvents), terminalEventType(cgoEvents); a != b {
		t.Errorf("terminal event mismatch: codex=%q codexgo=%q", a, b)
	} else if a != "turn.failed" {
		t.Errorf("expected terminal turn.failed on API error, got %q", a)
	}
	if a, b := eventTypeSequence(refEvents), eventTypeSequence(cgoEvents); strings.Join(a, ",") != strings.Join(b, ",") {
		t.Errorf("error-turn event-type sequence mismatch:\n codex:   %v\n codexgo: %v", a, b)
	}
	// NOTE (documented deviation, docs/PARITY.md): the error *message* text differs
	// — codex surfaces the clean upstream error body, while codexgo currently leaks
	// internal wrapping ("core: model stream failed: ..."). The event SHAPE, exit
	// code, and terminal turn.failed all match; the message-text cleanup is tracked.
}

// terminalEventType returns the "type" of the last event in the stream.
func terminalEventType(events []map[string]any) string {
	if len(events) == 0 {
		return ""
	}
	t, _ := events[len(events)-1]["type"].(string)
	return t
}
