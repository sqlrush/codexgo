package paritytest

// Reasoning-turn parity: a turn whose stream carries a reasoning item + summary
// deltas before the final message. The assertion is robust to whether codex
// surfaces reasoning in `exec --json` (visibility is config-gated): it requires
// codexgo to produce the SAME normalized JSONL as codex, whatever that is — so it
// confirms drop-in either way and flags a genuine divergence if they differ.

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// reasoningServer answers a single-request turn: a reasoning output item +
// reasoning summary delta, then the final assistant message.
type reasoningServer struct {
	*httptest.Server
	mu sync.Mutex
	n  int
}

func newReasoningServer(t *testing.T) *reasoningServer {
	t.Helper()
	s := &reasoningServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), "responses") {
			_, _ = io.Copy(io.Discard, r.Body)
			s.mu.Lock()
			s.n++
			s.mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			reasoningItem := `{"type":"response.output_item.done","item":{"type":"reasoning","id":"rs_parity_1","summary":[{"type":"summary_text","text":"thinking about parity"}]}}`
			msgItem := `{"type":"response.output_item.done","item":{"type":"message","role":"assistant","id":"msg_reasoning","content":[{"type":"output_text","text":"reasoned answer"}]}}`
			_, _ = io.WriteString(w, strings.Join([]string{
				sseEvent("response.created", `{"type":"response.created","response":{"id":"resp_reason_1"}}`),
				sseEvent("response.output_item.done", reasoningItem),
				sseEvent("response.reasoning_summary_text.delta", `{"type":"response.reasoning_summary_text.delta","delta":"thinking about parity"}`),
				sseEvent("response.output_item.done", msgItem),
				sseEvent("response.completed", toolTurnCompleted("resp_reason_1")),
			}, ""))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"models":[]}`)
	}))
	return s
}

// TestParityTurnReasoning asserts codexgo and codex produce identical normalized
// JSONL for a reasoning-bearing turn (drop-in regardless of reasoning visibility).
func TestParityTurnReasoning(t *testing.T) {
	refBin := referenceBin(t)
	cgoBin := buildCodexgo(t)

	run := func(who, bin string) ([]map[string]any, string) {
		srv := newReasoningServer(t)
		defer srv.Close()
		out, work := runToolCallTurn(t, who, bin, srv.URL)
		return parseJSONL(t, who, out), work
	}
	refEvents, refWork := run("codex", refBin)
	cgoEvents, cgoWork := run("codexgo", cgoBin)

	// Both must reach a normal final agent message.
	if got := finalAgentMessage(t, "codex", refEvents); got != "reasoned answer" {
		t.Fatalf("codex final message = %q, want %q", got, "reasoned answer")
	}

	refNorm := normalizeToolEvents(t, "codex", refEvents, refWork)
	cgoNorm := normalizeToolEvents(t, "codexgo", cgoEvents, cgoWork)
	if len(refNorm) != len(cgoNorm) {
		t.Fatalf("event count mismatch: codex=%d codexgo=%d\n codex:   %s\n codexgo: %s",
			len(refNorm), len(cgoNorm), bytes.Join(refNorm, []byte("\n")), bytes.Join(cgoNorm, []byte("\n")))
	}
	for i := range refNorm {
		if !bytes.Equal(refNorm[i], cgoNorm[i]) {
			t.Errorf("reasoning event[%d] mismatch\n codex:   %s\n codexgo: %s", i, refNorm[i], cgoNorm[i])
		}
	}
}
