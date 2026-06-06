package plugins

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildZipball builds a GitHub-style zipball whose entries are all nested under a
// single top-level wrapper directory (mirroring GitHub's archive layout).
func buildZipball(t *testing.T, prefix string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, contents := range files {
		f, err := w.Create(prefix + "/" + name)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := f.Write([]byte(contents)); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestExtractZipballStripsTopLevelPrefix(t *testing.T) {
	dir := t.TempDir()
	zipBytes := buildZipball(t, "openai-plugins-abc123", map[string]string{
		".agents/plugins/marketplace.json": `{"name":"openai-curated"}`,
		"README.md":                        "hello",
	})
	if err := extractZipballToDir(zipBytes, dir); err != nil {
		t.Fatalf("extract: %v", err)
	}
	manifest := filepath.Join(dir, ".agents", "plugins", "marketplace.json")
	if data, err := os.ReadFile(manifest); err != nil || !strings.Contains(string(data), "openai-curated") {
		t.Fatalf("manifest not extracted to stripped path: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Fatalf("README not extracted: %v", err)
	}
	// The wrapper directory must NOT appear under the destination.
	if _, err := os.Stat(filepath.Join(dir, "openai-plugins-abc123")); !os.IsNotExist(err) {
		t.Fatalf("expected wrapper prefix stripped, stat err=%v", err)
	}
}

func TestExtractZipballRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("wrapper/../../escape.txt")
	_, _ = f.Write([]byte("nope"))
	_ = w.Close()
	// The single top-level segment is "wrapper", so the prefix is stripped to
	// "../../escape.txt", which must be rejected.
	err := extractZipballToDir(buf.Bytes(), dir)
	if err == nil {
		t.Fatal("expected traversal rejection")
	}
	if !strings.Contains(err.Error(), "escapes destination") {
		t.Fatalf("error = %v, want traversal rejection", err)
	}
}

func TestActivateCuratedRepoAtomicSwap(t *testing.T) {
	home := t.TempDir()
	repoPath := CuratedPluginsRepoPath(home)

	// First activation: no existing repo.
	staged1, err := prepareCuratedRepoParentAndTempDir(repoPath)
	if err != nil {
		t.Fatalf("prepare staged1: %v", err)
	}
	must(t, os.WriteFile(filepath.Join(staged1, "marker.txt"), []byte("v1"), 0o644))
	if err := activateCuratedRepo(repoPath, staged1); err != nil {
		t.Fatalf("activate v1: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(repoPath, "marker.txt")); string(data) != "v1" {
		t.Fatalf("expected v1 marker after first activation")
	}

	// Second activation: replaces the existing repo atomically.
	staged2, err := prepareCuratedRepoParentAndTempDir(repoPath)
	if err != nil {
		t.Fatalf("prepare staged2: %v", err)
	}
	must(t, os.WriteFile(filepath.Join(staged2, "marker.txt"), []byte("v2"), 0o644))
	if err := activateCuratedRepo(repoPath, staged2); err != nil {
		t.Fatalf("activate v2: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(repoPath, "marker.txt")); string(data) != "v2" {
		t.Fatalf("expected v2 marker after second activation")
	}
}

// TestDefaultTransportHTTPSync exercises the real HTTP transport against an
// httptest GitHub API + zipball, verifying the snapshot is materialized and the
// sha recorded.
func TestDefaultTransportHTTPSync(t *testing.T) {
	home := t.TempDir()
	const sha = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	zipBytes := buildZipball(t, "openai-plugins-"+sha, map[string]string{
		".agents/plugins/marketplace.json": `{"name":"openai-curated","interface":{"displayName":"Curated"},"plugins":[]}`,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/openai/plugins", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"default_branch":"main"}`))
	})
	mux.HandleFunc("/repos/openai/plugins/git/ref/heads/main", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":{"sha":"` + sha + `"}}`))
	})
	mux.HandleFunc("/repos/openai/plugins/zipball/"+sha, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(zipBytes)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	transport := defaultCuratedSyncTransport{
		gitBinary:           "/nonexistent-git-binary",
		apiBaseURL:          server.URL,
		backupArchiveAPIURL: server.URL + "/unused",
	}
	got, err := transport.SyncViaHTTP(home)
	if err != nil {
		t.Fatalf("SyncViaHTTP: %v", err)
	}
	if got != sha {
		t.Fatalf("sha = %q, want %q", got, sha)
	}
	if !HasLocalCuratedPluginsSnapshot(home) {
		t.Fatal("expected a curated snapshot after HTTP sync")
	}
	recorded, ok := ReadCuratedPluginsSHA(home)
	if !ok || recorded != sha {
		t.Fatalf("recorded sha = (%q, %v), want (%q, true)", recorded, ok, sha)
	}

	// A second sync with the same sha short-circuits (snapshot already current).
	got2, err := transport.SyncViaHTTP(home)
	if err != nil || got2 != sha {
		t.Fatalf("second SyncViaHTTP = (%q, %v)", got2, err)
	}
}
