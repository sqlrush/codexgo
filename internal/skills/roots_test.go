package skills

// Tests for the default skill-root assembly, mirroring the Rust
// `skill_roots_with_home_dir` user-layer branches + repo `.agents/skills`
// discovery between the project root and the cwd.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

func absT(t *testing.T, path string) abspath.AbsolutePathBuf {
	t.Helper()
	p, err := abspath.FromAbsolutePathChecked(path)
	if err != nil {
		t.Fatalf("abs %q: %v", path, err)
	}
	return p
}

func rootPaths(roots []SkillRoot) []string {
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		out = append(out, r.Path.String())
	}
	return out
}

// TestDefaultSkillRootsUserLayer asserts the three user-layer roots render in
// the Rust order: CODEX_HOME/skills (user), HOME/.agents/skills (user),
// CODEX_HOME/skills/.system (system).
func TestDefaultSkillRootsUserLayer(t *testing.T) {
	codexHome := absT(t, t.TempDir())
	homeDir := absT(t, t.TempDir())
	cwd := absT(t, t.TempDir())

	roots := DefaultSkillRoots(codexHome, &homeDir, cwd)

	want := []string{
		codexHome.Join("skills").String(),
		homeDir.Join(".agents").Join("skills").String(),
		codexHome.Join("skills").Join(".system").String(),
	}
	got := rootPaths(roots)
	if len(got) != len(want) {
		t.Fatalf("roots = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("roots[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if roots[0].Scope != SkillScopeUser || roots[1].Scope != SkillScopeUser || roots[2].Scope != SkillScopeSystem {
		t.Errorf("scopes = %v/%v/%v, want user/user/system", roots[0].Scope, roots[1].Scope, roots[2].Scope)
	}
}

// TestDefaultSkillRootsNilHome skips the $HOME/.agents/skills root.
func TestDefaultSkillRootsNilHome(t *testing.T) {
	codexHome := absT(t, t.TempDir())
	cwd := absT(t, t.TempDir())

	roots := DefaultSkillRoots(codexHome, nil, cwd)
	if len(roots) != 2 {
		t.Fatalf("roots = %v, want 2 entries", rootPaths(roots))
	}
}

// TestDefaultSkillRootsRepoAgentsChain discovers existing `.agents/skills`
// directories between the project root (marked by .git) and the cwd, ordered
// root-first, and dedupes against the user roots.
func TestDefaultSkillRootsRepoAgentsChain(t *testing.T) {
	codexHome := absT(t, t.TempDir())
	repo := t.TempDir()
	// repo/.git marks the project root; repo/.agents/skills and
	// repo/sub/.agents/skills both exist; cwd is repo/sub/leaf.
	for _, dir := range []string{
		filepath.Join(repo, ".git"),
		filepath.Join(repo, ".agents", "skills"),
		filepath.Join(repo, "sub", ".agents", "skills"),
		filepath.Join(repo, "sub", "leaf"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	cwd := absT(t, filepath.Join(repo, "sub", "leaf"))

	roots := DefaultSkillRoots(codexHome, nil, cwd)

	// Derive expectations from the unresolved repo path (matching cwd's
	// ancestors) so symlinked temp dirs (macOS /var -> /private/var) compare
	// consistently.
	repoAbs := absT(t, repo)
	want := []string{
		codexHome.Join("skills").String(),
		codexHome.Join("skills").Join(".system").String(),
		repoAbs.Join(".agents").Join("skills").String(),
		repoAbs.Join("sub").Join(".agents").Join("skills").String(),
	}
	got := rootPaths(roots)
	if len(got) != len(want) {
		t.Fatalf("roots = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("roots[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	for _, r := range roots[2:] {
		if r.Scope != SkillScopeRepo {
			t.Errorf("repo chain scope = %v, want repo", r.Scope)
		}
	}
}
