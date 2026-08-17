package skills

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/sqlrush/codexgo/internal/utils/truncation"
)

const (
	defaultSkillMetadataCharBudget = 8000
	skillMetadataContextWindowPct  = 2
	skillDescTruncWarnThresholdCh  = 100
	approxBytesPerToken            = 4
)

// Warning and instruction text constants mirror the Rust render.rs string
// constants byte-for-byte.
const (
	// SkillDescriptionTruncatedWarning is shown when descriptions were shortened
	// under a character budget.
	SkillDescriptionTruncatedWarning = "Skill descriptions were shortened to fit the skills context budget. Codex can still see every skill, but some descriptions are shorter. Disable unused skills or plugins to leave more room for the rest."
	// SkillDescriptionTruncatedWarningWithPercent is shown when descriptions were
	// shortened under a token (2%) budget.
	SkillDescriptionTruncatedWarningWithPercent = "Skill descriptions were shortened to fit the 2% skills context budget. Codex can still see every skill, but some descriptions are shorter. Disable unused skills or plugins to leave more room for the rest."
	// SkillDescriptionsRemovedWarningPrefix prefixes the warning emitted when the
	// budget was exceeded and some skills were omitted.
	SkillDescriptionsRemovedWarningPrefix = "Exceeded skills context budget. All skill descriptions were removed and"
	// SkillsIntroWithAbsolutePaths introduces the skills block when absolute file
	// paths are listed inline.
	SkillsIntroWithAbsolutePaths = "A skill is a set of local instructions to follow that is stored in a `SKILL.md` file. Below is the list of skills that can be used. Each entry includes a name, description, and file path so you can open the source for full instructions when using a specific skill."
	// SkillsIntroWithAliases introduces the skills block when short alias paths
	// are listed (expanded via the skill roots table).
	SkillsIntroWithAliases = "A skill is a set of local instructions to follow that is stored in a `SKILL.md` file. Below is the list of skills that can be used. Each entry includes a name, description, and a short path that can be expanded into an absolute path using the skill roots table."
)

// SkillsHowToUseWithAbsolutePaths is the how-to-use guidance for the absolute
// path rendering. It mirrors the Rust constant byte-for-byte.
const SkillsHowToUseWithAbsolutePaths = `- Discovery: The list above is the skills available in this session (name + description + file path). Skill bodies live on disk at the listed paths.
- Trigger rules: If the user names a skill (with ` + "`$SkillName`" + ` or plain text) OR the task clearly matches a skill's description shown above, you must use that skill for that turn. Multiple mentions mean use them all. Do not carry skills across turns unless re-mentioned.
- Missing/blocked: If a named skill isn't in the list or the path can't be read, say so briefly and continue with the best fallback.
- How to use a skill (progressive disclosure):
  1) After deciding to use a skill, open its ` + "`SKILL.md`" + `. Read only enough to follow the workflow.
  2) When ` + "`SKILL.md`" + ` references relative paths (e.g., ` + "`scripts/foo.py`" + `), resolve them relative to the skill directory listed above first, and only consider other paths if needed.
  3) If ` + "`SKILL.md`" + ` points to extra folders such as ` + "`references/`" + `, load only the specific files needed for the request; don't bulk-load everything.
  4) If ` + "`scripts/`" + ` exist, prefer running or patching them instead of retyping large code blocks.
  5) If ` + "`assets/`" + ` or templates exist, reuse them instead of recreating from scratch.
- Coordination and sequencing:
  - If multiple skills apply, choose the minimal set that covers the request and state the order you'll use them.
  - Announce which skill(s) you're using and why (one short line). If you skip an obvious skill, say why.
- Context hygiene:
  - Keep context small: summarize long sections instead of pasting them; only load extra files when needed.
  - Avoid deep reference-chasing: prefer opening only files directly linked from ` + "`SKILL.md`" + ` unless you're blocked.
  - When variants exist (frameworks, providers, domains), pick only the relevant reference file(s) and note that choice.
- Safety and fallback: If a skill can't be applied cleanly (missing files, unclear instructions), state the issue, pick the next-best approach, and continue.`

// SkillsHowToUseWithAliases is the how-to-use guidance for the alias rendering.
// It mirrors the Rust constant byte-for-byte.
const SkillsHowToUseWithAliases = `- Discovery: The list above is the skills available in this session (name + description + short path). Skill bodies live on disk at the listed paths after expanding the matching alias from ` + "`### Skill roots`" + `.
- Trigger rules: If the user names a skill (with ` + "`$SkillName`" + ` or plain text) OR the task clearly matches a skill's description shown above, you must use that skill for that turn. Multiple mentions mean use them all. Do not carry skills across turns unless re-mentioned.
- Missing/blocked: If a named skill isn't in the list or the path can't be read, say so briefly and continue with the best fallback.
- How to use a skill (progressive disclosure):
  1) After deciding to use a skill, expand the listed short ` + "`path`" + ` with the matching alias from ` + "`### Skill roots`" + `, then open its ` + "`SKILL.md`" + `. Read only enough to follow the workflow.
  2) When ` + "`SKILL.md`" + ` references relative paths (e.g., ` + "`scripts/foo.py`" + `), resolve them relative to the directory containing that expanded ` + "`SKILL.md`" + ` first, and only consider other paths if needed.
  3) If ` + "`SKILL.md`" + ` points to extra folders such as ` + "`references/`" + `, load only the specific files needed for the request; don't bulk-load everything.
  4) If ` + "`scripts/`" + ` exist, prefer running or patching them instead of retyping large code blocks.
  5) If ` + "`assets/`" + ` or templates exist, reuse them instead of recreating from scratch.
- Coordination and sequencing:
  - If multiple skills apply, choose the minimal set that covers the request and state the order you'll use them.
  - Announce which skill(s) you're using and why (one short line). If you skip an obvious skill, say why.
- Context hygiene:
  - Keep context small: summarize long sections instead of pasting them; only load extra files when needed.
  - Avoid deep reference-chasing: prefer opening only files directly linked from ` + "`SKILL.md`" + ` unless you're blocked.
  - When variants exist (frameworks, providers, domains), pick only the relevant reference file(s) and note that choice.
- Safety and fallback: If a skill can't be applied cleanly (missing files, unclear instructions), state the issue, pick the next-best approach, and continue.`

// budgetKind discriminates a token budget from a character budget.
type budgetKind int

const (
	budgetTokens budgetKind = iota
	budgetCharacters
)

// SkillMetadataBudget bounds how much skills metadata may be injected, measured
// either in approximate tokens or characters. It mirrors the Rust
// `SkillMetadataBudget` enum.
type SkillMetadataBudget struct {
	kind  budgetKind
	limit int
}

// TokensBudget returns a token-measured budget.
func TokensBudget(limit int) SkillMetadataBudget {
	return SkillMetadataBudget{kind: budgetTokens, limit: limit}
}

// CharactersBudget returns a character-measured budget.
func CharactersBudget(limit int) SkillMetadataBudget {
	return SkillMetadataBudget{kind: budgetCharacters, limit: limit}
}

// Limit returns the numeric budget limit.
func (b SkillMetadataBudget) Limit() int { return b.limit }

// IsTokens reports whether the budget is token-measured.
func (b SkillMetadataBudget) IsTokens() bool { return b.kind == budgetTokens }

func (b SkillMetadataBudget) cost(text string) int {
	if b.kind == budgetTokens {
		return truncation.ApproxTokenCount(text)
	}
	return utf8.RuneCountInString(text)
}

func (b SkillMetadataBudget) costFromCounts(chars, bytes int) int {
	if b.kind == budgetTokens {
		return approxTokenCountFromBytes(bytes)
	}
	return chars
}

func approxTokenCountFromBytes(bytes int) int {
	return (bytes + approxBytesPerToken - 1) / approxBytesPerToken
}

// DefaultSkillMetadataBudget returns the default budget: 2% of the context window
// in tokens, or a fixed character budget when no usable window is given. It
// mirrors the Rust `default_skill_metadata_budget`. contextWindow uses a pointer
// to model the Rust `Option<i64>`.
func DefaultSkillMetadataBudget(contextWindow *int64) SkillMetadataBudget {
	if contextWindow != nil && *contextWindow > 0 {
		window := *contextWindow
		tokens := window * skillMetadataContextWindowPct / 100
		if tokens < 1 {
			tokens = 1
		}
		return TokensBudget(int(tokens))
	}
	return CharactersBudget(defaultSkillMetadataCharBudget)
}

// SkillRenderReport summarizes how skills were rendered under a budget. It
// mirrors the Rust `SkillRenderReport`.
type SkillRenderReport struct {
	TotalCount                int
	IncludedCount             int
	OmittedCount              int
	TruncatedDescriptionChars int
	TruncatedDescriptionCount int
}

func (r SkillRenderReport) averageTruncatedDescriptionChars() int {
	if r.TotalCount == 0 || r.TruncatedDescriptionChars == 0 {
		return 0
	}
	return (r.TruncatedDescriptionChars + r.TotalCount - 1) / r.TotalCount
}

// AvailableSkills is the rendered skills metadata ready to inject into the
// instructions, along with the render report and any user-facing warning. It
// mirrors the Rust `AvailableSkills`.
type AvailableSkills struct {
	SkillRootLines []string
	SkillLines     []string
	Report         SkillRenderReport
	WarningMessage *string
}

// RenderAvailableSkillsBody renders the full "## Skills" instruction block from
// the root and skill lines. It mirrors the Rust `render_available_skills_body`,
// including the leading and trailing newline.
func RenderAvailableSkillsBody(skillRootLines, skillLines []string) string {
	var lines []string
	lines = append(lines, "## Skills")
	if len(skillRootLines) == 0 {
		lines = append(lines, SkillsIntroWithAbsolutePaths)
	} else {
		lines = append(lines, SkillsIntroWithAliases)
		lines = append(lines, "### Skill roots")
		lines = append(lines, skillRootLines...)
	}
	lines = append(lines, "### Available skills")
	lines = append(lines, skillLines...)

	lines = append(lines, "### How to use skills")
	howToUse := SkillsHowToUseWithAbsolutePaths
	if len(skillRootLines) != 0 {
		howToUse = SkillsHowToUseWithAliases
	}
	lines = append(lines, howToUse)

	return "\n" + strings.Join(lines, "\n") + "\n"
}

// BuildAvailableSkills selects and renders the implicit-invocation-allowed skills
// for the given budget, preferring absolute paths but falling back to aliased
// short paths when those fit more skills. It mirrors the Rust
// `build_available_skills`. It returns nil when no skills are eligible.
func BuildAvailableSkills(outcome *SkillLoadOutcome, budget SkillMetadataBudget) *AvailableSkills {
	skills := outcome.AllowedSkillsForImplicitInvocation()
	if len(skills) == 0 {
		return nil
	}

	absoluteLines := orderedAbsoluteSkillLines(skills)
	absolute := buildAvailableSkillsFromLines(absoluteLines, len(skills), budget, skillPathAliases{})
	if absolute == nil {
		return nil
	}

	selected := absolute
	if absolute.Report.OmittedCount != 0 || absolute.Report.TruncatedDescriptionChars != 0 {
		if aliased := buildAliasedAvailableSkills(outcome, skills, budget); aliased != nil {
			if aliasedRenderIsBetter(aliased, absolute, budget) {
				selected = aliased
			}
		}
	}
	return selected
}

func buildAvailableSkillsFromLines(skillLines []skillLine, totalCount int, budget SkillMetadataBudget, aliases skillPathAliases) *AvailableSkills {
	if totalCount == 0 {
		return nil
	}
	renderedLines, report := renderSkillLinesFromLines(skillLines, totalCount, budget)

	var warning *string
	if report.OmittedCount > 0 {
		skillWord := "skills"
		verb := "were"
		if report.OmittedCount == 1 {
			skillWord = "skill"
			verb = "was"
		}
		msg := fmt.Sprintf("%s %d additional %s %s not included in the model-visible skills list.",
			budgetWarningPrefix(budget, SkillDescriptionsRemovedWarningPrefix),
			report.OmittedCount, skillWord, verb)
		warning = &msg
	} else if report.averageTruncatedDescriptionChars() > skillDescTruncWarnThresholdCh {
		msg := SkillDescriptionTruncatedWarning
		if budget.kind == budgetTokens {
			msg = SkillDescriptionTruncatedWarningWithPercent
		}
		warning = &msg
	}

	return &AvailableSkills{
		SkillRootLines: aliases.skillRootLines,
		SkillLines:     renderedLines,
		Report:         report,
		WarningMessage: warning,
	}
}

func budgetWarningPrefix(budget SkillMetadataBudget, prefix string) string {
	if budget.kind == budgetTokens {
		return strings.Replace(prefix,
			"Exceeded skills context budget.",
			"Exceeded skills context budget of 2%.", 1)
	}
	return prefix
}

func renderSkillLinesFromLines(skillLines []skillLine, totalCount int, budget SkillMetadataBudget) ([]string, SkillRenderReport) {
	fullCost := 0
	for i := range skillLines {
		fullCost += skillLines[i].fullCost(budget)
	}
	if fullCost <= budget.limit {
		included := make([]string, 0, len(skillLines))
		for i := range skillLines {
			included = append(included, skillLines[i].renderFull())
		}
		return included, SkillRenderReport{
			TotalCount:    totalCount,
			IncludedCount: len(skillLines),
		}
	}

	minimumCost := 0
	for i := range skillLines {
		minimumCost += skillLines[i].minimumCost(budget)
	}
	if minimumCost <= budget.limit {
		rendered := renderLinesWithDescriptionBudget(budget, skillLines, budget.limit-minimumCost)
		truncChars, truncCount := sumDescriptionTruncation(rendered)
		included := make([]string, 0, len(rendered))
		for _, r := range rendered {
			included = append(included, r.line)
		}
		return included, SkillRenderReport{
			TotalCount:                totalCount,
			IncludedCount:             len(skillLines),
			TruncatedDescriptionChars: truncChars,
			TruncatedDescriptionCount: truncCount,
		}
	}

	return renderMinimumSkillLinesUntilBudget(budget, skillLines, totalCount)
}

func renderMinimumSkillLinesUntilBudget(budget SkillMetadataBudget, skillLines []skillLine, totalCount int) ([]string, SkillRenderReport) {
	var included []string
	used := 0
	omittedCount := 0
	truncatedDescriptionChars := 0
	truncatedDescriptionCount := 0
	for i := range skillLines {
		line := &skillLines[i]
		lineCost := line.minimumCost(budget)
		descCharCount := line.descriptionCharCount()
		if used+lineCost <= budget.limit {
			used += lineCost
			included = append(included, line.renderMinimum())
		} else {
			omittedCount++
		}
		truncatedDescriptionChars += descCharCount
		if descCharCount > 0 {
			truncatedDescriptionCount++
		}
	}
	return included, SkillRenderReport{
		TotalCount:                totalCount,
		IncludedCount:             len(included),
		OmittedCount:              omittedCount,
		TruncatedDescriptionChars: truncatedDescriptionChars,
		TruncatedDescriptionCount: truncatedDescriptionCount,
	}
}

// skillLine holds the rendering inputs for one skill.
type skillLine struct {
	name        string
	description string
	path        string
}

type renderedSkillLine struct {
	line           string
	truncatedChars int
}

func newSkillLine(skill *SkillMetadata) skillLine {
	return skillLineWithPath(skill, toStringLossy(skill.PathToSkillsMd))
}

func skillLineWithPath(skill *SkillMetadata, path string) skillLine {
	return skillLine{name: skill.Name, description: skill.Description, path: path}
}

func (s *skillLine) fullCost(budget SkillMetadataBudget) int {
	return lineCost(budget, s.renderFull())
}

func (s *skillLine) minimumCost(budget SkillMetadataBudget) int {
	return lineCost(budget, s.renderMinimum())
}

func (s *skillLine) descriptionCharCount() int {
	return utf8.RuneCountInString(s.description)
}

func (s *skillLine) renderFull() string {
	return s.renderWithDescription(s.description)
}

func (s *skillLine) renderMinimum() string {
	return s.renderWithDescription("")
}

func (s *skillLine) renderedDescriptionPrefixLen(descriptionChars int) int {
	count := 0
	for idx := range s.description {
		if count == descriptionChars {
			return idx
		}
		count++
	}
	return len(s.description)
}

func (s *skillLine) renderWithDescriptionChars(descriptionChars int) string {
	if descriptionChars == 0 {
		return fmt.Sprintf("- %s: (file: %s)", s.name, s.path)
	}
	end := s.renderedDescriptionPrefixLen(descriptionChars)
	return fmt.Sprintf("- %s: %s (file: %s)", s.name, s.description[:end], s.path)
}

func (s *skillLine) renderWithDescription(description string) string {
	if description == "" {
		return fmt.Sprintf("- %s: (file: %s)", s.name, s.path)
	}
	return fmt.Sprintf("- %s: %s (file: %s)", s.name, description, s.path)
}

// descriptionBudgetLine precomputes per-character extra costs for distributing a
// description budget across skills. It mirrors the Rust `DescriptionBudgetLine`.
type descriptionBudgetLine struct {
	line               *skillLine
	descriptionCharCnt int
	extraCosts         []int
}

func newDescriptionBudgetLine(line *skillLine, budget SkillMetadataBudget) descriptionBudgetLine {
	minimumLine := line.renderMinimum()
	minimumChars := utf8.RuneCountInString(minimumLine) + 1
	minimumBytes := len(minimumLine) + 1
	minimumCost := budget.costFromCounts(minimumChars, minimumBytes)

	descCharCount := line.descriptionCharCount()
	extraCosts := make([]int, 0, descCharCount+1)
	extraCosts = append(extraCosts, 0)

	prefixChars := 0
	prefixBytes := 0
	for _, ch := range line.description {
		prefixChars++
		prefixBytes += utf8.RuneLen(ch)
		renderedChars := minimumChars + prefixChars + 1
		renderedBytes := minimumBytes + prefixBytes + 1
		cost := budget.costFromCounts(renderedChars, renderedBytes) - minimumCost
		if cost < 0 {
			cost = 0
		}
		extraCosts = append(extraCosts, cost)
	}

	return descriptionBudgetLine{
		line:               line,
		descriptionCharCnt: descCharCount,
		extraCosts:         extraCosts,
	}
}

func lineCost(budget SkillMetadataBudget, line string) int {
	return budget.cost(line + "\n")
}

func linesCost(budget SkillMetadataBudget, lines []string) int {
	used := 0
	for _, line := range lines {
		used += lineCost(budget, line)
	}
	return used
}

func sumDescriptionTruncation(rendered []renderedSkillLine) (int, int) {
	chars := 0
	count := 0
	for _, line := range rendered {
		if line.truncatedChars == 0 {
			continue
		}
		chars += line.truncatedChars
		count++
	}
	return chars, count
}

// renderLinesWithDescriptionBudget distributes a remaining description budget one
// character at a time across skills, mirroring the Rust
// `render_lines_with_description_budget`.
func renderLinesWithDescriptionBudget(budget SkillMetadataBudget, skillLines []skillLine, limit int) []renderedSkillLine {
	budgetLines := make([]descriptionBudgetLine, 0, len(skillLines))
	for i := range skillLines {
		budgetLines = append(budgetLines, newDescriptionBudgetLine(&skillLines[i], budget))
	}
	charAllocations := make([]int, len(budgetLines))
	currentExtraCosts := make([]int, len(budgetLines))
	remaining := limit

	for {
		changed := false
		for index := range budgetLines {
			line := &budgetLines[index]
			if charAllocations[index] >= line.descriptionCharCnt {
				continue
			}
			currentCost := currentExtraCosts[index]
			nextChars := charAllocations[index] + 1
			nextCost := line.extraCosts[nextChars]
			delta := nextCost - currentCost
			if delta < 0 {
				delta = 0
			}
			if delta <= remaining {
				charAllocations[index] = nextChars
				currentExtraCosts[index] = nextCost
				remaining -= delta
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	out := make([]renderedSkillLine, 0, len(budgetLines))
	for index := range budgetLines {
		line := &budgetLines[index]
		descriptionChars := charAllocations[index]
		truncatedChars := line.descriptionCharCnt - descriptionChars
		if truncatedChars < 0 {
			truncatedChars = 0
		}
		out = append(out, renderedSkillLine{
			line:           line.line.renderWithDescriptionChars(descriptionChars),
			truncatedChars: truncatedChars,
		})
	}
	return out
}

func orderedAbsoluteSkillLines(skills []SkillMetadata) []skillLine {
	ordered := orderedSkillsForBudget(skills)
	out := make([]skillLine, 0, len(ordered))
	for _, skill := range ordered {
		out = append(out, newSkillLine(skill))
	}
	return out
}

// orderedSkillsForBudget orders skills for prompt rendering by prompt scope rank,
// then name, then path. It mirrors the Rust `ordered_skills_for_budget`.
func orderedSkillsForBudget(skills []SkillMetadata) []*SkillMetadata {
	ordered := make([]*SkillMetadata, len(skills))
	for i := range skills {
		ordered[i] = &skills[i]
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if ra, rb := promptScopeRank(a.Scope), promptScopeRank(b.Scope); ra != rb {
			return ra < rb
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return comparePaths(a.PathToSkillsMd, b.PathToSkillsMd) < 0
	})
	return ordered
}

func aliasedRenderIsBetter(aliased, absolute *AvailableSkills, budget SkillMetadataBudget) bool {
	if aliased.Report.IncludedCount != absolute.Report.IncludedCount {
		return aliased.Report.IncludedCount > absolute.Report.IncludedCount
	}
	if aliased.Report.TruncatedDescriptionChars != absolute.Report.TruncatedDescriptionChars {
		return aliased.Report.TruncatedDescriptionChars < absolute.Report.TruncatedDescriptionChars
	}
	return availableSkillsCost(budget, aliased) < availableSkillsCost(budget, absolute)
}

func availableSkillsCost(budget SkillMetadataBudget, available *AvailableSkills) int {
	metadataCost := 0
	if len(available.SkillRootLines) != 0 {
		metadataCost = aliasedMetadataOverheadCost(budget, available.SkillRootLines)
	}
	return metadataCost + linesCost(budget, available.SkillLines)
}
