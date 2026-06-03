package applypatch

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// wrapPatch constructs a patch with the given body, mirroring the Rust test
// helper.
func wrapPatch(body string) string {
	return fmt.Sprintf("*** Begin Patch\n%s\n*** End Patch", body)
}

// applyToTempCwd applies patch with cwd set to dir, returning stdout, stderr,
// the delta and any error.
func applyToTempCwd(t *testing.T, patch, dir string) (string, string, AppliedPatchDelta, error) {
	t.Helper()
	cwd, err := abspath.FromAbsolutePath(dir)
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}
	var stdout, stderr bytes.Buffer
	delta, applyErr := ApplyPatch(patch, cwd, &stdout, &stderr, OSFileSystem{})
	return stdout.String(), stderr.String(), delta, applyErr
}

func TestAddFileHunkCreatesFileWithContents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "add.txt")
	patch := wrapPatch(fmt.Sprintf("*** Add File: %s\n+ab\n+cd", path))

	stdout, stderr, _, err := applyToTempCwd(t, patch, dir)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	wantOut := fmt.Sprintf("Success. Updated the following files:\nA %s\n", path)
	if stdout != wantOut {
		t.Fatalf("stdout got %q, want %q", stdout, wantOut)
	}
	if stderr != "" {
		t.Fatalf("stderr got %q, want empty", stderr)
	}
	contents, _ := os.ReadFile(path)
	if string(contents) != "ab\ncd\n" {
		t.Fatalf("contents got %q, want %q", contents, "ab\ncd\n")
	}
}

func TestApplyPatchHunksAcceptRelativeAndAbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	relativeAdd := filepath.Join(dir, "relative-add.txt")
	absoluteAdd := filepath.Join(dir, "absolute-add.txt")
	relativeDelete := filepath.Join(dir, "relative-delete.txt")
	absoluteDelete := filepath.Join(dir, "absolute-delete.txt")
	relativeUpdate := filepath.Join(dir, "relative-update.txt")
	absoluteUpdate := filepath.Join(dir, "absolute-update.txt")

	mustWrite(t, relativeDelete, "delete relative\n")
	mustWrite(t, absoluteDelete, "delete absolute\n")
	mustWrite(t, relativeUpdate, "relative old\n")
	mustWrite(t, absoluteUpdate, "absolute old\n")

	body := fmt.Sprintf("*** Add File: relative-add.txt\n"+
		"+relative add\n"+
		"*** Add File: %s\n"+
		"+absolute add\n"+
		"*** Delete File: relative-delete.txt\n"+
		"*** Delete File: %s\n"+
		"*** Update File: relative-update.txt\n"+
		"@@\n"+
		"-relative old\n"+
		"+relative new\n"+
		"*** Update File: %s\n"+
		"@@\n"+
		"-absolute old\n"+
		"+absolute new",
		absoluteAdd, absoluteDelete, absoluteUpdate)
	patch := wrapPatch(body)

	stdout, stderr, _, err := applyToTempCwd(t, patch, dir)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	assertFile(t, relativeAdd, "relative add\n")
	assertFile(t, absoluteAdd, "absolute add\n")
	assertNotExist(t, relativeDelete)
	assertNotExist(t, absoluteDelete)
	assertFile(t, relativeUpdate, "relative new\n")
	assertFile(t, absoluteUpdate, "absolute new\n")
	if stderr != "" {
		t.Fatalf("stderr got %q, want empty", stderr)
	}
	wantOut := fmt.Sprintf(
		"Success. Updated the following files:\nA relative-add.txt\nA %s\nM relative-update.txt\nM %s\nD relative-delete.txt\nD %s\n",
		absoluteAdd, absoluteUpdate, absoluteDelete)
	if stdout != wantOut {
		t.Fatalf("stdout got %q, want %q", stdout, wantOut)
	}
}

func TestDeleteFileHunkRemovesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "del.txt")
	mustWrite(t, path, "x")
	patch := wrapPatch(fmt.Sprintf("*** Delete File: %s", path))

	stdout, stderr, _, err := applyToTempCwd(t, patch, dir)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	wantOut := fmt.Sprintf("Success. Updated the following files:\nD %s\n", path)
	if stdout != wantOut {
		t.Fatalf("stdout got %q, want %q", stdout, wantOut)
	}
	if stderr != "" {
		t.Fatalf("stderr got %q, want empty", stderr)
	}
	assertNotExist(t, path)
}

func TestUpdateFileHunkModifiesContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update.txt")
	mustWrite(t, path, "foo\nbar\n")
	patch := wrapPatch(fmt.Sprintf("*** Update File: %s\n@@\n foo\n-bar\n+baz", path))

	stdout, stderr, _, err := applyToTempCwd(t, patch, dir)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	wantOut := fmt.Sprintf("Success. Updated the following files:\nM %s\n", path)
	if stdout != wantOut {
		t.Fatalf("stdout got %q, want %q", stdout, wantOut)
	}
	if stderr != "" {
		t.Fatalf("stderr got %q, want empty", stderr)
	}
	assertFile(t, path, "foo\nbaz\n")
}

func TestUpdateFileHunkCanMoveFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dest := filepath.Join(dir, "dst.txt")
	mustWrite(t, src, "line\n")
	patch := wrapPatch(fmt.Sprintf("*** Update File: %s\n*** Move to: %s\n@@\n-line\n+line2", src, dest))

	stdout, stderr, _, err := applyToTempCwd(t, patch, dir)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	wantOut := fmt.Sprintf("Success. Updated the following files:\nM %s\n", dest)
	if stdout != wantOut {
		t.Fatalf("stdout got %q, want %q", stdout, wantOut)
	}
	if stderr != "" {
		t.Fatalf("stderr got %q, want empty", stderr)
	}
	assertNotExist(t, src)
	assertFile(t, dest, "line2\n")
}

func TestFailedMoveReturnsCommittedDestinationDelta(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "locked")
	destDir := filepath.Join(dir, "out")
	mustMkdir(t, sourceDir)
	mustMkdir(t, destDir)
	src := filepath.Join(sourceDir, "src.txt")
	dest := filepath.Join(destDir, "dst.txt")
	mustWrite(t, src, "line\n")
	if err := os.Chmod(sourceDir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(sourceDir, 0o755) })

	patch := wrapPatch("*** Update File: locked/src.txt\n*** Move to: out/dst.txt\n@@\n-line\n+line2")
	_, stderr, _, err := applyToTempCwd(t, patch, dir)
	if err == nil {
		t.Fatalf("expected error from source removal")
	}
	failure, ok := err.(*ApplyFailure)
	if !ok {
		t.Fatalf("expected *ApplyFailure, got %T", err)
	}

	if err := os.Chmod(sourceDir, 0o755); err != nil {
		t.Fatalf("chmod restore: %v", err)
	}

	wantStderrSubstr := fmt.Sprintf("Failed to remove original %s", src)
	if !strings.Contains(stderr, wantStderrSubstr) {
		t.Fatalf("stderr %q missing %q", stderr, wantStderrSubstr)
	}

	wantDelta := newAppliedPatchDelta([]AppliedPatchChange{{
		Path: dest,
		Change: AppliedPatchFileChange{
			Kind:    AppliedAdd,
			Content: "line2\n",
		},
	}}, true)
	if !deltasEqual(failure.Delta, wantDelta) {
		t.Fatalf("delta got %#v, want %#v", failure.Delta, wantDelta)
	}
	assertFile(t, src, "line\n")
	assertFile(t, dest, "line2\n")
}

func TestMultipleUpdateChunksApplyToSingleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "multi.txt")
	mustWrite(t, path, "foo\nbar\nbaz\nqux\n")
	patch := wrapPatch(fmt.Sprintf("*** Update File: %s\n@@\n foo\n-bar\n+BAR\n@@\n baz\n-qux\n+QUX", path))

	stdout, stderr, _, err := applyToTempCwd(t, patch, dir)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	wantOut := fmt.Sprintf("Success. Updated the following files:\nM %s\n", path)
	if stdout != wantOut {
		t.Fatalf("stdout got %q, want %q", stdout, wantOut)
	}
	if stderr != "" {
		t.Fatalf("stderr got %q, want empty", stderr)
	}
	assertFile(t, path, "foo\nBAR\nbaz\nQUX\n")
}

func TestUpdateFileHunkInterleavedChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interleaved.txt")
	mustWrite(t, path, "a\nb\nc\nd\ne\nf\n")
	body := fmt.Sprintf("*** Update File: %s\n"+
		"@@\n a\n-b\n+B\n"+
		"@@\n c\n d\n-e\n+E\n"+
		"@@\n f\n+g\n"+
		"*** End of File", path)
	patch := wrapPatch(body)

	stdout, stderr, _, err := applyToTempCwd(t, patch, dir)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	wantOut := fmt.Sprintf("Success. Updated the following files:\nM %s\n", path)
	if stdout != wantOut {
		t.Fatalf("stdout got %q, want %q", stdout, wantOut)
	}
	if stderr != "" {
		t.Fatalf("stderr got %q, want empty", stderr)
	}
	assertFile(t, path, "a\nB\nc\nd\nE\nf\ng\n")
}

func TestPureAdditionChunkFollowedByRemoval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panic.txt")
	mustWrite(t, path, "line1\nline2\nline3\n")
	body := fmt.Sprintf("*** Update File: %s\n"+
		"@@\n+after-context\n+second-line\n"+
		"@@\n line1\n-line2\n-line3\n+line2-replacement", path)
	patch := wrapPatch(body)

	_, _, _, err := applyToTempCwd(t, patch, dir)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	assertFile(t, path, "line1\nline2-replacement\nafter-context\nsecond-line\n")
}

func TestUpdateLineWithUnicodeDash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unicode.py")
	// EN DASH (U+2013) and NON-BREAKING HYPHEN (U+2011) in the original.
	original := "import asyncio  # local import – avoids top‑level dep\n"
	mustWrite(t, path, original)

	body := fmt.Sprintf("*** Update File: %s\n"+
		"@@\n"+
		"-import asyncio  # local import - avoids top-level dep\n"+
		"+import asyncio  # HELLO", path)
	patch := wrapPatch(body)

	stdout, stderr, _, err := applyToTempCwd(t, patch, dir)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	assertFile(t, path, "import asyncio  # HELLO\n")
	wantOut := fmt.Sprintf("Success. Updated the following files:\nM %s\n", path)
	if stdout != wantOut {
		t.Fatalf("stdout got %q, want %q", stdout, wantOut)
	}
	if stderr != "" {
		t.Fatalf("stderr got %q, want empty", stderr)
	}
}

func TestApplyPatchFailsOnWriteError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}
	dir := t.TempDir()
	lockedDir := filepath.Join(dir, "locked")
	mustMkdir(t, lockedDir)
	if err := os.Chmod(lockedDir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(lockedDir, 0o755) })

	patch := wrapPatch("*** Add File: locked/new.txt\n+after")
	_, _, _, err := applyToTempCwd(t, patch, dir)
	if err := os.Chmod(lockedDir, 0o755); err != nil {
		t.Fatalf("chmod restore: %v", err)
	}
	failure, ok := err.(*ApplyFailure)
	if !ok {
		t.Fatalf("expected *ApplyFailure, got %T (%v)", err, err)
	}
	if failure.Delta.IsExact() {
		t.Fatalf("expected inexact delta after failed write")
	}
}

func TestUnreadableDestinationsReturnInexactDelta(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binary.dat")
	mustWrite(t, filepath.Join(dir, "source.txt"), "before\n")

	patches := []string{
		wrapPatch("*** Add File: binary.dat\n+text"),
		wrapPatch("*** Update File: source.txt\n*** Move to: binary.dat\n@@\n-before\n+after"),
	}
	for _, patch := range patches {
		// Write non-UTF-8 bytes so reading the destination fails the UTF-8 read,
		// matching the Rust scenario.
		if err := os.WriteFile(path, []byte{0xff, 0xfe, 0xfd}, 0o644); err != nil {
			t.Fatalf("write binary: %v", err)
		}
		_, _, delta, err := applyToTempCwd(t, patch, dir)
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
		if delta.IsExact() {
			t.Fatalf("expected inexact delta for patch %q", patch)
		}
	}
}

func TestDeleteSymlinkReturnsInexactDelta(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	link := filepath.Join(dir, "link.txt")
	mustWrite(t, target, "target\n")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	patch := wrapPatch("*** Delete File: link.txt")

	_, _, delta, err := applyToTempCwd(t, patch, dir)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if delta.IsExact() {
		t.Fatalf("expected inexact delta for symlink delete")
	}
}

// --- helpers ---

func mustWrite(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("file %s got %q, want %q", path, got, want)
	}
}

func assertNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err == nil {
		t.Fatalf("expected %s to not exist", path)
	}
}

func deltasEqual(a, b AppliedPatchDelta) bool {
	if a.exact != b.exact || len(a.changes) != len(b.changes) {
		return false
	}
	for i := range a.changes {
		if a.changes[i] != b.changes[i] {
			return false
		}
	}
	return true
}
