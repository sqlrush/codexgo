package paritytest

// tool_search dispatch parity: the model issues a tool_search call and BOTH
// binaries must feed back the same tool_search_output — the BM25-matched
// deferred tool specs (the multi_agent_v1 namespace in a bare run) serialized
// as LoadableToolSpecs. This locks the deferred-tool registry contents, the
// search behavior, and the output item shape. Env-gated on CODEX_PARITY_BIN.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
)

// toolSearchCallID is the call_id for the scripted tool_search call.
const toolSearchCallID = "call_parity_search_1"

// toolSearchServer is a two-request fake Responses server: request 1 streams a
// tool_search_call, request 2 streams the final assistant message.
type toolSearchServer struct {
	*httptest.Server

	mu       sync.Mutex
	requests [][]byte
}

func newToolSearchServer(t *testing.T, query string) *toolSearchServer {
	t.Helper()
	s := &toolSearchServer{}
	argsJSON, err := json.Marshal(map[string]any{"query": query})
	if err != nil {
		t.Fatalf("marshal query args: %v", err)
	}
	item, err := json.Marshal(map[string]any{
		"type":      "tool_search_call",
		"call_id":   toolSearchCallID,
		"status":    "completed",
		"execution": "client",
		"arguments": json.RawMessage(argsJSON),
	})
	if err != nil {
		t.Fatalf("marshal tool_search_call item: %v", err)
	}
	done, err := json.Marshal(map[string]any{
		"type": "response.output_item.done",
		"item": json.RawMessage(item),
	})
	if err != nil {
		t.Fatalf("marshal done event: %v", err)
	}
	firstStream := strings.Join([]string{
		sseEvent("response.created", `{"type":"response.created","response":{"id":"resp_search_1"}}`),
		sseEvent("response.output_item.done", string(done)),
		sseEvent("response.completed", toolTurnCompleted("resp_search_1")),
	}, "")
	finalStream := strings.Join([]string{
		sseEvent("response.created", `{"type":"response.created","response":{"id":"resp_search_2"}}`),
		sseEvent("response.output_item.done", `{"type":"response.output_item.done","item":{"type":"message","role":"assistant","id":"msg_search_2","content":[{"type":"output_text","text":"done"}]}}`),
		sseEvent("response.completed", toolTurnCompleted("resp_search_2")),
	}, "")

	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), "responses") {
			body, _ := io.ReadAll(r.Body)
			s.mu.Lock()
			s.requests = append(s.requests, body)
			n := len(s.requests)
			s.mu.Unlock()

			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if n == 1 {
				_, _ = io.WriteString(w, firstStream)
				return
			}
			_, _ = io.WriteString(w, finalStream)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"models":[]}`)
	}))
	return s
}

// fedBackSearchOutput returns the canonical JSON of the tool_search_output item
// the binary sent back, and whether it was found.
func (s *toolSearchServer) fedBackSearchOutput(t *testing.T) (string, bool) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, body := range s.requests {
		var req struct {
			Input []json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			continue
		}
		for _, raw := range req.Input {
			var item map[string]any
			if err := json.Unmarshal(raw, &item); err != nil {
				continue
			}
			if item["type"] != "tool_search_output" || item["call_id"] != toolSearchCallID {
				continue
			}
			return canonField(t, raw), true
		}
	}
	return "", false
}

// TestParityToolSearchTurn asserts both binaries answer a tool_search call with
// the same tool_search_output: same matched deferred specs (the multi_agent_v1
// namespace tools), same status/execution shape, byte-identical once the BM25
// tie order is canonicalized.
//
// Tie-order note: codex's ToolSearchHandler collects BM25 candidates into a
// std::collections::HashSet before its stable sort, so tools with EQUAL BM25
// scores are emitted in a process-random order (the multi_agent corpus has a
// tie between close_agent and resume_agent — both score-identical for the query
// "spawn agents"). codex therefore randomizes that pair run-to-run. codexgo is
// deterministic (descending document id). To compare against a non-deterministic
// reference, both outputs are canonicalized by sorting each namespace's inner
// tools by name; the relevance leader (spawn_agent) is additionally asserted to
// be first in BOTH raw outputs so a genuine ranking regression still fails.
func TestParityToolSearchTurn(t *testing.T) {
	refBin := referenceBin(t)
	cgoBin := buildCodexgo(t)

	raw := map[string]string{}
	for _, c := range []struct{ who, bin string }{
		{"codex", refBin},
		{"codexgo", cgoBin},
	} {
		srv := newToolSearchServer(t, "spawn agents")
		runToolCallTurn(t, c.who, c.bin, srv.URL)
		out, ok := srv.fedBackSearchOutput(t)
		srv.Close()
		if !ok {
			t.Fatalf("%s fed back no tool_search_output", c.who)
		}
		raw[c.who] = out
	}

	// Guard: the highest-relevance match (spawn_agent) must lead on both sides;
	// this is a deterministic, non-tied position, so a regression here is real.
	for _, who := range []string{"codex", "codexgo"} {
		if first := firstNamespaceToolName(t, raw[who]); first != "spawn_agent" {
			t.Errorf("%s: first matched tool = %q, want spawn_agent (relevance leader)", who, first)
		}
	}

	// Canonicalize the BM25-tie order (sort each namespace's tools by name) and
	// compare byte-for-byte.
	canonCodex := canonicalizeToolSearchOutput(t, raw["codex"])
	canonCgo := canonicalizeToolSearchOutput(t, raw["codexgo"])
	if canonCodex != canonCgo {
		_ = os.WriteFile("/tmp/codex_toolsearch_output.json", []byte(raw["codex"]), 0o644)
		_ = os.WriteFile("/tmp/codexgo_toolsearch_output.json", []byte(raw["codexgo"]), 0o644)
		t.Errorf("tool_search_output mismatch (full bodies in /tmp/codex*_toolsearch_output.json)\n codex:   %s\n codexgo: %s",
			truncate(canonCodex, 600), truncate(canonCgo, 600))
	}
}

// canonicalizeToolSearchOutput re-marshals a tool_search_output with every
// namespace's inner `tools` array sorted by tool name, so the comparison ignores
// the process-random order codex assigns to BM25-tied tools while keeping every
// other byte (specs, structure, status) enforced.
func canonicalizeToolSearchOutput(t *testing.T, canonical string) string {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(canonical), &v); err != nil {
		t.Fatalf("decode tool_search_output: %v", err)
	}
	toolsList, _ := v["tools"].([]any)
	for _, entry := range toolsList {
		obj, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		inner, ok := obj["tools"].([]any)
		if !ok {
			continue
		}
		sort.SliceStable(inner, func(i, j int) bool {
			return toolName(inner[i]) < toolName(inner[j])
		})
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("re-marshal tool_search_output: %v", err)
	}
	return string(b)
}

// firstNamespaceToolName returns the name of the first tool in the first
// namespace of a tool_search_output, or "" when absent.
func firstNamespaceToolName(t *testing.T, canonical string) string {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(canonical), &v); err != nil {
		t.Fatalf("decode tool_search_output: %v", err)
	}
	toolsList, _ := v["tools"].([]any)
	if len(toolsList) == 0 {
		return ""
	}
	ns, ok := toolsList[0].(map[string]any)
	if !ok {
		return ""
	}
	inner, ok := ns["tools"].([]any)
	if !ok || len(inner) == 0 {
		return ""
	}
	return toolName(inner[0])
}

// toolName extracts the "name" of a tool object, or "" when absent.
func toolName(v any) string {
	obj, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	name, _ := obj["name"].(string)
	return name
}
