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

// projectTrustGate reports whether the project containing cwd is trusted, and
// therefore whether project-layer skill roots (`.codex/skills`) may be loaded.
// It mirrors the Rust loader's per-directory ProjectTrustContext decision. A nil
// gate (or a false result) keeps project skills off, matching the conservative
// default. See [config.IsProjectTrusted].
type projectTrustGate func(cwd abspath.AbsolutePathBuf) bool

// assemblySkillsManager loads skills from the default roots for each cwd.
type assemblySkillsManager struct {
	codexHome abspath.AbsolutePathBuf
	manager   *skills.SkillsManager
	// trustGate decides per-cwd whether the project is trusted and so whether
	// project `.codex/skills` roots are included. nil disables project layers.
	trustGate projectTrustGate
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
	return newAssemblySkillsManagerWithTrust(codexHome, bundledEnabled, nil)
}

// newAssemblySkillsManagerWithTrust builds the manager with explicit bundled
// enablement and a per-cwd project-trust gate. When trustGate reports a cwd's
// project as trusted, project-layer `.codex/skills` roots are loaded (via
// skills.WithProjectLayer), matching the Rust loader's git-trust gate. A nil
// gate keeps project layers off.
func newAssemblySkillsManagerWithTrust(codexHome string, bundledEnabled bool, trustGate projectTrustGate) (*assemblySkillsManager, error) {
	home, err := abspath.FromAbsolutePathChecked(codexHome)
	if err != nil {
		return nil, err
	}
	return &assemblySkillsManager{
		codexHome: home,
		manager:   skills.NewSkillsManager(home, bundledEnabled),
		trustGate: trustGate,
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
// layer. The Project config-layer roots (`.codex/skills`) are enabled (via
// skills.WithProjectLayer) only when the trust gate reports cwd's project as
// trusted, mirroring the Rust loader's git-trust ProjectTrustContext gate. With
// no trust gate, or an untrusted/no-entry project, project skills stay off — so
// two binaries on a host with the cwd untrusted still agree.
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
	var opts []skills.RootsOption
	if m.trustGate != nil && m.trustGate(cwdPath) {
		opts = append(opts, skills.WithProjectLayer())
	}
	roots := skills.DefaultSkillRoots(m.codexHome, homeDir, cwdPath, opts...)
	outcome := m.manager.LoadSkills(ctx, cwdPath, roots, skills.SkillConfigRules{}, true /* useCache */, false /* forceReload */)
	return outcome, true
}
