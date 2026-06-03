package gitutils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFileNormalized(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func TestApplyAddSuccess(t *testing.T) {
	dir := initRepo(t)
	diff := "diff --git a/hello.txt b/hello.txt\nnew file mode 100644\n--- /dev/null\n+++ b/hello.txt\n@@ -0,0 +1,2 @@\n+hello\n+world\n"
	res, err := ApplyGitPatch(ApplyGitRequest{Cwd: dir, Diff: diff})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", res.ExitCode, res.Stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "hello.txt")); err != nil {
		t.Fatalf("hello.txt not created: %v", err)
	}
}

func TestApplyModifyConflict(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "file.txt", "line1\nline2\nline3\n")
	commitAll(t, dir, "seed")
	writeFile(t, dir, "file.txt", "line1\nlocal2\nline3\n")

	diff := "diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -1,3 +1,3 @@\n line1\n-line2\n+remote2\n line3\n"
	res, err := ApplyGitPatch(ApplyGitRequest{Cwd: dir, Diff: diff})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit on conflict")
	}
}

func TestApplyModifyMissingIndexSkipped(t *testing.T) {
	dir := initRepo(t)
	diff := "diff --git a/ghost.txt b/ghost.txt\n--- a/ghost.txt\n+++ b/ghost.txt\n@@ -1,1 +1,1 @@\n-old\n+new\n"
	res, err := ApplyGitPatch(ApplyGitRequest{Cwd: dir, Diff: diff})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit on missing index")
	}
}

func TestApplyThenRevert(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "file.txt", "orig\n")
	commitAll(t, dir, "seed")

	diff := "diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -1,1 +1,1 @@\n-orig\n+ORIG\n"
	res, err := ApplyGitPatch(ApplyGitRequest{Cwd: dir, Diff: diff})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("forward apply exit = %d (stderr: %s)", res.ExitCode, res.Stderr)
	}
	if got := readFileNormalized(t, filepath.Join(dir, "file.txt")); got != "ORIG\n" {
		t.Fatalf("after apply = %q, want ORIG", got)
	}

	revert, err := ApplyGitPatch(ApplyGitRequest{Cwd: dir, Diff: diff, Revert: true})
	if err != nil {
		t.Fatalf("revert: %v", err)
	}
	if revert.ExitCode != 0 {
		t.Fatalf("revert exit = %d (stderr: %s)", revert.ExitCode, revert.Stderr)
	}
	if got := readFileNormalized(t, filepath.Join(dir, "file.txt")); got != "orig\n" {
		t.Fatalf("after revert = %q, want orig", got)
	}
}

func TestPreflightDoesNotModify(t *testing.T) {
	dir := initRepo(t)
	diff := "diff --git a/ok.txt b/ok.txt\nnew file mode 100644\n--- /dev/null\n+++ b/ok.txt\n@@ -0,0 +1,2 @@\n+alpha\n+beta\n\n" +
		"diff --git a/ghost.txt b/ghost.txt\n--- a/ghost.txt\n+++ b/ghost.txt\n@@ -1,1 +1,1 @@\n-old\n+new\n"

	res, err := ApplyGitPatch(ApplyGitRequest{Cwd: dir, Diff: diff, Preflight: true})
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatal("preflight should report failure for partially-invalid patch")
	}
	if _, err := os.Stat(filepath.Join(dir, "ok.txt")); err == nil {
		t.Fatal("preflight must not create ok.txt")
	}
	if !strings.Contains(res.CmdForLog, "--check") {
		t.Fatalf("preflight cmd log should contain --check: %q", res.CmdForLog)
	}

	// Non-preflight path should not include --check.
	res2, err := ApplyGitPatch(ApplyGitRequest{Cwd: dir, Diff: diff})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if strings.Contains(res2.CmdForLog, "--check") {
		t.Fatalf("non-preflight cmd log should not contain --check: %q", res2.CmdForLog)
	}
}

func TestApplyNonRepoErrors(t *testing.T) {
	dir := t.TempDir()
	_, err := ApplyGitPatch(ApplyGitRequest{Cwd: dir, Diff: "diff --git a/x b/x\n"})
	if err == nil {
		t.Fatal("expected error for non-repo cwd")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStagePathsOnlyExisting(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "exists.txt", "x\n")
	commitAll(t, dir, "seed")
	// Modify the existing file so staging has an effect.
	writeFile(t, dir, "exists.txt", "y\n")

	diff := "diff --git a/exists.txt b/exists.txt\n--- a/exists.txt\n+++ b/exists.txt\n@@ -1 +1 @@\n-x\n+y\n" +
		"diff --git a/missing.txt b/missing.txt\n--- a/missing.txt\n+++ b/missing.txt\n@@ -1 +1 @@\n-a\n+b\n"
	if err := StagePaths(dir, diff); err != nil {
		t.Fatalf("StagePaths: %v", err)
	}
	staged := gitOut(t, dir, "diff", "--cached", "--name-only")
	if staged != "exists.txt" {
		t.Fatalf("staged = %q, want exists.txt only", staged)
	}
}
