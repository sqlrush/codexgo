package paritytest

// Multi-step tool-call parity: a THREE-request agent loop driven through a fake
// /v1/responses endpoint proves the codexgo binary loops over MULTIPLE tool calls
// exactly like codex 0.136.0 (request 1 -> shell_command, request 2 -> a SECOND
// shell_command, request 3 -> final message). Reuses the turn_toolcall_test.go
// helpers (runToolCallTurn, parseJSONL, normalizeToolEvents, lifecycleItemTypes,
// sseEvent, toolTurnCompleted, mustJSONString). Env-gated on CODEX_PARITY_BIN.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// multiStepServer answers a 3-request loop with two distinct shell_command calls
// then a final assistant message. A fresh server is used per binary run so its
// request counter is per-run.
type multiStepServer struct {
	*httptest.Server
	mu sync.Mutex
	n  int
}

func newMultiStepServer(t *testing.T) *multiStepServer {
	t.Helper()
	s := &multiStepServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), "responses") {
			_, _ = io.Copy(io.Discard, r.Body)
			s.mu.Lock()
			s.n++
			n := s.n
			s.mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			switch n {
			case 1:
				_, _ = io.WriteString(w, shellCallStream("call_ms_1", "echo step-one", "resp_ms_1"))
			case 2:
				_, _ = io.WriteString(w, shellCallStream("call_ms_2", "echo step-two", "resp_ms_2"))
			default:
				_, _ = io.WriteString(w, finalMessageStream("did two steps", "resp_ms_3"))
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"models":[]}`)
	}))
	return s
}

func (s *multiStepServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

// shellCallStream renders a function_call(shell_command{command}) + completed.
func shellCallStream(callID, command, respID string) string {
	args, _ := json.Marshal(map[string]any{"command": command})
	item, _ := json.Marshal(map[string]any{
		"type":      "function_call",
		"call_id":   callID,
		"name":      "shell_command",
		"arguments": string(args),
	})
	done, _ := json.Marshal(map[string]any{
		"type": "response.output_item.done",
		"item": json.RawMessage(item),
	})
	return strings.Join([]string{
		sseEvent("response.created", `{"type":"response.created","response":{"id":"`+respID+`"}}`),
		sseEvent("response.output_item.done", string(done)),
		sseEvent("response.completed", toolTurnCompleted(respID)),
	}, "")
}

// finalMessageStream renders an assistant message item + completed, matching the
// shape used by the single-tool-call test's finalStream.
func finalMessageStream(text, respID string) string {
	msg := `{"type":"response.output_item.done","item":{"type":"message","role":"assistant","id":"msg_ms_final","content":[{"type":"output_text","text":` +
		mustJSONString(text) + `}]}}`
	return strings.Join([]string{
		sseEvent("response.created", `{"type":"response.created","response":{"id":"`+respID+`"}}`),
		sseEvent("response.output_item.done", msg),
		sseEvent("response.completed", toolTurnCompleted(respID)),
	}, "")
}

// TestParityTurnMultiStep proves the codexgo binary loops over two sequential
// shell_command tool calls and ends with the assistant message exactly like codex
// (byte-identical normalized JSONL, two command_execution lifecycles).
func TestParityTurnMultiStep(t *testing.T) {
	refBin := referenceBin(t)
	cgoBin := buildCodexgo(t)

	run := func(who, bin string) ([]map[string]any, string) {
		srv := newMultiStepServer(t)
		defer srv.Close()
		out, work := runToolCallTurn(t, who, bin, srv.URL)
		if got := srv.count(); got != 3 {
			t.Fatalf("%s: expected 3 /responses requests (tool, tool, final), got %d\n%s", who, got, out)
		}
		return parseJSONL(t, who, out), work
	}

	refEvents, refWork := run("codex", refBin)
	cgoEvents, cgoWork := run("codexgo", cgoBin)

	// Both must run BOTH commands (two command_execution lifecycles) + final message.
	for who, ev := range map[string][]map[string]any{"codex": refEvents, "codexgo": cgoEvents} {
		execs := 0
		for _, it := range lifecycleItemTypes(ev) {
			if it == "command_execution" {
				execs++
			}
		}
		if execs < 2 {
			t.Fatalf("%s: expected 2 command_execution items, got %d (%v)", who, execs, lifecycleItemTypes(ev))
		}
	}

	// Strongest assertion: byte-identical normalized event streams.
	refNorm := normalizeToolEvents(t, "codex", refEvents, refWork)
	cgoNorm := normalizeToolEvents(t, "codexgo", cgoEvents, cgoWork)
	if len(refNorm) != len(cgoNorm) {
		t.Fatalf("event count mismatch: codex=%d codexgo=%d\n codex:   %s\n codexgo: %s",
			len(refNorm), len(cgoNorm), bytes.Join(refNorm, []byte("\n")), bytes.Join(cgoNorm, []byte("\n")))
	}
	for i := range refNorm {
		if !bytes.Equal(refNorm[i], cgoNorm[i]) {
			t.Errorf("multi-step event[%d] mismatch\n codex:   %s\n codexgo: %s", i, refNorm[i], cgoNorm[i])
		}
	}
}
