package gitutils

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestGitInfoJSONOmitsEmpty(t *testing.T) {
	t.Parallel()
	got, err := json.Marshal(GitInfo{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(got) != "{}" {
		t.Fatalf("empty GitInfo = %s, want {}", got)
	}

	sha := NewGitSha("abc123")
	branch := "main"
	url := "https://example.com/x.git"
	full := GitInfo{CommitHash: &sha, Branch: &branch, RepositoryURL: &url}
	gotFull, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal full: %v", err)
	}
	want := `{"commit_hash":"abc123","branch":"main","repository_url":"https://example.com/x.git"}`
	if string(gotFull) != want {
		t.Fatalf("full GitInfo = %s, want %s", gotFull, want)
	}
}

func TestGitShaTransparentJSON(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(NewGitSha("deadbeef"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `"deadbeef"` {
		t.Fatalf("GitSha JSON = %s, want \"deadbeef\"", b)
	}
	var sha GitSha
	if err := json.Unmarshal([]byte(`"feedface"`), &sha); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if sha != "feedface" {
		t.Fatalf("decoded GitSha = %q, want feedface", sha)
	}
}

func TestCollectGitInfo(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "hello\n")
	head := commitAll(t, dir, "init")
	runGit(t, dir, "remote", "add", "origin", "https://github.com/openai/codex.git")

	info := CollectGitInfo(context.Background(), dir)
	if info == nil {
		t.Fatal("CollectGitInfo returned nil for a valid repo")
	}
	if info.CommitHash == nil || info.CommitHash.String() != head {
		t.Fatalf("commit hash = %v, want %s", info.CommitHash, head)
	}
	if info.Branch == nil || *info.Branch != "main" {
		t.Fatalf("branch = %v, want main", info.Branch)
	}
	if info.RepositoryURL == nil || *info.RepositoryURL != "https://github.com/openai/codex.git" {
		t.Fatalf("repository url = %v", info.RepositoryURL)
	}
}

func TestCollectGitInfoFromSubdir(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, dir, "sub/deep/a.txt", "hello\n")
	head := commitAll(t, dir, "init")

	info := CollectGitInfo(context.Background(), filepath.Join(dir, "sub", "deep"))
	if info == nil || info.CommitHash == nil || info.CommitHash.String() != head {
		t.Fatalf("expected commit %s from subdir, got %+v", head, info)
	}
}

func TestCollectGitInfoNonRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if info := CollectGitInfo(context.Background(), dir); info != nil {
		t.Fatalf("expected nil for non-repo, got %+v", info)
	}
}

func TestCollectGitInfoDetachedHead(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "1\n")
	head := commitAll(t, dir, "c1")
	writeFile(t, dir, "a.txt", "2\n")
	commitAll(t, dir, "c2")
	runGit(t, dir, "checkout", head)

	info := CollectGitInfo(context.Background(), dir)
	if info == nil {
		t.Fatal("nil info on detached HEAD")
	}
	if info.Branch != nil {
		t.Fatalf("branch should be nil on detached HEAD, got %v", *info.Branch)
	}
	if info.CommitHash == nil || info.CommitHash.String() != head {
		t.Fatalf("commit hash = %v, want %s", info.CommitHash, head)
	}
}

func TestGetGitRepoRoot(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, dir, "sub/x.txt", "x\n")
	commitAll(t, dir, "init")

	root, ok := GetGitRepoRoot(filepath.Join(dir, "sub"))
	if !ok {
		t.Fatal("expected to find repo root")
	}
	// Resolve symlinks for macOS /var -> /private/var differences.
	if !sameDir(t, root, dir) {
		t.Fatalf("root = %q, want %q", root, dir)
	}

	if _, ok := GetGitRepoRoot(t.TempDir()); ok {
		t.Fatal("expected no repo root for empty temp dir")
	}
}

func TestGetHeadCommitHashAndBranch(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "hi\n")
	head := commitAll(t, dir, "init")

	sha, ok := GetHeadCommitHash(context.Background(), dir)
	if !ok || sha.String() != head {
		t.Fatalf("head = %v ok=%v, want %s", sha, ok, head)
	}

	branch, ok := CurrentBranchName(context.Background(), dir)
	if !ok || branch != "main" {
		t.Fatalf("branch = %q ok=%v, want main", branch, ok)
	}
}

func TestGetHasChanges(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "hi\n")
	commitAll(t, dir, "init")

	changed, ok := GetHasChanges(context.Background(), dir)
	if !ok {
		t.Fatal("has-changes failed on clean repo")
	}
	if changed {
		t.Fatal("clean repo reported changes")
	}

	writeFile(t, dir, "a.txt", "changed\n")
	changed, ok = GetHasChanges(context.Background(), dir)
	if !ok || !changed {
		t.Fatalf("dirty repo: changed=%v ok=%v, want true/true", changed, ok)
	}

	// Untracked files also count as changes.
	dir2 := initRepo(t)
	writeFile(t, dir2, "a.txt", "hi\n")
	commitAll(t, dir2, "init")
	writeFile(t, dir2, "untracked.txt", "new\n")
	changed, ok = GetHasChanges(context.Background(), dir2)
	if !ok || !changed {
		t.Fatalf("untracked: changed=%v ok=%v, want true/true", changed, ok)
	}
}

func TestRecentCommits(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "1\n")
	c1 := commitAll(t, dir, "first")
	writeFile(t, dir, "a.txt", "2\n")
	c2 := commitAll(t, dir, "second\n\nbody ignored")
	writeFile(t, dir, "a.txt", "3\n")
	c3 := commitAll(t, dir, "third")

	all := RecentCommits(context.Background(), dir, 0)
	if len(all) != 3 {
		t.Fatalf("expected 3 commits, got %d", len(all))
	}
	if all[0].Sha != c3 || all[1].Sha != c2 || all[2].Sha != c1 {
		t.Fatalf("commit order wrong: %v", []string{all[0].Sha, all[1].Sha, all[2].Sha})
	}
	if all[1].Subject != "second" {
		t.Fatalf("subject = %q, want %q (only first line)", all[1].Subject, "second")
	}
	if all[0].Timestamp == 0 {
		t.Fatal("timestamp should be non-zero")
	}

	limited := RecentCommits(context.Background(), dir, 2)
	if len(limited) != 2 {
		t.Fatalf("limit 2 returned %d commits", len(limited))
	}
	if limited[0].Sha != c3 || limited[1].Sha != c2 {
		t.Fatalf("limited order wrong")
	}
}

func TestRecentCommitsNonRepo(t *testing.T) {
	t.Parallel()
	got := RecentCommits(context.Background(), t.TempDir(), 5)
	if got == nil {
		t.Fatal("RecentCommits returned nil; expected empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d", len(got))
	}
}

func TestLocalGitBranchesDefaultFirst(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "1\n")
	commitAll(t, dir, "init")
	runGit(t, dir, "branch", "zeta")
	runGit(t, dir, "branch", "alpha")

	branches := LocalGitBranches(context.Background(), dir)
	if len(branches) != 3 {
		t.Fatalf("expected 3 branches, got %v", branches)
	}
	if branches[0] != "main" {
		t.Fatalf("default branch should be first, got %v", branches)
	}
	// Remaining branches sorted.
	if branches[1] != "alpha" || branches[2] != "zeta" {
		t.Fatalf("non-default branches should be sorted, got %v", branches)
	}
}

// sameDir reports whether two paths refer to the same directory after symlink
// resolution.
func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		return a == b
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		return a == b
	}
	return ra == rb
}
