package skills

// Default skill-root assembly for hosts without the full config-layer stack,
// porting the user-layer branches of the Rust `skill_roots_with_home_dir` plus
// the repo `.agents/skills` chain discovery (`repo_agents_skill_roots`):
//
//   1. `$CODEX_HOME/skills`            (user — deprecated location, kept)
//   2. `$HOME/.agents/skills`          (user-installed)
//   3. `$CODEX_HOME/skills/.system`    (embedded system skills cache)
//   4. each existing `.agents/skills` between the project root and the cwd
//      (repo scope, project-root-first)
//
// STUB: the project config layer's `.codex/skills` root and the admin
// `/etc/codex/skills` root come from the config-layer stack (config area) and
// are not assembled here; plugin skill roots are appended by the caller.

import (
	"os"
	"path/filepath"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// agentsDirName is the user/repo agents directory (`.agents`).
const agentsDirName = ".agents"

// defaultProjectRootMarkers mirrors the Rust DEFAULT_PROJECT_ROOT_MARKERS.
var defaultProjectRootMarkers = []string{".git"}

// DefaultSkillRoots assembles the skill roots for one session: the user-layer
// roots (CODEX_HOME skills, $HOME/.agents/skills, the embedded-system cache)
// followed by the repo `.agents/skills` chain from the project root down to
// the cwd. homeDir may be nil when no home directory resolves. Roots are
// deduplicated by path, preserving first occurrence.
func DefaultSkillRoots(codexHome abspath.AbsolutePathBuf, homeDir *abspath.AbsolutePathBuf, cwd abspath.AbsolutePathBuf) []SkillRoot {
	var roots []SkillRoot
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
	roots = append(roots, repoAgentsSkillRoots(cwd)...)
	return dedupeSkillRootsByPath(roots)
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
