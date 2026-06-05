package skills

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// SkillsManager orchestrates installing embedded system skills and loading,
// filtering, and finalizing skills for a session. It mirrors the loadable
// surface of the Rust `SkillsManager`.
//
// Root selection from the config-layer stack lives in the caller (which owns the
// config-layer machinery); the manager operates on the [SkillRoot] list it is
// given. It is safe for concurrent use.
type SkillsManager struct {
	codexHome          abspath.AbsolutePathBuf
	restrictionProduct *Product

	mu         sync.RWMutex
	cacheByCwd map[abspath.AbsolutePathBuf]SkillLoadOutcome
}

// NewSkillsManager constructs a manager and installs or removes embedded system
// skills based on bundledSkillsEnabled. The restriction product defaults to
// [ProductCodex]. It mirrors the Rust `SkillsManager::new`.
func NewSkillsManager(codexHome abspath.AbsolutePathBuf, bundledSkillsEnabled bool) *SkillsManager {
	product := ProductCodex
	return NewSkillsManagerWithRestrictionProduct(codexHome, bundledSkillsEnabled, &product)
}

// NewSkillsManagerWithRestrictionProduct constructs a manager with an explicit
// product restriction (nil means no restriction). It installs or removes
// embedded system skills based on bundledSkillsEnabled. It mirrors the Rust
// `SkillsManager::new_with_restriction_product`.
func NewSkillsManagerWithRestrictionProduct(codexHome abspath.AbsolutePathBuf, bundledSkillsEnabled bool, restrictionProduct *Product) *SkillsManager {
	manager := &SkillsManager{
		codexHome:          codexHome,
		restrictionProduct: cloneProductPtr(restrictionProduct),
		cacheByCwd:         make(map[abspath.AbsolutePathBuf]SkillLoadOutcome),
	}
	if !bundledSkillsEnabled {
		// Best-effort cleanup; root selection still enforces config even if
		// removal fails.
		UninstallSystemSkills(codexHome)
	} else if err := InstallSystemSkills(codexHome); err != nil {
		// Mirror the Rust tracing::error log-and-continue: callers cannot
		// recover (discovery proceeds without the system cache), but the
		// failure must not be silent.
		fmt.Fprintf(os.Stderr, "warning: failed to install system skills: %v\n", err)
	}
	return manager
}

// LoadSkills discovers skills from roots, applies the manager's product
// restriction and the given enable/disable rules, and finalizes the
// implicit-invocation indexes. cwd participates only in caching: when
// useCache is true the result is cached and reused for the same cwd unless
// forceReload is set. It mirrors the combination of `skills_for_cwd` and
// `build_skill_outcome` in the Rust manager.
func (m *SkillsManager) LoadSkills(
	ctx context.Context,
	cwd abspath.AbsolutePathBuf,
	roots []SkillRoot,
	rules SkillConfigRules,
	useCache bool,
	forceReload bool,
) SkillLoadOutcome {
	if useCache && !forceReload {
		if outcome, ok := m.cachedOutcome(cwd); ok {
			return outcome
		}
	}

	outcome := m.buildSkillOutcome(ctx, roots, rules)

	if useCache {
		m.mu.Lock()
		m.cacheByCwd[cwd] = outcome
		m.mu.Unlock()
	}
	return outcome
}

// buildSkillOutcome loads, product-filters, disables, and finalizes an outcome.
func (m *SkillsManager) buildSkillOutcome(ctx context.Context, roots []SkillRoot, rules SkillConfigRules) SkillLoadOutcome {
	outcome := FilterSkillLoadOutcomeForProduct(LoadSkillsFromRoots(ctx, roots), m.restrictionProduct)
	disabledPaths := ResolveDisabledSkillPaths(outcome.Skills, rules)
	return FinalizeSkillOutcome(outcome, disabledPaths)
}

// ClearCache empties the per-cwd cache. It mirrors the Rust `clear_cache`.
func (m *SkillsManager) ClearCache() {
	m.mu.Lock()
	m.cacheByCwd = make(map[abspath.AbsolutePathBuf]SkillLoadOutcome)
	m.mu.Unlock()
}

func (m *SkillsManager) cachedOutcome(cwd abspath.AbsolutePathBuf) (SkillLoadOutcome, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	outcome, ok := m.cacheByCwd[cwd]
	return outcome, ok
}

// FinalizeSkillOutcome attaches the disabled-path set and rebuilds the
// implicit-invocation indexes from the now enabled, implicit-allowed skills. It
// mirrors the Rust `finalize_skill_outcome`.
func FinalizeSkillOutcome(outcome SkillLoadOutcome, disabledPaths map[abspath.AbsolutePathBuf]struct{}) SkillLoadOutcome {
	outcome.DisabledPaths = disabledPaths
	byScriptsDir, byDocPath := BuildImplicitSkillPathIndexes(outcome.AllowedSkillsForImplicitInvocation())
	outcome.implicitSkillsByScriptsDir = byScriptsDir
	outcome.implicitSkillsByDocPath = byDocPath
	return outcome
}

func cloneProductPtr(p *Product) *Product {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
