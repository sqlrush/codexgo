package skills

import (
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// SkillMetadata describes a single discovered skill. It mirrors the Rust
// `codex_core_skills::SkillMetadata` struct. Pointer fields model the Rust
// `Option<...>` metadata that may be absent.
type SkillMetadata struct {
	Name             string
	Description      string
	ShortDescription *string
	Interface        *SkillInterface
	Dependencies     *SkillDependencies
	Policy           *SkillPolicy
	// PathToSkillsMd is the absolute path to the SKILL.md file that declares
	// this skill (canonicalized when it exists).
	PathToSkillsMd abspath.AbsolutePathBuf
	Scope          SkillScope
	PluginID       *string
}

// allowImplicitInvocation reports whether the skill permits implicit (non
// user-named) invocation. Absent policy or absent flag defaults to true,
// mirroring `SkillMetadata::allow_implicit_invocation`.
func (s *SkillMetadata) allowImplicitInvocation() bool {
	if s.Policy == nil || s.Policy.AllowImplicitInvocation == nil {
		return true
	}
	return *s.Policy.AllowImplicitInvocation
}

// MatchesProductRestrictionForProduct reports whether the skill is allowed for
// the given restriction product. Mirrors
// `SkillMetadata::matches_product_restriction_for_product`.
func (s *SkillMetadata) MatchesProductRestrictionForProduct(restriction *Product) bool {
	if s.Policy == nil {
		return true
	}
	if len(s.Policy.Products) == 0 {
		return true
	}
	if restriction == nil {
		return false
	}
	return restriction.MatchesProductRestriction(s.Policy.Products)
}

// SkillPolicy carries optional gating metadata for a skill. Mirrors the Rust
// `SkillPolicy`.
type SkillPolicy struct {
	AllowImplicitInvocation *bool
	Products                []Product
}

// SkillInterface carries optional presentation metadata for a skill. Mirrors the
// Rust `SkillInterface`. Icon paths are resolved to absolute paths under the
// skill's assets directory (or a plugin's shared assets directory).
type SkillInterface struct {
	DisplayName      *string
	ShortDescription *string
	IconSmall        *abspath.AbsolutePathBuf
	IconLarge        *abspath.AbsolutePathBuf
	BrandColor       *string
	DefaultPrompt    *string
}

// SkillDependencies lists the tool dependencies declared by a skill. Mirrors the
// Rust `SkillDependencies`.
type SkillDependencies struct {
	Tools []SkillToolDependency
}

// SkillToolDependency describes one tool dependency. Mirrors the Rust
// `SkillToolDependency`.
type SkillToolDependency struct {
	Type        string
	Value       string
	Description *string
	Transport   *string
	Command     *string
	URL         *string
}

// SkillError records a non-fatal failure to parse a SKILL.md file. Mirrors the
// Rust `SkillError`.
type SkillError struct {
	Path    abspath.AbsolutePathBuf
	Message string
}

// SkillLoadOutcome is the result of discovering skills from a set of roots,
// applying enable/disable rules, and building implicit-invocation indexes. It
// mirrors the Rust `SkillLoadOutcome`.
//
// Values are produced by [LoadSkillsFromRoots] and finalized by the manager;
// callers should treat the contents as read-only.
type SkillLoadOutcome struct {
	Skills        []SkillMetadata
	Errors        []SkillError
	DisabledPaths map[abspath.AbsolutePathBuf]struct{}

	skillRoots                 []abspath.AbsolutePathBuf
	skillRootByPath            map[abspath.AbsolutePathBuf]abspath.AbsolutePathBuf
	fileSystemsBySkillPath     map[abspath.AbsolutePathBuf]FileSystem
	implicitSkillsByScriptsDir map[abspath.AbsolutePathBuf]SkillMetadata
	implicitSkillsByDocPath    map[abspath.AbsolutePathBuf]SkillMetadata
}

// IsSkillEnabled reports whether skill is not in the disabled set. Mirrors
// `SkillLoadOutcome::is_skill_enabled`.
func (o *SkillLoadOutcome) IsSkillEnabled(skill *SkillMetadata) bool {
	if o.DisabledPaths == nil {
		return true
	}
	_, disabled := o.DisabledPaths[skill.PathToSkillsMd]
	return !disabled
}

// IsSkillAllowedForImplicitInvocation reports whether skill is enabled and its
// policy permits implicit invocation. Mirrors
// `SkillLoadOutcome::is_skill_allowed_for_implicit_invocation`.
func (o *SkillLoadOutcome) IsSkillAllowedForImplicitInvocation(skill *SkillMetadata) bool {
	return o.IsSkillEnabled(skill) && skill.allowImplicitInvocation()
}

// AllowedSkillsForImplicitInvocation returns a copy of the enabled,
// implicit-allowed skills in their stored order. Mirrors
// `SkillLoadOutcome::allowed_skills_for_implicit_invocation`.
func (o *SkillLoadOutcome) AllowedSkillsForImplicitInvocation() []SkillMetadata {
	var out []SkillMetadata
	for i := range o.Skills {
		skill := o.Skills[i]
		if o.IsSkillAllowedForImplicitInvocation(&skill) {
			out = append(out, skill)
		}
	}
	return out
}

// SkillWithEnabled pairs a skill with its enabled flag.
type SkillWithEnabled struct {
	Skill   SkillMetadata
	Enabled bool
}

// SkillsWithEnabled returns each stored skill paired with its enabled flag,
// preserving order. Mirrors `SkillLoadOutcome::skills_with_enabled`.
func (o *SkillLoadOutcome) SkillsWithEnabled() []SkillWithEnabled {
	out := make([]SkillWithEnabled, 0, len(o.Skills))
	for i := range o.Skills {
		skill := o.Skills[i]
		out = append(out, SkillWithEnabled{Skill: skill, Enabled: o.IsSkillEnabled(&skill)})
	}
	return out
}

// fileSystemForSkill returns the filesystem that discovered skill, or nil when
// none was recorded. Mirrors `SkillLoadOutcome::file_system_for_skill`.
func (o *SkillLoadOutcome) fileSystemForSkill(skill *SkillMetadata) FileSystem {
	if o.fileSystemsBySkillPath == nil {
		return nil
	}
	return o.fileSystemsBySkillPath[skill.PathToSkillsMd]
}

// FilterSkillLoadOutcomeForProduct returns a copy of outcome restricted to
// skills that match the given restriction product. Mirrors
// `filter_skill_load_outcome_for_product`.
func FilterSkillLoadOutcomeForProduct(outcome SkillLoadOutcome, restriction *Product) SkillLoadOutcome {
	var skills []SkillMetadata
	retainedPaths := make(map[abspath.AbsolutePathBuf]struct{})
	for i := range outcome.Skills {
		skill := outcome.Skills[i]
		if skill.MatchesProductRestrictionForProduct(restriction) {
			skills = append(skills, skill)
			retainedPaths[skill.PathToSkillsMd] = struct{}{}
		}
	}
	outcome.Skills = skills

	outcome.fileSystemsBySkillPath = filterMapByKey(outcome.fileSystemsBySkillPath, retainedPaths)
	outcome.skillRootByPath = filterMapByKey(outcome.skillRootByPath, retainedPaths)

	retainedRoots := make(map[abspath.AbsolutePathBuf]struct{})
	for _, root := range outcome.skillRootByPath {
		retainedRoots[root] = struct{}{}
	}
	outcome.skillRoots = retainRoots(outcome.skillRoots, retainedRoots)

	outcome.implicitSkillsByScriptsDir = filterImplicitIndex(outcome.implicitSkillsByScriptsDir, restriction)
	outcome.implicitSkillsByDocPath = filterImplicitIndex(outcome.implicitSkillsByDocPath, restriction)
	return outcome
}

func filterMapByKey[V any](m map[abspath.AbsolutePathBuf]V, keep map[abspath.AbsolutePathBuf]struct{}) map[abspath.AbsolutePathBuf]V {
	if m == nil {
		return nil
	}
	out := make(map[abspath.AbsolutePathBuf]V)
	for key, value := range m {
		if _, ok := keep[key]; ok {
			out[key] = value
		}
	}
	return out
}

func retainRoots(roots []abspath.AbsolutePathBuf, keep map[abspath.AbsolutePathBuf]struct{}) []abspath.AbsolutePathBuf {
	var out []abspath.AbsolutePathBuf
	for _, root := range roots {
		if _, ok := keep[root]; ok {
			out = append(out, root)
		}
	}
	return out
}

func filterImplicitIndex(index map[abspath.AbsolutePathBuf]SkillMetadata, restriction *Product) map[abspath.AbsolutePathBuf]SkillMetadata {
	if index == nil {
		return nil
	}
	out := make(map[abspath.AbsolutePathBuf]SkillMetadata)
	for key, skill := range index {
		if skill.MatchesProductRestrictionForProduct(restriction) {
			out[key] = skill
		}
	}
	return out
}
