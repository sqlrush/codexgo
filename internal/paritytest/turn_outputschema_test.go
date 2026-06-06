package paritytest

// Output-schema (structured-output) REQUEST-shape parity: `codex exec
// --output-schema <FILE>` must make the binary send the same `text.format` block
// in its POST /v1/responses body as real codex. This is a request-SIDE
// differential (what the binary SENDS) complementing the response-side turn
// tests. codex builds it via create_text_param_for_request:
//
//	"text": {"format": {"type":"json_schema","strict":true,
//	         "schema":<schema>,"name":"codex_output_schema"}}
//
// Both binaries are driven through the same fake server (which records request
// bodies, then replies with the deterministic single-message stream so the turn
// completes), and the captured `text` blocks are compared. Env-gated on
// CODEX_PARITY_BIN.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// capturingServer records every /responses request body and replies with the
// deterministic single-message SSE stream so the turn completes normally.
type capturingServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests [][]byte
}

func newCapturingServer(t *testing.T) *capturingServer {
	t.Helper()
	s := &capturingServer{}
	body := deterministicSSE()
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && hasResponsesSuffix(r.URL.Path) {
			b, _ := io.ReadAll(r.Body)
			s.mu.Lock()
			s.requests = append(s.requests, b)
			s.mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, body)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"models":[]}`)
	}))
	return s
}

// hasResponsesSuffix reports whether path ends in "responses" (ignoring a
// trailing slash), matching the route both binaries POST to.
func hasResponsesSuffix(path string) bool {
	for len(path) > 0 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	const suffix = "responses"
	return len(path) >= len(suffix) && path[len(path)-len(suffix):] == suffix
}

// firstRequestText returns the canonicalized `text` field of the first captured
// request body, and whether a request with a `text` field was seen.
func (s *capturingServer) firstRequestText(t *testing.T) ([]byte, bool) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, body := range s.requests {
		var req map[string]json.RawMessage
		if err := json.Unmarshal(body, &req); err != nil {
			continue
		}
		raw, ok := req["text"]
		if !ok {
			continue
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("decode text field: %v", err)
		}
		canon, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("re-marshal text field: %v", err)
		}
		return canon, true
	}
	return nil, false
}

// schemaJSON is the JSON Schema written to --output-schema for both binaries.
const schemaJSON = `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`

// runOutputSchemaTurn runs `bin exec --json --output-schema <file>` against srv.
func runOutputSchemaTurn(t *testing.T, who, bin, serverURL string) {
	t.Helper()
	home := t.TempDir()
	work := t.TempDir()
	writeParityConfig(t, home, serverURL)
	schemaPath := filepath.Join(work, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(schemaJSON), 0o644); err != nil {
		t.Fatalf("write schema.json: %v", err)
	}

	cmd := exec.Command(bin, "exec", "--json", "--skip-git-repo-check", "-C", work,
		"--output-schema", schemaPath, turnPrompt)
	cmd.Env = append(os.Environ(),
		"CODEX_HOME="+home,
		"CODEXGO_HOME="+home,
		fakeEnvKey+"="+fakeAPIKey,
		"OPENAI_API_KEY=",
		"CODEX_API_KEY=",
		"CODEXGO_API_KEY=",
		"CODEX_ACCESS_TOKEN=",
		"CODEXGO_ACCESS_TOKEN=",
	)
	cmd.Stdin = bytes.NewReader(nil)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s exec --output-schema failed: %v\nstdout:\n%s\nstderr:\n%s", who, err, stdout.String(), stderr.String())
	}
}

// TestParityOutputSchemaRequest asserts codexgo sends the SAME `text.format`
// json_schema block in its /responses request as real codex when driven with
// --output-schema.
func TestParityOutputSchemaRequest(t *testing.T) {
	refBin := referenceBin(t)
	cgoBin := buildCodexgo(t)

	capture := func(who, bin string) []byte {
		srv := newCapturingServer(t)
		defer srv.Close()
		runOutputSchemaTurn(t, who, bin, srv.URL)
		text, ok := srv.firstRequestText(t)
		if !ok {
			t.Fatalf("%s sent no request with a `text` field", who)
		}
		return text
	}
	ref := capture("codex", refBin)
	cgo := capture("codexgo", cgoBin)

	// The reference must embed the json_schema format with codex's fixed name.
	if !bytes.Contains(ref, []byte(`"json_schema"`)) || !bytes.Contains(ref, []byte(`"codex_output_schema"`)) {
		t.Fatalf("codex text block missing json_schema/codex_output_schema: %s", ref)
	}
	if !bytes.Equal(ref, cgo) {
		t.Errorf("output-schema request `text` block mismatch\n codex:   %s\n codexgo: %s", ref, cgo)
	}
}
