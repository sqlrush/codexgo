package memories

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestValidateFilename(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
		errSub  string
	}{
		{"valid", "2026-05-26T13-42-08-remember-this.md", false, ""},
		{"missing extension", "2026-05-26T13-42-08-remember-this.txt", true, "must end with .md"},
		{"bad timestamp", "2026-05-99X13-42-08-remember-this.md", true, "YYYY-MM-DDTHH-MM-SS"},
		{"empty slug reports timestamp error", "2026-05-26T13-42-08-.md", true, "YYYY-MM-DDTHH-MM-SS"},
		{"slug too long", "2026-05-26T13-42-08-" + strings.Repeat("a", 81) + ".md", true, "slug must be 1 to 80 bytes"},
		{"uppercase slug", "2026-05-26T13-42-08-Remember.md", true, "lowercase ASCII"},
		{"too long", "2026-05-26T13-42-08-" + strings.Repeat("a", 200) + ".md", true, "at most 128 bytes"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateFilename(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.in)
				}
				if !strings.Contains(err.Error(), tc.errSub) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tc.errSub)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestAddAdHocNoteRejectsDuplicate(t *testing.T) {
	backend, _ := newTempBackend(t)
	req := AddAdHocNoteRequest{Filename: "2026-05-26T13-42-08-dup.md", Note: "first"}
	if _, err := backend.AddAdHocNote(context.Background(), req); err != nil {
		t.Fatalf("first add: %v", err)
	}
	_, err := backend.AddAdHocNote(context.Background(), AddAdHocNoteRequest{Filename: req.Filename, Note: "second"})
	if err == nil {
		t.Fatal("expected duplicate error")
	}
	be, ok := err.(*BackendError)
	if !ok || be.Kind != ErrAdHocNoteAlreadyExists {
		t.Fatalf("error = %v, want AdHocNoteAlreadyExists", err)
	}
}

func TestAddAdHocNoteRejectsEmptyNote(t *testing.T) {
	backend, _ := newTempBackend(t)
	_, err := backend.AddAdHocNote(context.Background(), AddAdHocNoteRequest{Filename: "2026-05-26T13-42-08-x.md", Note: "   "})
	be, ok := err.(*BackendError)
	if !ok || be.Kind != ErrEmptyAdHocNote {
		t.Fatalf("error = %v, want EmptyAdHocNote", err)
	}
}

func TestReadInvalidLineOffset(t *testing.T) {
	backend, root := newTempBackend(t)
	writeMemoryFile(t, root, "MEMORY.md", "a\nb\n")
	_, err := backend.Read(context.Background(), ReadRequest{Path: "MEMORY.md", LineOffset: 0})
	be, ok := err.(*BackendError)
	if !ok || be.Kind != ErrInvalidLineOffset {
		t.Fatalf("error = %v, want InvalidLineOffset", err)
	}
}

func TestReadLineOffsetExceedsLength(t *testing.T) {
	backend, root := newTempBackend(t)
	writeMemoryFile(t, root, "MEMORY.md", "a\nb\n")
	_, err := backend.Read(context.Background(), ReadRequest{Path: "MEMORY.md", LineOffset: 99})
	be, ok := err.(*BackendError)
	if !ok || be.Kind != ErrLineOffsetExceedsFileLength {
		t.Fatalf("error = %v, want LineOffsetExceedsFileLength", err)
	}
}

func TestReadNotFound(t *testing.T) {
	backend, _ := newTempBackend(t)
	_, err := backend.Read(context.Background(), ReadRequest{Path: "missing.md", LineOffset: 1})
	be, ok := err.(*BackendError)
	if !ok || be.Kind != ErrNotFound {
		t.Fatalf("error = %v, want NotFound", err)
	}
}

func TestReadNotFile(t *testing.T) {
	backend, root := newTempBackend(t)
	writeMemoryFile(t, root, "skills/SKILL.md", "x")
	_, err := backend.Read(context.Background(), ReadRequest{Path: "skills", LineOffset: 1})
	be, ok := err.(*BackendError)
	if !ok || be.Kind != ErrNotFile {
		t.Fatalf("error = %v, want NotFile", err)
	}
}

func TestSearchNormalizedAndCaseInsensitive(t *testing.T) {
	backend, root := newTempBackend(t)
	writeMemoryFile(t, root, "MEMORY.md", "The Alpha-Beta value\n")

	resp, err := backend.Search(context.Background(), SearchRequest{
		Queries:       []string{"alphabeta"},
		MatchMode:     AnyMode(),
		CaseSensitive: false,
		Normalized:    true,
		MaxResults:    MaxSearchResults,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(resp.Matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(resp.Matches))
	}
	if resp.Matches[0].Content != "The Alpha-Beta value" {
		t.Fatalf("content = %q", resp.Matches[0].Content)
	}
}

func TestSearchEmptyQueryRejected(t *testing.T) {
	backend, root := newTempBackend(t)
	writeMemoryFile(t, root, "MEMORY.md", "x")
	_, err := backend.Search(context.Background(), SearchRequest{
		Queries:    []string{"  "},
		MatchMode:  AnyMode(),
		MaxResults: MaxSearchResults,
	})
	be, ok := err.(*BackendError)
	if !ok || be.Kind != ErrEmptyQuery {
		t.Fatalf("error = %v, want EmptyQuery", err)
	}
}

func TestSearchInvalidMatchWindow(t *testing.T) {
	backend, root := newTempBackend(t)
	writeMemoryFile(t, root, "MEMORY.md", "x")
	_, err := backend.Search(context.Background(), SearchRequest{
		Queries:    []string{"x"},
		MatchMode:  AllWithinLinesMode(0),
		MaxResults: MaxSearchResults,
	})
	be, ok := err.(*BackendError)
	if !ok || be.Kind != ErrInvalidMatchWindow {
		t.Fatalf("error = %v, want InvalidMatchWindow", err)
	}
}

func TestListPaginationCursor(t *testing.T) {
	backend, root := newTempBackend(t)
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		writeMemoryFile(t, root, name, "x")
	}
	resp, err := backend.List(context.Background(), ListRequest{MaxResults: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Entries) != 2 || !resp.Truncated || resp.NextCursor == nil || *resp.NextCursor != "2" {
		t.Fatalf("page1 = %#v", resp)
	}
	resp2, err := backend.List(context.Background(), ListRequest{Cursor: resp.NextCursor, MaxResults: 2})
	if err != nil {
		t.Fatalf("list page2: %v", err)
	}
	if len(resp2.Entries) != 1 || resp2.Truncated || resp2.NextCursor != nil {
		t.Fatalf("page2 = %#v", resp2)
	}
}

func TestListInvalidCursor(t *testing.T) {
	backend, root := newTempBackend(t)
	writeMemoryFile(t, root, "a.md", "x")
	_, err := backend.List(context.Background(), ListRequest{Cursor: ptr("nan"), MaxResults: 10})
	be, ok := err.(*BackendError)
	if !ok || be.Kind != ErrInvalidCursor {
		t.Fatalf("error = %v, want InvalidCursor", err)
	}
}

func TestResolveScopedPathHiddenComponentNotFound(t *testing.T) {
	backend, root := newTempBackend(t)
	writeMemoryFile(t, root, ".secret/file.md", "x")
	_, err := backend.resolveScopedPath(ptr(".secret/file.md"))
	be, ok := err.(*BackendError)
	if !ok || be.Kind != ErrNotFound {
		t.Fatalf("error = %v, want NotFound", err)
	}
}

func TestReadTruncatesToTokenBudget(t *testing.T) {
	backend, root := newTempBackend(t)
	big := strings.Repeat("lorem ipsum dolor sit amet ", 5000)
	writeMemoryFile(t, root, "MEMORY.md", big)
	resp, err := backend.Read(context.Background(), ReadRequest{Path: "MEMORY.md", LineOffset: 1, MaxTokens: DefaultReadMaxTokens})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !resp.Truncated {
		t.Fatal("expected truncation for oversized content")
	}
	if len(resp.Content) >= len(big) {
		t.Fatalf("content not truncated: %d >= %d", len(resp.Content), len(big))
	}
}

func TestSymlinkRejected(t *testing.T) {
	backend, root := newTempBackend(t)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	target := filepath.Join(root, "real.md")
	if err := os.WriteFile(target, []byte("data"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(root, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	_, err := backend.Read(context.Background(), ReadRequest{Path: "link.md", LineOffset: 1})
	be, ok := err.(*BackendError)
	if !ok || be.Kind != ErrInvalidPath {
		t.Fatalf("error = %v, want InvalidPath (symlink)", err)
	}
	if !strings.Contains(be.Error(), "must not be a symlink") {
		t.Fatalf("error = %q", be.Error())
	}
}
