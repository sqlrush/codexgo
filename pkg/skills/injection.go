package skills

import (
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
	"github.com/sqlrush/codexgo/internal/utils/pluginutil"
)

// This file ports the pure-logic portion of the Rust
// `codex_core_skills::injection` module: extracting explicit `$tool-name`
// mentions from user text and selecting which enabled skills those mentions
// refer to.
//
// The Rust module's `build_skill_injections` (async file reads plus analytics
// and OpenTelemetry emission) is intentionally not ported here: it depends on
// the `codex_analytics`, `codex_otel`, and `codex_exec_server` crates, which are
// outside this package's allowed dependencies. The selection logic below is the
// part consumers need to decide which SKILL.md files to inject.

// SkillInjection is a single skill ready to inject into the conversation: its
// name, on-disk path string, and SKILL.md contents. It mirrors the Rust
// `injection::SkillInjection` struct so callers that do read the contents can
// assemble the same shape.
type SkillInjection struct {
	Name     string
	Path     string
	Contents string
}

// ToolMentionKind classifies a mention path by its scheme prefix. It mirrors the
// Rust `injection::ToolMentionKind` enum.
type ToolMentionKind int

const (
	// ToolMentionKindApp is an "app://" mention.
	ToolMentionKindApp ToolMentionKind = iota
	// ToolMentionKindMcp is an "mcp://" mention.
	ToolMentionKindMcp
	// ToolMentionKindPlugin is a "plugin://" mention.
	ToolMentionKindPlugin
	// ToolMentionKindSkill is a "skill://" mention or a path whose final
	// component is (case-insensitively) "SKILL.md".
	ToolMentionKindSkill
	// ToolMentionKindOther is any other path.
	ToolMentionKindOther
)

const (
	appPathPrefix    = "app://"
	mcpPathPrefix    = "mcp://"
	pluginPathPrefix = "plugin://"
	skillPathPrefix  = "skill://"
	skillFilename    = "SKILL.md"
)

// ToolKindForPath classifies path by its scheme. It mirrors the Rust
// `injection::tool_kind_for_path`.
func ToolKindForPath(path string) ToolMentionKind {
	switch {
	case strings.HasPrefix(path, appPathPrefix):
		return ToolMentionKindApp
	case strings.HasPrefix(path, mcpPathPrefix):
		return ToolMentionKindMcp
	case strings.HasPrefix(path, pluginPathPrefix):
		return ToolMentionKindPlugin
	case strings.HasPrefix(path, skillPathPrefix) || isSkillFilename(path):
		return ToolMentionKindSkill
	default:
		return ToolMentionKindOther
	}
}

// isSkillFilename reports whether the final '/'- or '\\'-separated component of
// path equals "SKILL.md" ignoring ASCII case. Mirrors the Rust
// `injection::is_skill_filename`.
func isSkillFilename(path string) bool {
	fileName := path
	if idx := strings.LastIndexAny(path, "/\\"); idx >= 0 {
		fileName = path[idx+1:]
	}
	return strings.EqualFold(fileName, skillFilename)
}

// AppIDFromPath returns the non-empty app id following the "app://" prefix, or
// ("", false) when path is not a non-empty app mention. Mirrors the Rust
// `injection::app_id_from_path`.
func AppIDFromPath(path string) (string, bool) {
	value, ok := strings.CutPrefix(path, appPathPrefix)
	if !ok || value == "" {
		return "", false
	}
	return value, true
}

// PluginConfigNameFromPath returns the non-empty plugin config name following
// the "plugin://" prefix, or ("", false) when path is not a non-empty plugin
// mention. Mirrors the Rust `injection::plugin_config_name_from_path`.
func PluginConfigNameFromPath(path string) (string, bool) {
	value, ok := strings.CutPrefix(path, pluginPathPrefix)
	if !ok || value == "" {
		return "", false
	}
	return value, true
}

// normalizeSkillPath strips the "skill://" prefix if present, leaving the path
// otherwise unchanged. Mirrors the Rust `injection::normalize_skill_path`.
func normalizeSkillPath(path string) string {
	if rest, ok := strings.CutPrefix(path, skillPathPrefix); ok {
		return rest
	}
	return path
}

// ToolMentions captures the names and resource paths extracted from a single
// text input, plus the subset of names that appeared as plain (non-linked)
// mentions. It mirrors the Rust `injection::ToolMentions` struct.
type ToolMentions struct {
	names      map[string]struct{}
	paths      map[string]struct{}
	plainNames map[string]struct{}
}

func newToolMentions() ToolMentions {
	return ToolMentions{
		names:      make(map[string]struct{}),
		paths:      make(map[string]struct{}),
		plainNames: make(map[string]struct{}),
	}
}

func (m ToolMentions) isEmpty() bool {
	return len(m.names) == 0 && len(m.paths) == 0
}

// PlainNames returns the set of plain (non-linked) mention names. The returned
// map must not be mutated.
func (m ToolMentions) PlainNames() map[string]struct{} { return m.plainNames }

// Paths returns the set of resource paths captured from linked mentions. The
// returned map must not be mutated.
func (m ToolMentions) Paths() map[string]struct{} { return m.paths }

// Names returns the set of all mention names. The returned map must not be
// mutated.
func (m ToolMentions) Names() map[string]struct{} { return m.names }

// ExtractToolMentions extracts `$tool-name` mentions from text using the default
// tool-mention sigil. It mirrors the Rust `injection::extract_tool_mentions`.
func ExtractToolMentions(text string) ToolMentions {
	return ExtractToolMentionsWithSigil(text, pluginutil.ToolMentionSigil)
}

// ExtractToolMentionsWithSigil extracts mentions from text using the given
// sigil. Plain `<sigil>name` tokens populate both names and plainNames; linked
// `[<sigil>name](path)` mentions populate names (unless the path is an app, mcp,
// or plugin path) and always capture path. Common environment-variable names are
// skipped. It mirrors the Rust `injection::extract_tool_mentions_with_sigil`.
func ExtractToolMentionsWithSigil(text string, sigil rune) ToolMentions {
	bytes := []byte(text)
	sigilByte := byte(sigil)
	mentions := newToolMentions()

	index := 0
	for index < len(bytes) {
		b := bytes[index]
		if b == '[' {
			if name, path, end, ok := parseLinkedToolMention(text, bytes, index, sigilByte); ok {
				if !isCommonEnvVar(name) {
					kind := ToolKindForPath(path)
					if kind != ToolMentionKindApp && kind != ToolMentionKindMcp && kind != ToolMentionKindPlugin {
						mentions.names[name] = struct{}{}
					}
					mentions.paths[path] = struct{}{}
				}
				index = end
				continue
			}
		}

		if b != sigilByte {
			index++
			continue
		}

		nameStart := index + 1
		if nameStart >= len(bytes) || !isMentionNameChar(bytes[nameStart]) {
			index++
			continue
		}

		nameEnd := nameStart + 1
		for nameEnd < len(bytes) && isMentionNameChar(bytes[nameEnd]) {
			nameEnd++
		}

		name := text[nameStart:nameEnd]
		if !isCommonEnvVar(name) {
			mentions.names[name] = struct{}{}
			mentions.plainNames[name] = struct{}{}
		}
		index = nameEnd
	}

	return mentions
}

// CollectExplicitSkillMentions resolves the skills explicitly mentioned by
// inputs, preserving the order of skills. Structured `skill` inputs are resolved
// first by path against enabled skills; their names then block plain-name text
// fallback. Text inputs are scanned for `$name` and `[$name](path)` mentions:
// linked paths match exactly, while plain names are only used when the name is
// unambiguous (exactly one enabled skill carries it and no connector slug
// collides). It mirrors the Rust `injection::collect_explicit_skill_mentions`.
func CollectExplicitSkillMentions(
	inputs []protocol.UserInput,
	skills []SkillMetadata,
	disabledPaths map[abspath.AbsolutePathBuf]struct{},
	connectorSlugCounts map[string]int,
) []SkillMetadata {
	exactCounts, _ := BuildSkillNameCounts(skills, disabledPaths)

	ctx := skillSelectionContext{
		skills:              skills,
		disabledPaths:       disabledPaths,
		skillNameCounts:     exactCounts,
		connectorSlugCounts: connectorSlugCounts,
	}

	var selected []SkillMetadata
	seenNames := make(map[string]struct{})
	seenPaths := make(map[abspath.AbsolutePathBuf]struct{})
	blockedPlainNames := make(map[string]struct{})

	for _, input := range inputs {
		if input.Type != protocol.UserInputKindSkill {
			continue
		}
		blockedPlainNames[input.Name] = struct{}{}
		path, err := abspath.RelativeToCurrentDir(input.Path)
		if err != nil {
			continue
		}
		if isDisabled(ctx.disabledPaths, path) {
			continue
		}
		if _, seen := seenPaths[path]; seen {
			continue
		}
		for i := range ctx.skills {
			if ctx.skills[i].PathToSkillsMd == path {
				seenPaths[ctx.skills[i].PathToSkillsMd] = struct{}{}
				seenNames[ctx.skills[i].Name] = struct{}{}
				selected = append(selected, ctx.skills[i])
				break
			}
		}
	}

	for _, input := range inputs {
		if input.Type != protocol.UserInputKindText {
			continue
		}
		mentions := ExtractToolMentions(input.Text)
		selected = selectSkillsFromMentions(&ctx, blockedPlainNames, mentions, seenNames, seenPaths, selected)
	}

	return selected
}

// skillSelectionContext bundles the read-only inputs shared by the selection
// helpers. Mirrors the Rust `injection::SkillSelectionContext`.
type skillSelectionContext struct {
	skills              []SkillMetadata
	disabledPaths       map[abspath.AbsolutePathBuf]struct{}
	skillNameCounts     map[string]int
	connectorSlugCounts map[string]int
}

// selectSkillsFromMentions appends mentioned skills to selected while preserving
// the order of ctx.skills. Linked paths are matched first, then unambiguous
// plain names. Mirrors the Rust `injection::select_skills_from_mentions`.
func selectSkillsFromMentions(
	ctx *skillSelectionContext,
	blockedPlainNames map[string]struct{},
	mentions ToolMentions,
	seenNames map[string]struct{},
	seenPaths map[abspath.AbsolutePathBuf]struct{},
	selected []SkillMetadata,
) []SkillMetadata {
	if mentions.isEmpty() {
		return selected
	}

	mentionSkillPaths := make(map[string]struct{})
	for path := range mentions.paths {
		kind := ToolKindForPath(path)
		if kind == ToolMentionKindApp || kind == ToolMentionKindMcp || kind == ToolMentionKindPlugin {
			continue
		}
		mentionSkillPaths[normalizeSkillPath(path)] = struct{}{}
	}

	for i := range ctx.skills {
		skill := ctx.skills[i]
		if isDisabled(ctx.disabledPaths, skill.PathToSkillsMd) {
			continue
		}
		if _, seen := seenPaths[skill.PathToSkillsMd]; seen {
			continue
		}
		if _, ok := mentionSkillPaths[skill.PathToSkillsMd.String()]; ok {
			seenPaths[skill.PathToSkillsMd] = struct{}{}
			seenNames[skill.Name] = struct{}{}
			selected = append(selected, skill)
		}
	}

	for i := range ctx.skills {
		skill := ctx.skills[i]
		if isDisabled(ctx.disabledPaths, skill.PathToSkillsMd) {
			continue
		}
		if _, seen := seenPaths[skill.PathToSkillsMd]; seen {
			continue
		}
		if _, blocked := blockedPlainNames[skill.Name]; blocked {
			continue
		}
		if _, ok := mentions.plainNames[skill.Name]; !ok {
			continue
		}

		skillCount := ctx.skillNameCounts[skill.Name]
		connectorCount := ctx.connectorSlugCounts[asciiLower(skill.Name)]
		if skillCount != 1 || connectorCount != 0 {
			continue
		}

		if _, exists := seenNames[skill.Name]; exists {
			continue
		}
		seenNames[skill.Name] = struct{}{}
		seenPaths[skill.PathToSkillsMd] = struct{}{}
		selected = append(selected, skill)
	}

	return selected
}

// parseLinkedToolMention attempts to parse a `[<sigil>name](path)` mention
// beginning at start (the '[' byte). On success it returns the name, the trimmed
// non-empty path, and the index just past the closing ')'. Mirrors the Rust
// `injection::parse_linked_tool_mention`.
func parseLinkedToolMention(text string, bytes []byte, start int, sigil byte) (string, string, int, bool) {
	sigilIndex := start + 1
	if sigilIndex >= len(bytes) || bytes[sigilIndex] != sigil {
		return "", "", 0, false
	}

	nameStart := sigilIndex + 1
	if nameStart >= len(bytes) || !isMentionNameChar(bytes[nameStart]) {
		return "", "", 0, false
	}

	nameEnd := nameStart + 1
	for nameEnd < len(bytes) && isMentionNameChar(bytes[nameEnd]) {
		nameEnd++
	}

	if nameEnd >= len(bytes) || bytes[nameEnd] != ']' {
		return "", "", 0, false
	}

	pathStart := nameEnd + 1
	for pathStart < len(bytes) && isASCIIWhitespace(bytes[pathStart]) {
		pathStart++
	}
	if pathStart >= len(bytes) || bytes[pathStart] != '(' {
		return "", "", 0, false
	}

	pathEnd := pathStart + 1
	for pathEnd < len(bytes) && bytes[pathEnd] != ')' {
		pathEnd++
	}
	if pathEnd >= len(bytes) || bytes[pathEnd] != ')' {
		return "", "", 0, false
	}

	path := strings.TrimSpace(text[pathStart+1 : pathEnd])
	if path == "" {
		return "", "", 0, false
	}

	name := text[nameStart:nameEnd]
	return name, path, pathEnd + 1, true
}

// commonEnvVars is the set of environment-variable names that are never treated
// as mentions. Mirrors the Rust `injection::is_common_env_var`.
var commonEnvVars = map[string]struct{}{
	"PATH": {}, "HOME": {}, "USER": {}, "SHELL": {}, "PWD": {},
	"TMPDIR": {}, "TEMP": {}, "TMP": {}, "LANG": {}, "TERM": {},
	"XDG_CONFIG_HOME": {},
}

func isCommonEnvVar(name string) bool {
	_, ok := commonEnvVars[asciiUpper(name)]
	return ok
}

// asciiUpper uppercases only ASCII letters, mirroring Rust's
// `str::to_ascii_uppercase`.
func asciiUpper(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r - ('a' - 'A')
		}
		return r
	}, s)
}

// isMentionNameChar reports whether b may appear in a mention name. Mirrors the
// Rust `injection::is_mention_name_char`.
func isMentionNameChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= 'A' && b <= 'Z':
		return true
	case b >= '0' && b <= '9':
		return true
	case b == '_' || b == '-' || b == ':':
		return true
	default:
		return false
	}
}

func isASCIIWhitespace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	default:
		return false
	}
}

func isDisabled(disabled map[abspath.AbsolutePathBuf]struct{}, path abspath.AbsolutePathBuf) bool {
	if disabled == nil {
		return false
	}
	_, ok := disabled[path]
	return ok
}
