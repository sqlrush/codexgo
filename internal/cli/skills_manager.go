package cli

// assemblySkillsManager is the headless host glue for skills: it installs the
// embedded system skills under CODEX_HOME (via skills.NewSkillsManager), and
// renders the `<skills_instructions>` developer section for a new thread's
// initial context — the include_skill_instructions block of codex's
// build_initial_context. It implements core.SkillsManager (the per-turn
// opaque-outcome surface) and core.InitialSkillsRenderer.

import (
	"context"
	"os"

	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/skills"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// skillsInstructionsOpenTag / skillsInstructionsCloseTag mirror the Rust
// SKILLS_INSTRUCTIONS_OPEN_TAG / _CLOSE_TAG protocol constants.
const (
	skillsInstructionsOpenTag  = "<skills_instructions>"
	skillsInstructionsCloseTag = "</skills_instructions>"
)

// assemblySkillsManager loads skills from the default roots for each cwd.
type assemblySkillsManager struct {
	codexHome abspath.AbsolutePathBuf
	manager   *skills.SkillsManager
}

var (
	_ core.SkillsManager         = (*assemblySkillsManager)(nil)
	_ core.InitialSkillsRenderer = (*assemblySkillsManager)(nil)
)

// newAssemblySkillsManager builds the manager with bundled (system) skills
// enabled, codex's default.
func newAssemblySkillsManager(codexHome string) (*assemblySkillsManager, error) {
	return newAssemblySkillsManagerWithBundled(codexHome, true)
}

// newAssemblySkillsManagerWithBundled builds the manager with explicit bundled
// enablement (config `bundled_skills_enabled`).
func newAssemblySkillsManagerWithBundled(codexHome string, bundledEnabled bool) (*assemblySkillsManager, error) {
	home, err := abspath.FromAbsolutePathChecked(codexHome)
	if err != nil {
		return nil, err
	}
	return &assemblySkillsManager{
		codexHome: home,
		manager:   skills.NewSkillsManager(home, bundledEnabled),
	}, nil
}

// SkillsForTurn loads the skill outcome for the turn's cwd, returned opaque for
// the turn context (core does not interpret it).
func (m *assemblySkillsManager) SkillsForTurn(ctx context.Context, tc *core.TurnContext) (any, error) {
	cwd := ""
	if tc != nil {
		cwd = tc.Cwd
	}
	outcome, ok := m.loadOutcome(ctx, cwd)
	if !ok {
		return nil, nil
	}
	return outcome, nil
}

// RenderInitialSkillsInstructions scans the default skill roots for cwd and
// renders the tagged skills_instructions text under the model's metadata
// budget. ok=false when no skills are eligible.
func (m *assemblySkillsManager) RenderInitialSkillsInstructions(ctx context.Context, cwd string, contextWindow *int64) (string, bool) {
	outcome, ok := m.loadOutcome(ctx, cwd)
	if !ok {
		return "", false
	}
	available := skills.BuildAvailableSkills(&outcome, skills.DefaultSkillMetadataBudget(contextWindow))
	if available == nil {
		return "", false
	}
	body := skills.RenderAvailableSkillsBody(available.SkillRootLines, available.SkillLines)
	return skillsInstructionsOpenTag + body + skillsInstructionsCloseTag, true
}

// loadOutcome assembles the default roots for cwd and loads the skills.
//
// The admin (System config layer) root `/etc/codex/skills` is included by
// default, mirroring the reference binary, which always carries a System config
// layer. The Project config-layer roots (`.codex/skills`) are intentionally NOT
// enabled (WithProjectLayer is omitted): the reference loader gates them on
// git-trust, which this headless host does not yet resolve (STUB — see
// skills.DefaultSkillRoots doc + DEVIATIONS.md). Both binaries on the same host
// therefore agree, since neither loads project skills here.
func (m *assemblySkillsManager) loadOutcome(ctx context.Context, cwd string) (skills.SkillLoadOutcome, bool) {
	cwdPath, err := abspath.FromAbsolutePathChecked(cwd)
	if err != nil {
		return skills.SkillLoadOutcome{}, false
	}
	var homeDir *abspath.AbsolutePathBuf
	if dir, err := os.UserHomeDir(); err == nil {
		if p, perr := abspath.FromAbsolutePathChecked(dir); perr == nil {
			homeDir = &p
		}
	}
	roots := skills.DefaultSkillRoots(m.codexHome, homeDir, cwdPath)
	outcome := m.manager.LoadSkills(ctx, cwdPath, roots, skills.SkillConfigRules{}, true /* useCache */, false /* forceReload */)
	return outcome, true
}
