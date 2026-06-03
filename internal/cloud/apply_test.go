package cloud

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/gitutils"
)

func gitResult(exit int, applied, skipped, conflicted []string) gitutils.ApplyGitResult {
	return gitutils.ApplyGitResult{
		ExitCode:        exit,
		AppliedPaths:    applied,
		SkippedPaths:    skipped,
		ConflictedPaths: conflicted,
	}
}

func TestApplyHelpers(t *testing.T) {
	t.Parallel()

	if got := tail("hello world", 5); got != "world" {
		t.Errorf("tail = %q, want %q", got, "world")
	}
	if got := tail("short", 100); got != "short" {
		t.Errorf("tail = %q, want short", got)
	}

	tests := []struct {
		s    string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"a\n", 1},
		{"a\nb", 2},
		{"a\nb\n", 2},
	}
	for _, tt := range tests {
		if got := lineCount(tt.s); got != tt.want {
			t.Errorf("lineCount(%q) = %d, want %d", tt.s, got, tt.want)
		}
	}

	kinds := []struct {
		patch string
		want  string
	}{
		{"*** Begin Patch\nstuff", "codex-patch"},
		{"diff --git a/x b/x\n", "git-diff"},
		{"@@ -1 +1 @@\n", "unified-diff"},
		{"random text", "unknown"},
	}
	for _, tt := range kinds {
		got := summarizePatchForLogging(tt.patch)
		if !strings.Contains(got, "kind="+tt.want) {
			t.Errorf("summarize(%q) kind = %q, want kind=%s", tt.patch, got, tt.want)
		}
	}
}

func TestApplyStatusFromResultAndMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		exit int
		app  []string
		conf []string
		want ApplyStatus
	}{
		{"success", 0, nil, nil, ApplyStatusSuccess},
		{"partial_applied", 1, []string{"a"}, nil, ApplyStatusPartial},
		{"partial_conflict", 1, nil, []string{"b"}, ApplyStatusPartial},
		{"error", 1, nil, nil, ApplyStatusError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := gitResult(tt.exit, tt.app, nil, tt.conf)
			if got := applyStatusFromResult(r); got != tt.want {
				t.Errorf("applyStatusFromResult = %v, want %v", got, tt.want)
			}
			// Message strings should differ by preflight + status without panicking.
			_ = applyMessage("T1", true, applyStatusFromResult(r), r)
			_ = applyMessage("T1", false, applyStatusFromResult(r), r)
		})
	}
}

func TestApplyTaskAgainstRealRepo(t *testing.T) {
	repo := initGitRepo(t)
	t.Chdir(repo)

	diff := "diff --git a/file.txt b/file.txt\n" +
		"index 0000000..1111111 100644\n" +
		"--- a/file.txt\n" +
		"+++ b/file.txt\n" +
		"@@ -1 +1,2 @@\n" +
		" original\n" +
		"+added line\n"

	c := NewHTTPClient("https://example.test")
	ctx := context.Background()

	// Preflight should pass without modifying the file.
	pre, err := c.ApplyTaskPreflight(ctx, "T1", &diff)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if pre.Status != ApplyStatusSuccess || pre.Applied {
		t.Fatalf("preflight outcome = %+v", pre)
	}
	if data, _ := os.ReadFile(filepath.Join(repo, "file.txt")); strings.Contains(string(data), "added line") {
		t.Fatal("preflight must not modify the working tree")
	}

	// Apply should modify the file.
	out, err := c.ApplyTask(ctx, "T1", &diff)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if out.Status != ApplyStatusSuccess || !out.Applied {
		t.Fatalf("apply outcome = %+v", out)
	}
	data, _ := os.ReadFile(filepath.Join(repo, "file.txt"))
	if !strings.Contains(string(data), "added line") {
		t.Errorf("apply did not modify file, got %q", data)
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
