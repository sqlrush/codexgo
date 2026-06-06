package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCuratedTransport scripts the three transports' results to exercise the
// fallback chain in SyncOpenAIPluginsRepoWithTransport.
type fakeCuratedTransport struct {
	gitSHA, gitErr       string
	httpSHA, httpErr     string
	archiveSHA, archErr  string
	gitCalls, httpCalls  int
	archiveCalls         int
	writeSnapshotOnHTTP  bool
	writeSnapshotOnGit   bool
	writeSnapshotArchive bool
}

func (f *fakeCuratedTransport) SyncViaGit(codexHome string) (string, error) {
	f.gitCalls++
	if f.gitErr != "" {
		return "", errors.New(f.gitErr)
	}
	if f.writeSnapshotOnGit {
		writeCuratedSnapshot(nil, codexHome, f.gitSHA)
	}
	return f.gitSHA, nil
}

func (f *fakeCuratedTransport) SyncViaHTTP(codexHome string) (string, error) {
	f.httpCalls++
	if f.httpErr != "" {
		return "", errors.New(f.httpErr)
	}
	if f.writeSnapshotOnHTTP {
		writeCuratedSnapshot(nil, codexHome, f.httpSHA)
	}
	return f.httpSHA, nil
}

func (f *fakeCuratedTransport) SyncViaBackupArchive(codexHome string) (string, error) {
	f.archiveCalls++
	if f.archErr != "" {
		return "", errors.New(f.archErr)
	}
	if f.writeSnapshotArchive {
		writeCuratedSnapshot(nil, codexHome, f.archiveSHA)
	}
	return f.archiveSHA, nil
}

// writeCuratedSnapshot materializes a curated snapshot (manifest + recorded sha)
// under codexHome so HasLocalCuratedPluginsSnapshot reports true. t may be nil
// (used from transport fakes) — failures panic in that case.
func writeCuratedSnapshot(t *testing.T, codexHome, sha string) {
	if t != nil {
		t.Helper()
	}
	manifest := filepath.Join(CuratedPluginsRepoPath(codexHome), curatedPluginsMarketplaceRelative)
	must(t, os.MkdirAll(filepath.Dir(manifest), 0o755))
	must(t, os.WriteFile(manifest, []byte(`{"name":"openai-curated","interface":{"displayName":"Curated"},"plugins":[]}`), 0o644))
	must(t, writeCuratedPluginsSHA(curatedPluginsSHAPath(codexHome), sha))
}

func must(t *testing.T, err error) {
	if err == nil {
		return
	}
	if t != nil {
		t.Fatalf("setup: %v", err)
	}
	panic(err)
}

func TestSyncPrefersGit(t *testing.T) {
	home := t.TempDir()
	transport := &fakeCuratedTransport{gitSHA: "sha-git"}
	sha, err := SyncOpenAIPluginsRepoWithTransport(home, transport)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if sha != "sha-git" {
		t.Fatalf("sha = %q, want sha-git", sha)
	}
	if transport.httpCalls != 0 || transport.archiveCalls != 0 {
		t.Fatalf("expected only git, got http=%d archive=%d", transport.httpCalls, transport.archiveCalls)
	}
}

func TestSyncFallsBackToHTTPWhenGitFails(t *testing.T) {
	home := t.TempDir()
	transport := &fakeCuratedTransport{gitErr: "git unavailable", httpSHA: "sha-http"}
	sha, err := SyncOpenAIPluginsRepoWithTransport(home, transport)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if sha != "sha-http" {
		t.Fatalf("sha = %q, want sha-http", sha)
	}
	if transport.gitCalls != 1 || transport.httpCalls != 1 || transport.archiveCalls != 0 {
		t.Fatalf("call counts git=%d http=%d archive=%d", transport.gitCalls, transport.httpCalls, transport.archiveCalls)
	}
}

func TestSyncFallsBackToArchiveWhenNoSnapshot(t *testing.T) {
	home := t.TempDir()
	transport := &fakeCuratedTransport{
		gitErr:               "git down",
		httpErr:              "http down",
		archiveSHA:           "export-backup",
		writeSnapshotArchive: true,
	}
	sha, err := SyncOpenAIPluginsRepoWithTransport(home, transport)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if sha != "export-backup" {
		t.Fatalf("sha = %q, want export-backup", sha)
	}
	if transport.archiveCalls != 1 {
		t.Fatalf("expected archive fallback, got %d calls", transport.archiveCalls)
	}
}

func TestSyncSkipsArchiveWhenSnapshotExists(t *testing.T) {
	home := t.TempDir()
	// A pre-existing snapshot makes an HTTP failure terminal — the lagging
	// export archive must not refresh an existing snapshot.
	writeCuratedSnapshot(t, home, "sha-old")
	transport := &fakeCuratedTransport{gitErr: "git down", httpErr: "http down"}
	_, err := SyncOpenAIPluginsRepoWithTransport(home, transport)
	if err == nil {
		t.Fatal("expected error when both git and http fail with a snapshot present")
	}
	if transport.archiveCalls != 0 {
		t.Fatalf("expected archive skipped, got %d calls", transport.archiveCalls)
	}
	if !strings.Contains(err.Error(), "export archive fallback skipped") {
		t.Fatalf("error missing skip note: %v", err)
	}
}

func TestSyncAggregatesAllTransportErrors(t *testing.T) {
	home := t.TempDir()
	transport := &fakeCuratedTransport{gitErr: "G", httpErr: "H", archErr: "A"}
	_, err := SyncOpenAIPluginsRepoWithTransport(home, transport)
	if err == nil {
		t.Fatal("expected aggregate error")
	}
	for _, want := range []string{"G", "H", "A"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestHasLocalCuratedPluginsSnapshot(t *testing.T) {
	home := t.TempDir()
	if HasLocalCuratedPluginsSnapshot(home) {
		t.Fatal("expected no snapshot on a fresh home")
	}
	writeCuratedSnapshot(t, home, "sha-1")
	if !HasLocalCuratedPluginsSnapshot(home) {
		t.Fatal("expected snapshot after materializing manifest + sha")
	}

	// Missing sha file alone is not a usable snapshot.
	must(t, os.Remove(curatedPluginsSHAPath(home)))
	if HasLocalCuratedPluginsSnapshot(home) {
		t.Fatal("expected no snapshot when sha file is missing")
	}
}

func TestReadCuratedPluginsSHA(t *testing.T) {
	home := t.TempDir()
	if _, ok := ReadCuratedPluginsSHA(home); ok {
		t.Fatal("expected no sha on a fresh home")
	}
	must(t, writeCuratedPluginsSHA(curatedPluginsSHAPath(home), "  abc123  \n"))
	sha, ok := ReadCuratedPluginsSHA(home)
	if !ok || sha != "abc123" {
		t.Fatalf("ReadCuratedPluginsSHA = (%q, %v), want (abc123, true)", sha, ok)
	}
}
