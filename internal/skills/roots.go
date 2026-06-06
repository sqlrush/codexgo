package skills

// Default skill-root assembly for hosts without the full config-layer stack,
// porting the layer branches of the Rust `skill_roots_from_layer_stack_inner`
// (HighestPrecedenceFirst walk: Project layers, then User, then System) plus the
// repo `.agents/skills` chain discovery (`repo_agents_skill_roots`):
//
//   1. each existing `.codex/skills` between the project root and the cwd
//      (repo scope, cwd-closest-first) — Project config layers, gated behind
//      WithProjectLayer
//   2. `$CODEX_HOME/skills`            (user — deprecated location, kept)
//   3. `$HOME/.agents/skills`          (user-installed)
//   4. `$CODEX_HOME/skills/.system`    (embedded system skills cache)
//   5. `<systemConfigDir>/skills`      (admin scope; defaults to /etc/codex,
//      override via WithSystemConfigDir) — the System config layer
//   6. each existing `.agents/skills` between the project root and the cwd
//      (repo scope, project-root-first) — appended by the outer assembly
//
// Trust gate: the Rust loader gates Project-layer (`.codex`) discovery on
// git-trust (ProjectTrustContext). codexgo now ports that decision in
// internal/config (config.IsProjectTrusted / BuildProjectTrustContext); the CLI
// host resolves it per-cwd and opts in via WithProjectLayer when the project is
// trusted. With no opt-in (untrusted/no-entry project, or a host without a trust
// gate) the project `.codex/skills` roots are not assembled. Plugin skill roots
// are appended by the caller.

import (
	"os"
	"path/filepath"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// agentsDirName is the user/repo agents directory (`.agents`).
const agentsDirName = ".agents"

// projectConfigDirName is the project config folder (`.codex`) that the Rust
// Project config layer exposes as its config_folder().
const projectConfigDirName = ".codex"

// defaultSystemConfigDirUnix is the Unix system config folder, the parent of the
// Rust `SYSTEM_CONFIG_TOML_FILE_UNIX` (`/etc/codex/config.toml`). The admin
// skill root is `<systemConfigDir>/skills`.
const defaultSystemConfigDirUnix = "/etc/codex"

// defaultProjectRootMarkers mirrors the Rust DEFAULT_PROJECT_ROOT_MARKERS.
var defaultProjectRootMarkers = []string{".git"}

// rootsOptions holds the configurable seams for DefaultSkillRoots.
type rootsOptions struct {
	// includeProjectLayer enables Project-layer `.codex/skills` discovery. It is
	// off by default because codexgo does not yet port the git-trust gate.
	includeProjectLayer bool
	// systemConfigDir is the Unix system config folder whose `skills` child is
	// the admin root. It defaults to /etc/codex; tests inject a writable dir.
	systemConfigDir abspath.AbsolutePathBuf
	// hasSystemConfigDir distinguishes the injected zero value from the default.
	hasSystemConfigDir bool
}

// RootsOption configures DefaultSkillRoots.
type RootsOption func(*rootsOptions)

// WithProjectLayer enables Project config-layer skill roots (`.codex/skills` on
// the chain from the project root down to the cwd). The CLI host enables this
// once it has resolved git-trust for the project (config.IsProjectTrusted —
// see the package doc comment + DEVIATIONS.md).
func WithProjectLayer() RootsOption {
	return func(o *rootsOptions) { o.includeProjectLayer = true }
}

// WithSystemConfigDir overrides the Unix system config folder (default
// /etc/codex) whose `skills` child becomes the admin-scoped root. The seam lets
// tests inject a writable directory in place of the unwritable /etc/codex.
func WithSystemConfigDir(dir abspath.AbsolutePathBuf) RootsOption {
	return func(o *rootsOptions) {
		o.systemConfigDir = dir
		o.hasSystemConfigDir = true
	}
}

// DefaultSkillRoots assembles the skill roots for one session in the Rust
// HighestPrecedenceFirst order: the Project-layer `.codex/skills` roots
// (cwd-closest first, only when WithProjectLayer is set), then the user-layer
// roots (CODEX_HOME skills, $HOME/.agents/skills, the embedded-system cache),
// then the admin `<systemConfigDir>/skills` root, then the repo `.agents/skills`
// chain from the project root down to the cwd. homeDir may be nil when no home
// directory resolves. Roots are deduplicated by path, preserving first
// occurrence.
func DefaultSkillRoots(codexHome abspath.AbsolutePathBuf, homeDir *abspath.AbsolutePathBuf, cwd abspath.AbsolutePathBuf, opts ...RootsOption) []SkillRoot {
	cfg := rootsOptions{}
	for _, opt := range opts {
		opt(&cfg)
	}

	var roots []SkillRoot
	// Project config layers (highest precedence): `.codex/skills` on the chain
	// from the cwd up to the project root, cwd-closest first.
	if cfg.includeProjectLayer {
		roots = append(roots, projectConfigSkillRoots(cwd)...)
	}
	// User layer.
	roots = append(roots, SkillRoot{
		Path:  codexHome.Join(skillsDirName),
		Scope: SkillScopeUser,
	})
	if homeDir != nil {
		roots = append(roots, SkillRoot{
			Path:  homeDir.Join(agentsDirName).Join(skillsDirName),
			Scope: SkillScopeUser,
		})
	}
	roots = append(roots, SkillRoot{
		Path:  SystemCacheRootDir(codexHome),
		Scope: SkillScopeSystem,
	})
	// System config layer (lowest precedence): the admin `/etc/codex/skills`
	// root, or the injected override.
	roots = append(roots, SkillRoot{
		Path:  systemConfigDir(cfg).Join(skillsDirName),
		Scope: SkillScopeAdmin,
	})
	// Repo `.agents/skills` chain, appended last by the outer assembly.
	roots = append(roots, repoAgentsSkillRoots(cwd)...)
	return dedupeSkillRootsByPath(roots)
}

// systemConfigDir resolves the admin config folder: the injected override, or
// the Unix default /etc/codex.
func systemConfigDir(cfg rootsOptions) abspath.AbsolutePathBuf {
	if cfg.hasSystemConfigDir {
		return cfg.systemConfigDir
	}
	// /etc/codex is always absolute; the checked constructor cannot fail here.
	dir, _ := abspath.FromAbsolutePathChecked(defaultSystemConfigDirUnix)
	return dir
}

// projectConfigSkillRoots returns the existing `.codex/skills` directories on the
// chain from the cwd up to the project root, cwd-closest first. This mirrors the
// Rust HighestPrecedenceFirst walk over Project config layers, whose
// config_folder() is each ancestor's `.codex` directory. Note this is the
// reverse of repoAgentsSkillRoots' project-root-first order, because Project
// layers are highest precedence (cwd-closest layer wins).
func projectConfigSkillRoots(cwd abspath.AbsolutePathBuf) []SkillRoot {
	projectRoot := findProjectRoot(cwd, defaultProjectRootMarkers)
	dirs := dirsBetweenProjectRootAndCwd(cwd, projectRoot)
	var roots []SkillRoot
	// Walk cwd-closest first (reverse of the project-root-first chain).
	for i := len(dirs) - 1; i >= 0; i-- {
		dotCodexSkills := dirs[i].Join(projectConfigDirName).Join(skillsDirName)
		info, err := os.Stat(dotCodexSkills.String())
		if err != nil || !info.IsDir() {
			continue
		}
		roots = append(roots, SkillRoot{
			Path:  dotCodexSkills,
			Scope: SkillScopeRepo,
		})
	}
	return roots
}

// repoAgentsSkillRoots returns the existing `.agents/skills` directories on the
// chain from the project root (detected via the default markers) down to the
// cwd, project-root-first. Mirrors the Rust `repo_agents_skill_roots` over the
// local filesystem.
func repoAgentsSkillRoots(cwd abspath.AbsolutePathBuf) []SkillRoot {
	projectRoot := findProjectRoot(cwd, defaultProjectRootMarkers)
	dirs := dirsBetweenProjectRootAndCwd(cwd, projectRoot)
	var roots []SkillRoot
	for _, dir := range dirs {
		agentsSkills := dir.Join(agentsDirName).Join(skillsDirName)
		info, err := os.Stat(agentsSkills.String())
		if err != nil || !info.IsDir() {
			continue
		}
		roots = append(roots, SkillRoot{
			Path:  agentsSkills,
			Scope: SkillScopeRepo,
		})
	}
	return roots
}

// findProjectRoot walks the cwd's ancestors looking for the first directory
// containing any marker, falling back to the cwd. Mirrors the Rust
// `find_project_root`.
func findProjectRoot(cwd abspath.AbsolutePathBuf, markers []string) abspath.AbsolutePathBuf {
	if len(markers) == 0 {
		return cwd
	}
	for _, ancestor := range cwd.Ancestors() {
		for _, marker := range markers {
			if _, err := os.Stat(filepath.Join(ancestor.String(), marker)); err == nil {
				return ancestor
			}
		}
	}
	return cwd
}

// dirsBetweenProjectRootAndCwd returns the ancestor chain from the project
// root down to the cwd (inclusive). Mirrors the Rust
// `dirs_between_project_root_and_cwd`.
func dirsBetweenProjectRootAndCwd(cwd, projectRoot abspath.AbsolutePathBuf) []abspath.AbsolutePathBuf {
	var dirs []abspath.AbsolutePathBuf
	for _, dir := range cwd.Ancestors() {
		dirs = append(dirs, dir)
		if dir == projectRoot {
			break
		}
	}
	// Reverse to project-root-first order.
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}
	return dirs
}

// dedupeSkillRootsByPath drops later duplicates of the same root path. Mirrors
// the Rust `dedupe_skill_roots_by_path`.
func dedupeSkillRootsByPath(roots []SkillRoot) []SkillRoot {
	seen := make(map[string]struct{}, len(roots))
	out := make([]SkillRoot, 0, len(roots))
	for _, root := range roots {
		key := root.Path.String()
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, root)
	}
	return out
}
