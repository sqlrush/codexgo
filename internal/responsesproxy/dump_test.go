package responsesproxy

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// readDumpWithSuffix finds the single dump file ending in suffix and parses it.
func readDumpWithSuffix(t *testing.T, dir, suffix string) map[string]any {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var matches []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), suffix) {
			matches = append(matches, e.Name())
		}
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 file with suffix %q, got %v", suffix, matches)
	}
	data, err := os.ReadFile(filepath.Join(dir, matches[0]))
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("parsing dump %q: %v", matches[0], err)
	}
	return v
}

func TestDumpRequestWritesRedactedHeadersAndJSONBody(t *testing.T) {
	dumpDir := t.TempDir()
	dumper, err := newExchangeDumper(dumpDir)
	if err != nil {
		t.Fatal(err)
	}

	headers := []headerDump{
		newHeaderDump("Authorization", "Bearer secret"),
		newHeaderDump("Cookie", "user-session=secret"),
		newHeaderDump("Content-Type", "application/json"),
		newHeaderDump("x-codex-window-id", "thread-1:0"),
		newHeaderDump("x-codex-parent-thread-id", "parent-thread-1"),
		newHeaderDump("x-openai-subagent", "collab_spawn"),
	}

	dump, err := dumper.dumpRequest(http.MethodPost, "/v1/responses", headers, []byte(`{"model":"gpt-5.4"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(dump.responsePath, "-response.json") {
		t.Fatalf("response path %q must end with -response.json", dump.responsePath)
	}

	got := readDumpWithSuffix(t, dumpDir, "-request.json")
	want := map[string]any{
		"method": "POST",
		"url":    "/v1/responses",
		"headers": []any{
			map[string]any{"name": "Authorization", "value": "[REDACTED]"},
			map[string]any{"name": "Cookie", "value": "[REDACTED]"},
			map[string]any{"name": "Content-Type", "value": "application/json"},
			map[string]any{"name": "x-codex-window-id", "value": "thread-1:0"},
			map[string]any{"name": "x-codex-parent-thread-id", "value": "parent-thread-1"},
			map[string]any{"name": "x-openai-subagent", "value": "collab_spawn"},
		},
		"body": map[string]any{"model": "gpt-5.4"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("request dump mismatch:\n got %#v\nwant %#v", got, want)
	}
}

func TestResponseBodyDumpStreamsBodyAndWritesResponseFile(t *testing.T) {
	dumpDir := t.TempDir()
	dumper, err := newExchangeDumper(dumpDir)
	if err != nil {
		t.Fatal(err)
	}
	dump, err := dumper.dumpRequest(http.MethodPost, "/v1/responses", nil, []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}

	respHeaders := []headerDump{
		newHeaderDump("content-type", "text/event-stream"),
		newHeaderDump("authorization", "Bearer secret"),
		newHeaderDump("set-cookie", "user-session=secret"),
	}

	tee := dump.teeResponseBody(200, respHeaders, strings.NewReader("data: hello\n\n"))
	body, err := io.ReadAll(tee)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "data: hello\n\n" {
		t.Fatalf("got body %q want %q", body, "data: hello\n\n")
	}

	got := readDumpWithSuffix(t, dumpDir, "-response.json")
	want := map[string]any{
		"status": float64(200),
		"headers": []any{
			map[string]any{"name": "content-type", "value": "text/event-stream"},
			map[string]any{"name": "authorization", "value": "[REDACTED]"},
			map[string]any{"name": "set-cookie", "value": "[REDACTED]"},
		},
		"body": "data: hello\n\n",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("response dump mismatch:\n got %#v\nwant %#v", got, want)
	}
}

func TestDumpBodyNonJSONStoredAsString(t *testing.T) {
	dumpDir := t.TempDir()
	dumper, err := newExchangeDumper(dumpDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dumper.dumpRequest(http.MethodPost, "/v1/responses", nil, []byte("not json")); err != nil {
		t.Fatal(err)
	}
	got := readDumpWithSuffix(t, dumpDir, "-request.json")
	if got["body"] != "not json" {
		t.Fatalf("got body %#v want \"not json\"", got["body"])
	}
}

func TestDumpFileNamingAndSequence(t *testing.T) {
	dumpDir := t.TempDir()
	dumper, err := newExchangeDumper(dumpDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dumper.dumpRequest(http.MethodPost, "/v1/responses", nil, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if _, err := dumper.dumpRequest(http.MethodPost, "/v1/responses", nil, []byte("{}")); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dumpDir)
	if err != nil {
		t.Fatal(err)
	}
	var requestFiles []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "-request.json") {
			requestFiles = append(requestFiles, e.Name())
		}
	}
	if len(requestFiles) != 2 {
		t.Fatalf("got %d request files want 2", len(requestFiles))
	}
	// First sequence is 000001, second is 000002.
	hasSeq := func(seq string) bool {
		for _, f := range requestFiles {
			if strings.HasPrefix(f, seq+"-") {
				return true
			}
		}
		return false
	}
	if !hasSeq("000001") || !hasSeq("000002") {
		t.Fatalf("expected sequences 000001 and 000002, got %v", requestFiles)
	}
}

func TestShouldRedactHeader(t *testing.T) {
	tests := []struct {
		name string
		hdr  string
		want bool
	}{
		{name: "authorization", hdr: "Authorization", want: true},
		{name: "authorization lower", hdr: "authorization", want: true},
		{name: "cookie", hdr: "Cookie", want: true},
		{name: "set-cookie", hdr: "Set-Cookie", want: true},
		{name: "content-type", hdr: "Content-Type", want: false},
		{name: "x-header", hdr: "X-Custom", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRedactHeader(tt.hdr); got != tt.want {
				t.Fatalf("shouldRedactHeader(%q)=%v want %v", tt.hdr, got, tt.want)
			}
		})
	}
}

func TestWriteJSONDumpFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	dump := requestDump{
		Method:  "POST",
		URL:     "/v1/responses",
		Headers: []headerDump{{Name: "a", Value: "b"}},
		Body:    json.RawMessage(`{"k":"v"}`),
	}
	if err := writeJSONDump(path, dump); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Must be 2-space indented and end with a newline.
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("dump must end with newline")
	}
	if !strings.Contains(string(data), "  \"method\": \"POST\"") {
		t.Fatalf("dump must use 2-space indent, got:\n%s", data)
	}
}
