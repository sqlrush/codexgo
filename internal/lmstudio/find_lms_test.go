package lmstudio

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// withEmptyPath sets PATH to a directory guaranteed not to contain `lms` so
// exec.LookPath fails deterministically, then restores PATH afterward.
func withEmptyPath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func TestFindLMSNotFound(t *testing.T) {
	withEmptyPath(t)
	_, err := findLMSWithHomeDir(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "LM Studio not found") {
		t.Fatalf("error = %v, want LM Studio not found", err)
	}
}

func TestDownloadModelLMSNotFound(t *testing.T) {
	withEmptyPath(t)
	client := FromHostRoot("http://localhost:1234")
	err := client.DownloadModel(context.Background(), "openai/gpt-oss-20b")
	if err == nil || !strings.Contains(err.Error(), "LM Studio not found") {
		t.Fatalf("error = %v, want LM Studio not found", err)
	}
}

func TestHTTPStatusKnownAndUnknown(t *testing.T) {
	if got := httpStatus(http.StatusNotFound); got != "404 Not Found" {
		t.Errorf("httpStatus(404) = %q, want 404 Not Found", got)
	}
	// An unassigned code has no canonical text and falls back to the number.
	if got := httpStatus(799); got != "799" {
		t.Errorf("httpStatus(799) = %q, want 799", got)
	}
}
