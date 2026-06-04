package paritytest

// Turn-level differential parity test: prove that codexgo and the real codex
// 0.136.0 binary produce the *same observable turn* when both are pointed at the
// same fake `/v1/responses` endpoint via the same drop-in `config.toml`. This
// closes the "turn-level behavioral drop-in" gap recorded in docs/PARITY.md
// without needing any real OpenAI credentials.
//
// It is env-gated on CODEX_PARITY_BIN (reusing referenceBin) and skips when the
// var is unset, so CI stays hermetic. Run locally:
//
//	CODEX_PARITY_BIN=/path/to/codex \
//	  go test ./internal/paritytest/ -run TestParityTurnExec -v
//
// How each binary is driven (and why they differ):
//
//   - Real codex: the actual binary, exec --json, configured purely through the
//     CODEX_HOME/config.toml `[model_providers.parity]` block. The provider's
//     base_url points at the test server and env_key=PARITY_FAKE_KEY supplies a
//     dummy bearer token. This exercises codex's real Responses client, SSE
//     parser, turn loop, and exec JSONL serializer end to end.
//
//   - codexgo: driven IN-PROCESS through internal/appserver.Assemble + a real
//     appserver.NewModelClientFactory whose provider points at the same server.
//     This is required because codexgo's `cmd/codex exec` assembly
//     (internal/cli/assembly.go) always builds the model client with
//     CreateOpenAIProvider(nil) and resolves credentials only from
//     OPENAI_API_KEY / CODEX_API_KEY / auth.json -- it does NOT honor a custom
//     `[model_providers.parity]` base_url or its `env_key`, so the codexgo binary
//     would silently fall back to the scripted mock and never hit the server.
//     Driving codexgo in-process exercises the same real code path the binary
//     would use against api.openai.com (core.ResponsesModelClient -> the
//     internal/api SSE parser -> the exec turn loop -> the exec JSONL sink), just
//     with the provider base_url retargeted. The gap in the binary wiring is a
//     genuine drop-in finding, documented in docs/PARITY.md.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/client"
	"github.com/sqlrush/codexgo/internal/core"
	execpkg "github.com/sqlrush/codexgo/internal/exec"
	"github.com/sqlrush/codexgo/internal/modelproviderinfo"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// turnPrompt is the user prompt sent to both binaries. The fake server ignores
// the request body and always replies with the same deterministic stream, so the
// prompt only needs to be non-empty.
const turnPrompt = "hello"

// fakeAPIKey is the dummy bearer value the parity provider reads from
// PARITY_FAKE_KEY. The fake server never validates it.
const fakeAPIKey = "dummy"

// fakeEnvKey is the env var name the parity provider reads its API key from. It
// matches the config.toml `env_key` so both binaries authenticate identically.
const fakeEnvKey = "PARITY_FAKE_KEY"

// parityModelSlug is a slug present in the bundled model catalog of both
// binaries, so neither rejects it before the turn.
const parityModelSlug = "gpt-5.5"

// TestParityTurnExec drives one `exec --json` turn through the real codex binary
// and through codexgo (in-process), both pointed at the same fake Responses SSE
// endpoint, and asserts that the normalized JSONL event streams match.
func TestParityTurnExec(t *testing.T) {
	ref := referenceBin(t)

	srv := newResponsesServer(t)
	defer srv.Close()

	// Real codex: configured entirely via config.toml in a temp CODEX_HOME.
	refRaw := runRealCodexTurn(t, ref, srv.URL)
	refEvents := parseJSONL(t, "codex", refRaw)

	// codexgo: driven in-process with a real provider-backed model client that
	// targets the same server. See the file-level comment for why the binary is
	// not used here.
	cgoRaw := runCodexgoTurnInProcess(t, srv.URL)
	cgoEvents := parseJSONL(t, "codexgo", cgoRaw)

	// 1) Normalized event-type sequence must match.
	refSeq := eventTypeSequence(refEvents)
	cgoSeq := eventTypeSequence(cgoEvents)
	if strings.Join(refSeq, ",") != strings.Join(cgoSeq, ",") {
		t.Errorf("event-type sequence mismatch\n codex:   %v\n codexgo: %v", refSeq, cgoSeq)
	}

	// 2) Final agent message text must match.
	refMsg := finalAgentMessage(t, "codex", refEvents)
	cgoMsg := finalAgentMessage(t, "codexgo", cgoEvents)
	if refMsg != cgoMsg {
		t.Errorf("final agent message mismatch\n codex:   %q\n codexgo: %q", refMsg, cgoMsg)
	}

	// 3) turn.completed usage must match.
	refUsage := turnUsage(t, "codex", refEvents)
	cgoUsage := turnUsage(t, "codexgo", cgoEvents)
	if !bytes.Equal(refUsage, cgoUsage) {
		t.Errorf("turn usage mismatch\n codex:   %s\n codexgo: %s", refUsage, cgoUsage)
	}

	// 4) Full normalized event objects must match (after stripping volatile
	//    fields such as the thread id). This is the strongest assertion: it proves
	//    every event's payload is byte-identical, not just its type.
	refNorm := normalizeEvents(t, "codex", refEvents)
	cgoNorm := normalizeEvents(t, "codexgo", cgoEvents)
	if len(refNorm) != len(cgoNorm) {
		t.Fatalf("event count mismatch: codex=%d codexgo=%d", len(refNorm), len(cgoNorm))
	}
	for i := range refNorm {
		if !bytes.Equal(refNorm[i], cgoNorm[i]) {
			t.Errorf("event[%d] mismatch\n codex:   %s\n codexgo: %s", i, refNorm[i], cgoNorm[i])
		}
	}
}

// newResponsesServer starts an httptest.Server that serves a deterministic SSE
// stream for any POST whose path ends in "responses" and a minimal JSON body for
// any other request (e.g. a GET /models the client may issue). The SSE event
// shape mirrors codex-rs/core/tests/common/responses.rs exactly so both the real
// codex client and codexgo's internal/api parser accept it.
func newResponsesServer(t *testing.T) *httptest.Server {
	t.Helper()
	body := deterministicSSE()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), "responses") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, body)
			return
		}
		// Any other endpoint (e.g. /models) gets a harmless empty JSON payload so
		// the client stays hermetic.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"models":[]}`)
	}))
}

// deterministicSSE builds the fixed Server-Sent Events stream both clients
// consume. The format (event: <type>\n then data: <json>\n\n) and the event
// vocabulary match the Rust test harness `sse()` in
// codex-rs/core/tests/common/responses.rs:
//
//   - response.created (carries a response.id),
//   - response.output_item.added (the assistant message item begins),
//   - response.output_text.delta x3 ("Hello ", "from ", "parity"),
//   - response.output_item.done (the completed assistant message),
//   - response.completed (carries usage: input/output/total tokens).
func deterministicSSE() string {
	const respID = "resp_parity_0001"
	const msgID = "msg_parity_0001"
	events := []string{
		sseEvent("response.created", `{"type":"response.created","response":{"id":"`+respID+`"}}`),
		sseEvent("response.output_item.added",
			`{"type":"response.output_item.added","item":{"type":"message","role":"assistant","id":"`+msgID+`","content":[{"type":"output_text","text":""}]}}`),
		sseEvent("response.output_text.delta", `{"type":"response.output_text.delta","delta":"Hello "}`),
		sseEvent("response.output_text.delta", `{"type":"response.output_text.delta","delta":"from "}`),
		sseEvent("response.output_text.delta", `{"type":"response.output_text.delta","delta":"parity"}`),
		sseEvent("response.output_item.done",
			`{"type":"response.output_item.done","item":{"type":"message","role":"assistant","id":"`+msgID+`","content":[{"type":"output_text","text":"Hello from parity"}]}}`),
		sseEvent("response.completed",
			`{"type":"response.completed","response":{"id":"`+respID+`","usage":{"input_tokens":11,"input_tokens_details":null,"output_tokens":3,"output_tokens_details":null,"total_tokens":14}}}`),
	}
	return strings.Join(events, "")
}

// sseEvent renders one SSE frame: an `event:` line, a `data:` line, and the
// terminating blank line, matching the wire format the Responses API uses.
func sseEvent(kind, data string) string {
	return "event: " + kind + "\ndata: " + data + "\n\n"
}

// runRealCodexTurn writes a drop-in config.toml into a temp CODEX_HOME and runs
// the real codex binary's `exec --json` against the fake server. It returns the
// captured stdout (the JSONL stream). The binary is configured *only* through
// config.toml + the PARITY_FAKE_KEY env var, proving format-and-behavior drop-in
// for the real binary.
func runRealCodexTurn(t *testing.T, bin, serverURL string) string {
	t.Helper()
	home := t.TempDir()
	writeParityConfig(t, home, serverURL)

	cmd := exec.Command(bin, "exec", "--json", "--skip-git-repo-check", turnPrompt)
	cmd.Env = append(os.Environ(),
		"CODEX_HOME="+home,
		fakeEnvKey+"="+fakeAPIKey,
	)
	// Empty stdin so exec does not block waiting for piped input.
	cmd.Stdin = bytes.NewReader(nil)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("real codex exec failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

// writeParityConfig writes the drop-in config.toml that points a custom
// `parity` model provider at serverURL. The same file shape is accepted by both
// binaries. base_url ends in "/v1" so the client appends "/responses" to form
// "<serverURL>/v1/responses" (matching Provider::url_for_path in codex-rs).
func writeParityConfig(t *testing.T, home, serverURL string) {
	t.Helper()
	cfg := "" +
		"model = \"" + parityModelSlug + "\"\n" +
		"model_provider = \"parity\"\n" +
		"\n" +
		"[model_providers.parity]\n" +
		"name = \"parity\"\n" +
		"base_url = \"" + serverURL + "/v1\"\n" +
		"wire_api = \"responses\"\n" +
		"requires_openai_auth = false\n" +
		"env_key = \"" + fakeEnvKey + "\"\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
}

// runCodexgoTurnInProcess drives one codexgo exec --json turn in-process against
// serverURL, returning the captured JSONL stdout. It builds the engine the same
// way codexgo's binary does (appserver.Assemble + appserver.NewModelClientFactory
// selecting a real core.ResponsesModelClient), but with the provider retargeted
// at the fake server and a static auth resolver supplying the dummy bearer token.
func runCodexgoTurnInProcess(t *testing.T, serverURL string) string {
	t.Helper()

	baseURL := serverURL + "/v1"
	provider := modelproviderinfo.ModelProviderInfo{
		Name:               "parity",
		BaseURL:            &baseURL,
		EnvKey:             strPtr(fakeEnvKey),
		WireApi:            modelproviderinfo.WireApiResponses,
		RequiresOpenAIAuth: false,
	}

	mode := appserverproto.AuthModeApiKey
	resolver := appserver.AuthResolverFunc(
		func(context.Context, protocol.ThreadID, core.SessionConfiguration) (appserver.ResolvedAuth, error) {
			return appserver.ResolvedAuth{
				HasCredentials: true,
				AuthProvider:   bearerAuthProvider{token: fakeAPIKey},
				AuthMode:       &mode,
			}, nil
		},
	)

	// The fallback mock must never be selected here (credentials are always
	// present); supply one anyway so the factory contract is satisfied.
	fallback := appserver.ModelClientFactory(
		func(_ context.Context, _ protocol.ThreadID, cfg core.SessionConfiguration) (core.ModelClient, error) {
			t.Errorf("codexgo unexpectedly fell back to the mock client; the real client should have been selected")
			return core.NewMockModelClient(cfg.Model(), nil), nil
		},
	)

	factory, err := appserver.NewModelClientFactory(appserver.RealModelClientFactoryConfig{
		AuthResolver: resolver,
		Provider:     provider,
		Fallback:     fallback,
	})
	if err != nil {
		t.Fatalf("build model client factory: %v", err)
	}

	asm, err := appserver.Assemble(appserver.AssemblyConfig{
		ModelClientFactory: factory,
		DefaultModel:       parityModelSlug,
	})
	if err != nil {
		t.Fatalf("assemble codexgo engine: %v", err)
	}

	cli, err := execpkg.ParseArgs([]string{"--json", "--skip-git-repo-check", turnPrompt})
	if err != nil {
		t.Fatalf("parse exec args: %v", err)
	}

	var stdout, stderr bytes.Buffer
	env := execpkg.Environment{
		Stdin:    bytes.NewReader(nil),
		Stdout:   &stdout,
		Stderr:   &stderr,
		Assembly: asm,
		Defaults: appserver.Defaults{
			Model:      parityModelSlug,
			ProviderID: "parity",
			Cwd:        ".",
			UserAgent:  "codex-cli-go",
		},
	}
	if code := execpkg.Run(context.Background(), cli, env); code != 0 {
		t.Fatalf("codexgo exec exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	return stdout.String()
}

// bearerAuthProvider is a minimal api.AuthProvider that attaches a static bearer
// token, mirroring the Authorization header the real codex sends from its
// env_key-derived API key. It never mutates the incoming request.
type bearerAuthProvider struct{ token string }

// compile-time assertion that bearerAuthProvider satisfies api.AuthProvider.
var _ api.AuthProvider = bearerAuthProvider{}

// AddAuthHeaders sets the Authorization bearer header.
func (b bearerAuthProvider) AddAuthHeaders(h http.Header) {
	h.Set("Authorization", "Bearer "+b.token)
}

// ApplyAuth returns a copy of req with the bearer header applied.
func (b bearerAuthProvider) ApplyAuth(_ context.Context, req client.Request) (client.Request, *api.AuthError) {
	out := req.WithCompression(req.Compression)
	b.AddAuthHeaders(out.Headers)
	return out, nil
}

// parseJSONL splits raw stdout into one decoded JSON object per non-blank line,
// failing the test on the first malformed line. The who argument identifies the
// producer ("codex" or "codexgo") in error messages.
func parseJSONL(t *testing.T, who, raw string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("%s emitted a non-JSON line: %q (%v)", who, line, err)
		}
		out = append(out, ev)
	}
	if len(out) == 0 {
		t.Fatalf("%s produced no JSONL events; raw output:\n%s", who, raw)
	}
	return out
}

// eventTypeSequence extracts the ordered list of event "type" tags.
func eventTypeSequence(events []map[string]any) []string {
	seq := make([]string, 0, len(events))
	for _, ev := range events {
		if tag, ok := ev["type"].(string); ok {
			seq = append(seq, tag)
		} else {
			seq = append(seq, "<missing-type>")
		}
	}
	return seq
}

// finalAgentMessage returns the text of the last agent_message item in the
// stream, failing the test if none is present.
func finalAgentMessage(t *testing.T, who string, events []map[string]any) string {
	t.Helper()
	var text string
	found := false
	for _, ev := range events {
		item, ok := ev["item"].(map[string]any)
		if !ok {
			continue
		}
		if item["type"] != "agent_message" {
			continue
		}
		if s, ok := item["text"].(string); ok {
			text = s
			found = true
		}
	}
	if !found {
		t.Fatalf("%s stream has no agent_message item", who)
	}
	return text
}

// turnUsage returns the canonicalized usage object from the turn.completed event,
// failing the test if it is absent.
func turnUsage(t *testing.T, who string, events []map[string]any) []byte {
	t.Helper()
	for _, ev := range events {
		if ev["type"] != "turn.completed" {
			continue
		}
		usage, ok := ev["usage"]
		if !ok {
			t.Fatalf("%s turn.completed has no usage field", who)
		}
		b, err := json.Marshal(usage)
		if err != nil {
			t.Fatalf("%s marshal usage: %v", who, err)
		}
		return b
	}
	t.Fatalf("%s stream has no turn.completed event", who)
	return nil
}

// normalizeEvents returns the canonicalized (sorted-key, whitespace-free) JSON
// for each event after stripping volatile fields that legitimately differ
// between runs and between the two producers:
//
//   - thread.started.thread_id: codex mints a UUIDv7, codexgo a monotonic id;
//   - any item "id" field: opaque per-run identifiers.
//
// Everything else (event type, message text, usage counts, item shapes) must
// match byte-for-byte.
func normalizeEvents(t *testing.T, who string, events []map[string]any) [][]byte {
	t.Helper()
	out := make([][]byte, 0, len(events))
	for _, ev := range events {
		stripVolatile(ev)
		b, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("%s marshal normalized event: %v", who, err)
		}
		out = append(out, b)
	}
	return out
}

// stripVolatile removes per-run identifiers from an event in place: the
// top-level thread_id and any nested item id. encoding/json marshals maps in
// sorted-key order, so the result is canonical without a separate pass.
func stripVolatile(ev map[string]any) {
	delete(ev, "thread_id")
	if item, ok := ev["item"].(map[string]any); ok {
		delete(item, "id")
	}
}

// strPtr returns a pointer to s.
func strPtr(s string) *string { return &s }
