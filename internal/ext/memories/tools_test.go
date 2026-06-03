package memories

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTempBackend(t *testing.T) (*LocalBackend, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "memories")
	return NewLocalBackend(root), root
}

func writeMemoryFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestMemoryToolNamespaceIsResponsesAPISafe(t *testing.T) {
	if MemoryToolsNamespace == "" {
		t.Fatal("namespace must not be empty")
	}
	for i := 0; i < len(MemoryToolsNamespace); i++ {
		c := MemoryToolsNamespace[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-'
		if !ok {
			t.Fatalf("namespace contains invalid byte %q", c)
		}
	}
}

func TestToolName(t *testing.T) {
	if got := ToolName(ListToolName); got != "memories/list" {
		t.Fatalf("ToolName = %q", got)
	}
}

func TestAddAdHocNoteCreatesNoteFile(t *testing.T) {
	backend, root := newTempBackend(t)
	args := mustJSON(t, map[string]string{
		"filename": "2026-05-26T13-42-08-remember-review-style.md",
		"note":     "Remember to keep PR review comments concise.",
	})
	res := CallAddAdHocNote(context.Background(), backend, args)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if string(res.Output) != "{}" {
		t.Fatalf("output = %s, want {}", res.Output)
	}
	got, err := os.ReadFile(filepath.Join(root, "extensions/ad_hoc/notes", "2026-05-26T13-42-08-remember-review-style.md"))
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	if string(got) != "Remember to keep PR review comments concise." {
		t.Fatalf("note content = %q", string(got))
	}
}

func TestAddAdHocNoteRejectsPathLikeFilename(t *testing.T) {
	backend, _ := newTempBackend(t)
	args := mustJSON(t, map[string]string{
		"filename": "../2026-05-26T13-42-08-remember-review-style.md",
		"note":     "Remember to keep PR review comments concise.",
	})
	res := CallAddAdHocNote(context.Background(), backend, args)
	if res.Err == nil {
		t.Fatal("expected error for path-like filename")
	}
	msg := res.Err.Error()
	if !strings.Contains(msg, "filename") || !strings.Contains(msg, "YYYY-MM-DDTHH-MM-SS") {
		t.Fatalf("error = %q", msg)
	}
}

func TestReadReadsMemoryFileWindow(t *testing.T) {
	backend, root := newTempBackend(t)
	writeMemoryFile(t, root, "MEMORY.md", "first line\nsecond needle line\nthird line\n")

	args := mustJSON(t, map[string]any{
		"path":        "MEMORY.md",
		"line_offset": 2,
		"max_lines":   1,
	})
	res := CallRead(context.Background(), backend, args)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	var got ReadResponse
	if err := json.Unmarshal(res.Output, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := ReadResponse{Path: "MEMORY.md", StartLineNumber: 2, Content: "second needle line\n", Truncated: true}
	if got != want {
		t.Fatalf("read = %#v, want %#v", got, want)
	}
}

func TestSearchAcceptsMultipleQueries(t *testing.T) {
	backend, root := newTempBackend(t)
	writeMemoryFile(t, root, "MEMORY.md", "alpha only\nneedle only\nalpha needle\n")

	args := mustJSON(t, map[string]any{
		"queries":        []string{"alpha", "needle"},
		"case_sensitive": false,
	})
	res := CallSearch(context.Background(), backend, args)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	wantJSON := `{"queries":["alpha","needle"],"match_mode":{"type":"any"},"path":null,"matches":[` +
		`{"path":"MEMORY.md","match_line_number":1,"content_start_line_number":1,"content":"alpha only","matched_queries":["alpha"]},` +
		`{"path":"MEMORY.md","match_line_number":2,"content_start_line_number":2,"content":"needle only","matched_queries":["needle"]},` +
		`{"path":"MEMORY.md","match_line_number":3,"content_start_line_number":3,"content":"alpha needle","matched_queries":["alpha","needle"]}],` +
		`"next_cursor":null,"truncated":false}`
	if string(res.Output) != wantJSON {
		t.Fatalf("search output:\n got %s\nwant %s", res.Output, wantJSON)
	}
}

func TestSearchWindowedAllMatchMode(t *testing.T) {
	backend, root := newTempBackend(t)
	writeMemoryFile(t, root, "MEMORY.md", "alpha\nmiddle\nneedle\n")

	args := mustJSON(t, map[string]any{
		"queries":    []string{"alpha", "needle"},
		"match_mode": map[string]any{"type": "all_within_lines", "line_count": 3},
	})
	res := CallSearch(context.Background(), backend, args)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	wantJSON := `{"queries":["alpha","needle"],"match_mode":{"type":"all_within_lines","line_count":3},"path":null,"matches":[` +
		`{"path":"MEMORY.md","match_line_number":1,"content_start_line_number":1,"content":"alpha\nmiddle\nneedle","matched_queries":["alpha","needle"]}],` +
		`"next_cursor":null,"truncated":false}`
	if string(res.Output) != wantJSON {
		t.Fatalf("search output:\n got %s\nwant %s", res.Output, wantJSON)
	}
}

func TestSearchRejectsLegacySingleQuery(t *testing.T) {
	backend, _ := newTempBackend(t)
	args := mustJSON(t, map[string]any{"query": "needle"})
	res := CallSearch(context.Background(), backend, args)
	if res.Err == nil {
		t.Fatal("expected unknown field error")
	}
	msg := res.Err.Error()
	if !strings.Contains(msg, "unknown field") || !strings.Contains(msg, "query") {
		t.Fatalf("error = %q", msg)
	}
}

func TestListReturnsSortedEntries(t *testing.T) {
	backend, root := newTempBackend(t)
	writeMemoryFile(t, root, "MEMORY.md", "x")
	writeMemoryFile(t, root, "skills/a/SKILL.md", "y")
	writeMemoryFile(t, root, ".hidden.md", "z")

	res := CallList(context.Background(), backend, "")
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	var got ListResponse
	if err := json.Unmarshal(res.Output, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Path != nil {
		t.Fatalf("path = %v, want nil", got.Path)
	}
	wantEntries := []MemoryEntry{
		{Path: "MEMORY.md", EntryType: EntryFile},
		{Path: "skills", EntryType: EntryDirectory},
	}
	if len(got.Entries) != len(wantEntries) {
		t.Fatalf("entries = %#v, want %#v", got.Entries, wantEntries)
	}
	for i := range wantEntries {
		if got.Entries[i] != wantEntries[i] {
			t.Fatalf("entry[%d] = %#v, want %#v", i, got.Entries[i], wantEntries[i])
		}
	}
}

func TestReadRejectsTraversalPath(t *testing.T) {
	backend, _ := newTempBackend(t)
	args := mustJSON(t, map[string]any{"path": "../escape.md"})
	res := CallRead(context.Background(), backend, args)
	if res.Err == nil {
		t.Fatal("expected traversal rejection")
	}
	if !strings.Contains(res.Err.Error(), "must stay within the memories root") {
		t.Fatalf("error = %q", res.Err.Error())
	}
}

func TestCallEmptyArgsUsesDefaults(t *testing.T) {
	backend, root := newTempBackend(t)
	writeMemoryFile(t, root, "MEMORY.md", "x")
	res := CallList(context.Background(), backend, "   ")
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
}
