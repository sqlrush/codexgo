package filesearch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile creates parent dirs and writes content to path under root.
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// createTempTree creates fileCount files named file-NNNN.txt in a temp dir.
func createTempTree(t *testing.T, fileCount int) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < fileCount; i++ {
		name := "file-" + pad4(i) + ".txt"
		writeFile(t, dir, name, "contents")
	}
	return dir
}

func pad4(i int) string {
	s := []byte("0000")
	for p := 3; p >= 0 && i > 0; p-- {
		s[p] = byte('0' + i%10)
		i /= 10
	}
	return string(s)
}

func hasPath(matches []FileMatch, rel string) bool {
	want := filepath.FromSlash(rel)
	for _, m := range matches {
		if m.Path == want {
			return true
		}
	}
	return false
}

func findPath(matches []FileMatch, rel string) (FileMatch, bool) {
	want := filepath.FromSlash(rel)
	for _, m := range matches {
		if m.Path == want {
			return m, true
		}
	}
	return FileMatch{}, false
}

func TestRunReturnsMatchesForQuery(t *testing.T) {
	dir := createTempTree(t, 40)
	res, err := Run("file-0000", []string{dir}, DefaultOptions(), nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Matches) == 0 {
		t.Fatal("expected matches")
	}
	if res.TotalMatchCount < len(res.Matches) {
		t.Errorf("total %d < shown %d", res.TotalMatchCount, len(res.Matches))
	}
	if !hasPath(res.Matches, "file-0000.txt") {
		t.Errorf("expected file-0000.txt in matches, got %+v", res.Matches)
	}
}

func TestRunReturnsDirectoryMatches(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "docs/guides/intro.md", "intro")
	writeFile(t, dir, "docs/readme.md", "readme")

	res, err := Run("guides", []string{dir}, DefaultOptions(), nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	m, ok := findPath(res.Matches, "docs/guides")
	if !ok {
		t.Fatalf("expected docs/guides directory match, got %+v", res.Matches)
	}
	if m.MatchType != MatchTypeDirectory {
		t.Errorf("MatchType = %v, want directory", m.MatchType)
	}
}

func TestRunLimitTruncates(t *testing.T) {
	dir := createTempTree(t, 50)
	opts := DefaultOptions()
	opts.Limit = 5
	res, err := Run("file", []string{dir}, opts, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Matches) != 5 {
		t.Errorf("len(Matches) = %d, want 5", len(res.Matches))
	}
	if res.TotalMatchCount <= 5 {
		t.Errorf("TotalMatchCount = %d, want > 5", res.TotalMatchCount)
	}
}

func TestRunComputeIndices(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "alpha.txt", "a")

	opts := DefaultOptions()
	opts.ComputeIndices = true
	res, err := Run("alpha", []string{dir}, opts, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	m, ok := findPath(res.Matches, "alpha.txt")
	if !ok {
		t.Fatal("expected alpha.txt")
	}
	if m.Indices == nil {
		t.Fatal("expected non-nil indices when ComputeIndices set")
	}
	wantIdx := []int{0, 1, 2, 3, 4}
	if len(m.Indices) != len(wantIdx) {
		t.Fatalf("indices = %v, want %v", m.Indices, wantIdx)
	}
	for i := range wantIdx {
		if m.Indices[i] != wantIdx[i] {
			t.Fatalf("indices = %v, want %v", m.Indices, wantIdx)
		}
	}

	// Without ComputeIndices, indices must be nil.
	res2, _ := Run("alpha", []string{dir}, DefaultOptions(), nil)
	m2, _ := findPath(res2.Matches, "alpha.txt")
	if m2.Indices != nil {
		t.Errorf("expected nil indices, got %v", m2.Indices)
	}
}

func TestRunCancelReturnsEmpty(t *testing.T) {
	dir := createTempTree(t, 200)
	res, err := Run("file-", []string{dir}, DefaultOptions(), func() bool { return true })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Matches) != 0 || res.TotalMatchCount != 0 {
		t.Errorf("expected empty results after cancel, got %+v", res)
	}
}

func TestRunRequiresRoot(t *testing.T) {
	_, err := Run("x", nil, DefaultOptions(), nil)
	if err == nil {
		t.Fatal("expected error for missing root")
	}
}

func TestRunExclude(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "keep.txt", "k")
	writeFile(t, dir, "skip.txt", "s")

	opts := DefaultOptions()
	opts.Exclude = []string{"skip.txt"}
	res, err := Run("txt", []string{dir}, opts, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasPath(res.Matches, "keep.txt") {
		t.Error("expected keep.txt")
	}
	if hasPath(res.Matches, "skip.txt") {
		t.Error("skip.txt should be excluded")
	}
}

// Regression: a parent directory's .gitignore with "*" must not suppress files
// inside a child directory when there is no git context (require_git semantics).
func TestParentGitignoreOutsideRepoDoesNotHideFiles(t *testing.T) {
	temp := t.TempDir()
	parent := filepath.Join(temp, "home")
	repo := filepath.Join(parent, "repo")
	writeFile(t, parent, ".gitignore", "*\n!.gitignore\n")
	writeFile(t, repo, ".gitignore", ".vscode/*\n!.vscode/\n!.vscode/settings.json\n!package.json\n")
	writeFile(t, repo, "package.json", "{}")
	writeFile(t, repo, ".vscode/settings.json", "{}")

	res, err := Run("package", []string{repo}, DefaultOptions(), nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasPath(res.Matches, "package.json") {
		t.Errorf("expected package.json (no git context => gitignore ignored), got %+v", res.Matches)
	}

	res2, _ := Run("settings", []string{repo}, DefaultOptions(), nil)
	if !hasPath(res2.Matches, ".vscode/settings.json") {
		t.Errorf("expected .vscode/settings.json, got %+v", res2.Matches)
	}
}

// When a git context exists, local .gitignore rules are respected.
func TestGitRepoRespectsLocalGitignore(t *testing.T) {
	temp := t.TempDir()
	repo := filepath.Join(temp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	writeFile(t, repo, ".gitignore", ".vscode/*\n!.vscode/\n!.vscode/settings.json\n!package.json\n")
	writeFile(t, repo, "package.json", "{}")
	writeFile(t, repo, ".vscode/settings.json", "{}")
	writeFile(t, repo, ".vscode/extensions.json", "{}")

	pkg, _ := Run("package", []string{repo}, DefaultOptions(), nil)
	if !hasPath(pkg.Matches, "package.json") {
		t.Errorf("expected package.json, got %+v", pkg.Matches)
	}

	ext, _ := Run("extensions.json", []string{repo}, DefaultOptions(), nil)
	if hasPath(ext.Matches, ".vscode/extensions.json") {
		t.Errorf("expected .vscode/extensions.json to be ignored, got %+v", ext.Matches)
	}

	set, _ := Run("settings.json", []string{repo}, DefaultOptions(), nil)
	if !hasPath(set.Matches, ".vscode/settings.json") {
		t.Errorf("expected whitelisted .vscode/settings.json, got %+v", set.Matches)
	}
}

func TestRespectGitignoreFalseDisablesIgnore(t *testing.T) {
	temp := t.TempDir()
	repo := filepath.Join(temp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	writeFile(t, repo, ".gitignore", "secret.txt\n")
	writeFile(t, repo, "secret.txt", "x")

	// With gitignore respected, the file is hidden.
	on, _ := Run("secret", []string{repo}, DefaultOptions(), nil)
	if hasPath(on.Matches, "secret.txt") {
		t.Error("secret.txt should be ignored when RespectGitignore=true")
	}

	// Disabled: the file shows up.
	opts := DefaultOptions()
	opts.RespectGitignore = false
	off, _ := Run("secret", []string{repo}, opts, nil)
	if !hasPath(off.Matches, "secret.txt") {
		t.Error("secret.txt should appear when RespectGitignore=false")
	}
}

func TestRunRankingBestFirst(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.go", "c")      // strong prefix match for "config"
	writeFile(t, dir, "x/oldconfig.go", "o") // weaker, non-prefix
	res, err := Run("config", []string{dir}, DefaultOptions(), nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Matches) < 2 {
		t.Fatalf("expected >= 2 matches, got %+v", res.Matches)
	}
	if res.Matches[0].Score < res.Matches[len(res.Matches)-1].Score {
		t.Error("matches not sorted by descending score")
	}
	if res.Matches[0].Path != "config.go" {
		t.Errorf("best match = %q, want config.go", res.Matches[0].Path)
	}
}

func TestRunTieBreakByPath(t *testing.T) {
	dir := t.TempDir()
	// Two equal-length, equal-position matches => same score, sorted by path.
	writeFile(t, dir, "b.txt", "b")
	writeFile(t, dir, "a.txt", "a")
	res, _ := Run("txt", []string{dir}, DefaultOptions(), nil)
	var paths []string
	for _, m := range res.Matches {
		if m.Path == "a.txt" || m.Path == "b.txt" {
			paths = append(paths, m.Path)
		}
	}
	if len(paths) == 2 && paths[0] != "a.txt" {
		t.Errorf("tie not broken by ascending path: %v", paths)
	}
}

func TestFullPathAndFileName(t *testing.T) {
	m := FileMatch{Root: filepath.FromSlash("/root"), Path: filepath.FromSlash("a/b.txt")}
	if got, want := m.FullPath(), filepath.FromSlash("/root/a/b.txt"); got != want {
		t.Errorf("FullPath = %q, want %q", got, want)
	}
	tests := []struct{ in, want string }{
		{filepath.FromSlash("foo/bar.txt"), "bar.txt"},
		{"", ""},
		{"plain", "plain"},
	}
	for _, tt := range tests {
		if got := FileNameFromPath(tt.in); got != tt.want {
			t.Errorf("FileNameFromPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMultiRootDeepestAttribution(t *testing.T) {
	outer := t.TempDir()
	inner := filepath.Join(outer, "sub")
	writeFile(t, inner, "target.txt", "t")

	// inner is deeper than outer; the file reachable via both roots should be
	// attributed to inner.
	res, err := Run("target", []string{outer, inner}, DefaultOptions(), nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	m, ok := findPath(res.Matches, "target.txt")
	if !ok {
		// Via outer it would be sub/target.txt; ensure the deepest root wins.
		if _, viaOuter := findPath(res.Matches, "sub/target.txt"); viaOuter {
			t.Fatal("file attributed to shallow root; expected deepest root attribution")
		}
		t.Fatalf("target not found: %+v", res.Matches)
	}
	if filepath.Clean(m.Root) != filepath.Clean(inner) {
		t.Errorf("Root = %q, want %q", m.Root, inner)
	}
}

func TestComponentCount(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{filepath.FromSlash("/a/b/c"), 3},
		{filepath.FromSlash("/a"), 1},
		{".", 0},
	}
	for _, tt := range tests {
		if got := componentCount(tt.in); got != tt.want {
			t.Errorf("componentCount(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestInvertScoreOrdering(t *testing.T) {
	// Smaller fuzzymatch score (better) must yield a larger inverted uint32.
	better := invertScore(-99)
	worse := invertScore(5)
	if better <= worse {
		t.Errorf("invertScore(-99)=%d should exceed invertScore(5)=%d", better, worse)
	}
	if got := invertScore(2147483647); got != 0 {
		t.Errorf("invertScore(MaxInt32) = %d, want 0", got)
	}
}

func TestExcludeInvalidPattern(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "a")
	opts := DefaultOptions()
	opts.Exclude = []string{"# comment"}
	_, err := Run("a", []string{dir}, opts, nil)
	if err == nil || !strings.Contains(err.Error(), "exclude") {
		t.Errorf("expected exclude error, got %v", err)
	}
}
