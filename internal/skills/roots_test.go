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

// TestDefaultSkillRootsUserLayer asserts the user-layer roots render in the Rust
// order: CODEX_HOME/skills (user), HOME/.agents/skills (user),
// CODEX_HOME/skills/.system (system), followed by the always-present admin root
// from the System config layer (a writable override injected here in place of
// the unwritable /etc/codex).
func TestDefaultSkillRootsUserLayer(t *testing.T) {
	codexHome := absT(t, t.TempDir())
	homeDir := absT(t, t.TempDir())
	cwd := absT(t, t.TempDir())
	systemConfig := absT(t, t.TempDir())

	roots := DefaultSkillRoots(codexHome, &homeDir, cwd, WithSystemConfigDir(systemConfig))

	want := []string{
		codexHome.Join("skills").String(),
		homeDir.Join(".agents").Join("skills").String(),
		codexHome.Join("skills").Join(".system").String(),
		systemConfig.Join("skills").String(),
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
	if roots[0].Scope != SkillScopeUser || roots[1].Scope != SkillScopeUser ||
		roots[2].Scope != SkillScopeSystem || roots[3].Scope != SkillScopeAdmin {
		t.Errorf("scopes = %v/%v/%v/%v, want user/user/system/admin",
			roots[0].Scope, roots[1].Scope, roots[2].Scope, roots[3].Scope)
	}
}

// TestDefaultSkillRootsNilHome skips the $HOME/.agents/skills root, leaving the
// CODEX_HOME user root, the system cache, and the admin root.
func TestDefaultSkillRootsNilHome(t *testing.T) {
	codexHome := absT(t, t.TempDir())
	cwd := absT(t, t.TempDir())
	systemConfig := absT(t, t.TempDir())

	roots := DefaultSkillRoots(codexHome, nil, cwd, WithSystemConfigDir(systemConfig))
	if len(roots) != 3 {
		t.Fatalf("roots = %v, want 3 entries", rootPaths(roots))
	}
}

// TestDefaultSkillRootsProjectLayer discovers `.codexgo/skills` directories on
// the chain between the project root (marked by .git) and the cwd as Repo-scoped
// roots, emitted FIRST (cwd-closest first, mirroring the Rust
// HighestPrecedenceFirst walk: Project layers precede the User layer) and gated
// behind WithProjectLayer.
func TestDefaultSkillRootsProjectLayer(t *testing.T) {
	codexHome := absT(t, t.TempDir())
	repo := t.TempDir()
	// repo/.git marks the project root; repo/.codexgo/skills and
	// repo/sub/.codexgo/skills both exist; cwd is repo/sub/leaf.
	for _, dir := range []string{
		filepath.Join(repo, ".git"),
		filepath.Join(repo, ".codexgo", "skills"),
		filepath.Join(repo, "sub", ".codexgo", "skills"),
		filepath.Join(repo, "sub", "leaf"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	cwd := absT(t, filepath.Join(repo, "sub", "leaf"))
	repoAbs := absT(t, repo)
	systemConfig := absT(t, t.TempDir())

	roots := DefaultSkillRoots(codexHome, nil, cwd, WithProjectLayer(), WithSystemConfigDir(systemConfig))

	want := []string{
		// Project `.codexgo/skills` roots come first, cwd-closest first.
		repoAbs.Join("sub").Join(".codexgo").Join("skills").String(),
		repoAbs.Join(".codexgo").Join("skills").String(),
		// Then the user layer.
		codexHome.Join("skills").String(),
		codexHome.Join("skills").Join(".system").String(),
		// Then the admin root.
		systemConfig.Join("skills").String(),
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
	if roots[0].Scope != SkillScopeRepo || roots[1].Scope != SkillScopeRepo {
		t.Errorf("project scopes = %v/%v, want repo/repo", roots[0].Scope, roots[1].Scope)
	}
}

// TestDefaultSkillRootsProjectLayerDisabled omits the `.codexgo/skills` roots when
// WithProjectLayer is not supplied (the default CLI host gates project loading
// on git-trust, which codexgo carries as opaque).
func TestDefaultSkillRootsProjectLayerDisabled(t *testing.T) {
	codexHome := absT(t, t.TempDir())
	repo := t.TempDir()
	for _, dir := range []string{
		filepath.Join(repo, ".git"),
		filepath.Join(repo, ".codexgo", "skills"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	cwd := absT(t, repo)

	roots := DefaultSkillRoots(codexHome, nil, cwd)
	for _, r := range roots {
		if r.Scope == SkillScopeRepo && filepath.Base(filepath.Dir(r.Path.String())) == ".codexgo" {
			t.Errorf("project root leaked without WithProjectLayer: %q", r.Path.String())
		}
	}
}

// TestDefaultSkillRootsAdminLayer appends `<systemConfigDir>/skills` as an
// Admin-scoped root LAST (mirroring the System config layer being lowest
// precedence in the HighestPrecedenceFirst walk), via WithSystemConfigDir.
func TestDefaultSkillRootsAdminLayer(t *testing.T) {
	codexHome := absT(t, t.TempDir())
	cwd := absT(t, t.TempDir())
	systemConfig := absT(t, t.TempDir())

	roots := DefaultSkillRoots(codexHome, nil, cwd, WithSystemConfigDir(systemConfig))

	want := []string{
		codexHome.Join("skills").String(),
		codexHome.Join("skills").Join(".system").String(),
		systemConfig.Join("skills").String(),
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
	if roots[2].Scope != SkillScopeAdmin {
		t.Errorf("admin scope = %v, want admin", roots[2].Scope)
	}
}

// TestDefaultSkillRootsFullStack exercises all four scopes together and asserts
// the Rust HighestPrecedenceFirst order: project `.codexgo/skills` (cwd-first),
// user roots, system cache, admin `<systemConfigDir>/skills`, then the repo
// `.agents/skills` chain appended last by the outer assembly.
func TestDefaultSkillRootsFullStack(t *testing.T) {
	codexHome := absT(t, t.TempDir())
	homeDir := absT(t, t.TempDir())
	systemConfig := absT(t, t.TempDir())
	repo := t.TempDir()
	for _, dir := range []string{
		filepath.Join(repo, ".git"),
		filepath.Join(repo, ".codexgo", "skills"),
		filepath.Join(repo, ".agents", "skills"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	cwd := absT(t, repo)
	repoAbs := absT(t, repo)

	roots := DefaultSkillRoots(codexHome, &homeDir, cwd,
		WithProjectLayer(), WithSystemConfigDir(systemConfig))

	want := []string{
		repoAbs.Join(".codexgo").Join("skills").String(),  // project
		codexHome.Join("skills").String(),                 // user (deprecated)
		homeDir.Join(".agents").Join("skills").String(),   // user-installed
		codexHome.Join("skills").Join(".system").String(), // system cache
		systemConfig.Join("skills").String(),              // admin
		repoAbs.Join(".agents").Join("skills").String(),   // repo agents chain
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
	systemConfig := absT(t, t.TempDir())

	roots := DefaultSkillRoots(codexHome, nil, cwd, WithSystemConfigDir(systemConfig))

	// Derive expectations from the unresolved repo path (matching cwd's
	// ancestors) so symlinked temp dirs (macOS /var -> /private/var) compare
	// consistently.
	repoAbs := absT(t, repo)
	want := []string{
		codexHome.Join("skills").String(),
		codexHome.Join("skills").Join(".system").String(),
		systemConfig.Join("skills").String(),
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
	// roots[3:] are the repo `.agents/skills` chain (roots[2] is the admin root).
	for _, r := range roots[3:] {
		if r.Scope != SkillScopeRepo {
			t.Errorf("repo chain scope = %v, want repo", r.Scope)
		}
	}
}
