package gitutils

import (
	"context"
	"strings"
	"testing"
)

func TestGitDiffToRemoteNoRemote(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "1\n")
	commitAll(t, dir, "init")

	// No remote configured -> no closest remote SHA -> nil.
	if got := GitDiffToRemoteResult(context.Background(), dir); got != nil {
		t.Fatalf("expected nil without a remote, got %+v", got)
	}
}

func TestGitDiffToRemoteNonRepo(t *testing.T) {
	t.Parallel()
	if got := GitDiffToRemoteResult(context.Background(), t.TempDir()); got != nil {
		t.Fatalf("expected nil for non-repo, got %+v", got)
	}
}

func TestGitDiffToRemoteWithRemote(t *testing.T) {
	t.Parallel()
	// Set up a bare remote and a working clone.
	root := t.TempDir()
	remote := initBareRemote(t, root)

	repo := initRepo(t)
	runGit(t, repo, "remote", "add", "origin", remote)
	writeFile(t, repo, "a.txt", "base\n")
	base := commitAll(t, repo, "base")
	runGit(t, repo, "push", "-u", "origin", "main")

	// Local change not pushed.
	writeFile(t, repo, "a.txt", "changed\n")
	writeFile(t, repo, "untracked.txt", "new\n")
	commitAll(t, repo, "local change")

	res := GitDiffToRemoteResult(context.Background(), repo)
	if res == nil {
		t.Fatal("expected a diff-to-remote result")
	}
	if res.Sha.String() != base {
		t.Fatalf("base sha = %s, want %s", res.Sha, base)
	}
	if !strings.Contains(res.Diff, "changed") {
		t.Fatalf("diff should contain committed change:\n%s", res.Diff)
	}
	if !strings.Contains(res.Diff, "untracked.txt") {
		t.Fatalf("diff should include untracked file:\n%s", res.Diff)
	}
}

func TestDefaultBranchName(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "1\n")
	commitAll(t, dir, "init")

	name, ok := DefaultBranchName(context.Background(), dir)
	if !ok {
		t.Fatal("expected a default branch")
	}
	if name != "main" {
		t.Fatalf("default branch = %q, want main", name)
	}
}

// initBareRemote creates a bare repository under root and returns its path.
func initBareRemote(t *testing.T, root string) string {
	t.Helper()
	remote := root + "/remote.git"
	runGit(t, root, "init", "--bare", "remote.git")
	return remote
}
