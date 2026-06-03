package skills

import (
	"fmt"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// skillPathAliases carries the rendered "### Skill roots" table lines for the
// aliased rendering. It mirrors the Rust `SkillPathAliases`.
type skillPathAliases struct {
	skillRootLines []string
}

// aliasPlan captures the alias roots and per-skill alias mapping used to render
// short paths. It mirrors the Rust `AliasPlan`.
type aliasPlan struct {
	aliases         skillPathAliases
	rootAliases     map[abspath.AbsolutePathBuf]string
	aliasRootByPath map[abspath.AbsolutePathBuf]abspath.AbsolutePathBuf
	tableCost       int
}

// buildAliasedAvailableSkills renders skills using short alias paths, deducting
// the alias table cost from the budget. It mirrors the Rust
// `build_aliased_available_skills`, returning nil when no alias plan applies or
// the table alone exceeds the budget.
func buildAliasedAvailableSkills(outcome *SkillLoadOutcome, skills []SkillMetadata, budget SkillMetadataBudget) *AvailableSkills {
	plan := buildAliasPlan(outcome, skills, budget)
	if plan == nil {
		return nil
	}
	if plan.tableCost >= budget.limit {
		return nil
	}
	adjustedLimit := budget.limit - plan.tableCost
	adjustedBudget := SkillMetadataBudget{kind: budget.kind, limit: adjustedLimit}

	ordered := orderedSkillsForBudget(skills)
	skillLines := make([]skillLine, 0, len(ordered))
	for _, skill := range ordered {
		skillLines = append(skillLines, skillLineWithPath(skill, renderSkillPathWithAliases(skill, plan)))
	}
	return buildAvailableSkillsFromLines(skillLines, len(skills), adjustedBudget, plan.aliases)
}

// buildAliasPlan computes the alias roots and per-skill alias mapping. It mirrors
// the Rust `build_alias_plan`, returning nil when no roots are in play.
func buildAliasPlan(outcome *SkillLoadOutcome, skills []SkillMetadata, budget SkillMetadataBudget) *aliasPlan {
	skillPaths := make(map[abspath.AbsolutePathBuf]struct{}, len(skills))
	for i := range skills {
		skillPaths[skills[i].PathToSkillsMd] = struct{}{}
	}

	skillRootByPath := make(map[abspath.AbsolutePathBuf]abspath.AbsolutePathBuf)
	for path, root := range outcome.skillRootByPath {
		if _, ok := skillPaths[path]; ok {
			skillRootByPath[path] = root
		}
	}

	var usedRoots []abspath.AbsolutePathBuf
	for _, root := range outcome.skillRoots {
		for _, skillRoot := range skillRootByPath {
			if skillRoot == root {
				usedRoots = append(usedRoots, root)
				break
			}
		}
	}
	if len(usedRoots) == 0 {
		return nil
	}

	versionCounts := pluginVersionSkillCounts(skillRootByPath)
	aliasRootBySkillRoot := make(map[abspath.AbsolutePathBuf]abspath.AbsolutePathBuf, len(usedRoots))
	for _, root := range usedRoots {
		aliasRootBySkillRoot[root] = aliasRootForSkillRoot(root, versionCounts)
	}

	aliasRoots, ok := orderedAliasRoots(usedRoots, aliasRootBySkillRoot)
	if !ok {
		return nil
	}

	rootAliases := make(map[abspath.AbsolutePathBuf]string, len(aliasRoots))
	for index, aliasRoot := range aliasRoots {
		rootAliases[aliasRoot] = fmt.Sprintf("r%d", index)
	}

	aliasRootByPath := make(map[abspath.AbsolutePathBuf]abspath.AbsolutePathBuf)
	for path, skillRoot := range skillRootByPath {
		if aliasRoot, ok := aliasRootBySkillRoot[skillRoot]; ok {
			aliasRootByPath[path] = aliasRoot
		}
	}

	skillRootLines := buildSkillRootLines(aliasRoots)
	tableCost := aliasedMetadataOverheadCost(budget, skillRootLines)

	return &aliasPlan{
		aliases:         skillPathAliases{skillRootLines: skillRootLines},
		rootAliases:     rootAliases,
		aliasRootByPath: aliasRootByPath,
		tableCost:       tableCost,
	}
}

// orderedAliasRoots dedupes alias roots in used-root order. It mirrors the Rust
// `ordered_alias_roots`.
func orderedAliasRoots(usedRoots []abspath.AbsolutePathBuf, aliasRootBySkillRoot map[abspath.AbsolutePathBuf]abspath.AbsolutePathBuf) ([]abspath.AbsolutePathBuf, bool) {
	seen := make(map[abspath.AbsolutePathBuf]struct{})
	var aliasRoots []abspath.AbsolutePathBuf
	for _, root := range usedRoots {
		aliasRoot, ok := aliasRootBySkillRoot[root]
		if !ok {
			return nil, false
		}
		if _, dup := seen[aliasRoot]; !dup {
			seen[aliasRoot] = struct{}{}
			aliasRoots = append(aliasRoots, aliasRoot)
		}
	}
	return aliasRoots, true
}

// aliasRootForSkillRoot chooses the alias root for a skill root: the plugin
// marketplace base for single-skill plugin versions, otherwise the skill root
// itself. It mirrors the Rust `alias_root_for_skill_root`.
func aliasRootForSkillRoot(root abspath.AbsolutePathBuf, versionCounts map[abspath.AbsolutePathBuf]int) abspath.AbsolutePathBuf {
	versionBase, ok := pluginVersionBase(root)
	if !ok {
		return root
	}
	if versionCounts[versionBase] > 1 {
		return root
	}
	if marketplace, ok := pluginMarketplaceBase(root); ok {
		return marketplace
	}
	return root
}

// pluginVersionSkillCounts counts how many skill roots share each plugin version
// base. It mirrors the Rust `plugin_version_skill_counts_for_skill_roots`.
func pluginVersionSkillCounts(skillRootByPath map[abspath.AbsolutePathBuf]abspath.AbsolutePathBuf) map[abspath.AbsolutePathBuf]int {
	counts := make(map[abspath.AbsolutePathBuf]int)
	for _, root := range skillRootByPath {
		if versionBase, ok := pluginVersionBase(root); ok {
			counts[versionBase]++
		}
	}
	return counts
}

// aliasedMetadataOverheadCost is the budget cost of the alias table beyond the
// absolute-path body. It mirrors the Rust `aliased_metadata_overhead_cost`.
func aliasedMetadataOverheadCost(budget SkillMetadataBudget, skillRootLines []string) int {
	absoluteBody := RenderAvailableSkillsBody(nil, nil)
	aliasedBody := RenderAvailableSkillsBody(skillRootLines, nil)
	cost := budget.cost(aliasedBody) - budget.cost(absoluteBody)
	if cost < 0 {
		cost = 0
	}
	return cost
}

// buildSkillRootLines renders the "- `r0` = `...`" alias table lines. It mirrors
// the Rust `build_skill_root_lines`.
func buildSkillRootLines(roots []abspath.AbsolutePathBuf) []string {
	out := make([]string, 0, len(roots))
	for index, root := range roots {
		out = append(out, fmt.Sprintf("- `r%d` = `%s`", index, toStringLossy(root)))
	}
	return out
}

// pluginMarketplaceBase returns the ".../plugins/cache/<marketplace>" base for a
// plugin path, or ok=false. It mirrors the Rust `plugin_marketplace_base`.
func pluginMarketplaceBase(path abspath.AbsolutePathBuf) (abspath.AbsolutePathBuf, bool) {
	comps := pathComponents(path.String())
	// Find the deepest "<marketplace>" directory whose parent is "cache" and
	// whose grandparent is "plugins": candidate = component after cache.
	for i := len(comps) - 1; i >= 0; i-- {
		// candidate is comps[:i+1]; its parent is comps[:i]; the parent's last
		// component is comps[i-1] and the grandparent's is comps[i-2].
		if i >= 2 && comps[i-1] == "cache" && comps[i-2] == "plugins" {
			return rebuildAbsolute(path, comps[:i+1]), true
		}
	}
	return abspath.AbsolutePathBuf{}, false
}

// pluginVersionBase returns ".../<marketplace>/<plugin>/<version>" for a plugin
// path, or ok=false. It mirrors the Rust `plugin_version_base`.
func pluginVersionBase(path abspath.AbsolutePathBuf) (abspath.AbsolutePathBuf, bool) {
	marketplace, ok := pluginMarketplaceBase(path)
	if !ok {
		return abspath.AbsolutePathBuf{}, false
	}
	relative, ok := stripPathPrefix(path, marketplace)
	if !ok {
		return abspath.AbsolutePathBuf{}, false
	}
	parts := pathComponents(relative)
	if len(parts) < 2 {
		return abspath.AbsolutePathBuf{}, false
	}
	return marketplace.Join(parts[0]).Join(parts[1]), true
}

// rebuildAbsolute reconstructs an absolute path from a component prefix, reusing
// the platform root from reference.
func rebuildAbsolute(reference abspath.AbsolutePathBuf, comps []string) abspath.AbsolutePathBuf {
	ancestors := reference.Ancestors()
	root := ancestors[len(ancestors)-1]
	result := root
	for _, comp := range comps {
		result = result.Join(comp)
	}
	return result
}

// renderSkillPathWithAliases renders a skill's path as an alias-prefixed short
// path, falling back to the absolute path when no alias applies. It mirrors the
// Rust `render_skill_path_with_aliases`.
func renderSkillPathWithAliases(skill *SkillMetadata, plan *aliasPlan) string {
	if relative, ok := outcomeRelativeSkillPath(skill, plan); ok {
		return relative
	}
	return toStringLossy(skill.PathToSkillsMd)
}

func outcomeRelativeSkillPath(skill *SkillMetadata, plan *aliasPlan) (string, bool) {
	aliasRoot, ok := plan.aliasRootByPath[skill.PathToSkillsMd]
	if !ok {
		return "", false
	}
	alias, ok := plan.rootAliases[aliasRoot]
	if !ok {
		return "", false
	}
	relative, ok := stripPathPrefix(skill.PathToSkillsMd, aliasRoot)
	if !ok {
		return "", false
	}
	return alias + "/" + relative, true
}
