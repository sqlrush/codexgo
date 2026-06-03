package skills

import (
	"strings"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// SkillConfig is a single enable/disable rule from the `skills.config` array in
// config. Exactly one of Path or Name selects the skill(s) the rule applies to.
// It mirrors the Rust `codex_config::SkillConfig`.
type SkillConfig struct {
	// Path selects a skill by its (canonicalized) SKILL.md path.
	Path *abspath.AbsolutePathBuf
	// Name selects all loaded skills with this name.
	Name *string
	// Enabled is the desired state for the selected skill(s).
	Enabled bool
}

// SkillConfigRuleSelectorKind discriminates the two selector variants.
type SkillConfigRuleSelectorKind int

const (
	// SkillSelectorName selects skills by name.
	SkillSelectorName SkillConfigRuleSelectorKind = iota
	// SkillSelectorPath selects a skill by SKILL.md path.
	SkillSelectorPath
)

// SkillConfigRuleSelector identifies the target of a config rule. It mirrors the
// Rust `SkillConfigRuleSelector` enum.
type SkillConfigRuleSelector struct {
	Kind SkillConfigRuleSelectorKind
	Name string
	Path abspath.AbsolutePathBuf
}

// SkillConfigRule is a resolved enable/disable rule. It mirrors the Rust
// `SkillConfigRule`.
type SkillConfigRule struct {
	Selector SkillConfigRuleSelector
	Enabled  bool
}

// SkillConfigRules is the ordered set of enable/disable rules. Later entries
// override earlier ones for the same selector. It mirrors the Rust
// `SkillConfigRules`.
type SkillConfigRules struct {
	Entries []SkillConfigRule
}

// SkillConfigRulesFromEntries builds [SkillConfigRules] from `skills.config`
// entries in lowest-precedence-first order, mirroring the per-layer merge in the
// Rust `skill_config_rules_from_stack`. Entries with an empty or ambiguous
// selector are ignored. A later entry for the same selector replaces the earlier
// one, preserving layer ordering so a name selector can override a path selector
// for the same loaded skill.
func SkillConfigRulesFromEntries(entries []SkillConfig) SkillConfigRules {
	var rules []SkillConfigRule
	for _, entry := range entries {
		selector, ok := skillConfigRuleSelector(entry)
		if !ok {
			continue
		}
		filtered := rules[:0]
		for _, rule := range rules {
			if !selectorsEqual(rule.Selector, selector) {
				filtered = append(filtered, rule)
			}
		}
		rules = filtered
		rules = append(rules, SkillConfigRule{Selector: selector, Enabled: entry.Enabled})
	}
	return SkillConfigRules{Entries: rules}
}

// ResolveDisabledSkillPaths computes the set of SKILL.md paths disabled by the
// given rules, applied against the loaded skills. It mirrors the Rust
// `resolve_disabled_skill_paths`: rules apply in order, name selectors expand to
// every loaded skill with that name, and a later enable can re-enable a path an
// earlier rule disabled.
func ResolveDisabledSkillPaths(skills []SkillMetadata, rules SkillConfigRules) map[abspath.AbsolutePathBuf]struct{} {
	disabled := make(map[abspath.AbsolutePathBuf]struct{})
	for _, entry := range rules.Entries {
		switch entry.Selector.Kind {
		case SkillSelectorPath:
			if entry.Enabled {
				delete(disabled, entry.Selector.Path)
			} else {
				disabled[entry.Selector.Path] = struct{}{}
			}
		case SkillSelectorName:
			for _, skill := range skills {
				if skill.Name != entry.Selector.Name {
					continue
				}
				if entry.Enabled {
					delete(disabled, skill.PathToSkillsMd)
				} else {
					disabled[skill.PathToSkillsMd] = struct{}{}
				}
			}
		}
	}
	return disabled
}

// skillConfigRuleSelector derives a selector from a config entry, requiring
// exactly one of Path/Name. It mirrors the Rust `skill_config_rule_selector`.
func skillConfigRuleSelector(entry SkillConfig) (SkillConfigRuleSelector, bool) {
	hasPath := entry.Path != nil
	hasName := entry.Name != nil
	switch {
	case hasPath && !hasName:
		return SkillConfigRuleSelector{
			Kind: SkillSelectorPath,
			Path: canonicalizeIfExists(*entry.Path),
		}, true
	case !hasPath && hasName:
		name := strings.TrimSpace(*entry.Name)
		if name == "" {
			return SkillConfigRuleSelector{}, false
		}
		return SkillConfigRuleSelector{Kind: SkillSelectorName, Name: name}, true
	default:
		return SkillConfigRuleSelector{}, false
	}
}

func selectorsEqual(a, b SkillConfigRuleSelector) bool {
	if a.Kind != b.Kind {
		return false
	}
	if a.Kind == SkillSelectorName {
		return a.Name == b.Name
	}
	return a.Path == b.Path
}
