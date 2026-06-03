package skills

import (
	"reflect"
	"sort"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// makeSkill builds a minimal SkillMetadata with an absolute SKILL.md path,
// mirroring the Rust `injection_tests::make_skill` helper. Paths are absolute so
// that RelativeToCurrentDir leaves them unchanged (matching the Rust use of
// canonicalized absolute test paths).
func makeSkill(name, path string) SkillMetadata {
	return SkillMetadata{
		Name:           name,
		Description:    name + " skill",
		PathToSkillsMd: mustAbs(path),
		Scope:          SkillScopeUser,
	}
}

func textInput(text string) protocol.UserInput {
	return protocol.UserInput{Type: protocol.UserInputKindText, Text: text}
}

func skillInput(name, path string) protocol.UserInput {
	return protocol.UserInput{Type: protocol.UserInputKindSkill, Name: name, Path: path}
}

func linkedSkillMention(name, path string) string {
	return "[$" + name + "](" + path + ")"
}

func disabledSet(paths ...string) map[abspath.AbsolutePathBuf]struct{} {
	out := make(map[abspath.AbsolutePathBuf]struct{}, len(paths))
	for _, p := range paths {
		out[mustAbs(p)] = struct{}{}
	}
	return out
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func skillNames(skills []SkillMetadata) []string {
	out := make([]string, 0, len(skills))
	for _, s := range skills {
		out = append(out, s.Name)
	}
	return out
}

// textMentionsSkill mirrors the Rust test-only helper
// `injection::text_mentions_skill`: it reports whether text contains a `$name`
// token with an exact name boundary.
func textMentionsSkill(text, skillName string) bool {
	if skillName == "" {
		return false
	}
	bytes := []byte(text)
	nameBytes := []byte(skillName)
	for i := 0; i < len(bytes); i++ {
		if bytes[i] != '$' {
			continue
		}
		nameStart := i + 1
		if nameStart+len(nameBytes) > len(bytes) {
			continue
		}
		match := true
		for j := 0; j < len(nameBytes); j++ {
			if bytes[nameStart+j] != nameBytes[j] {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		afterIndex := nameStart + len(nameBytes)
		if afterIndex >= len(bytes) || !isMentionNameChar(bytes[afterIndex]) {
			return true
		}
	}
	return false
}

func TestTextMentionsSkill(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		skill string
		want  bool
	}{
		{"space-after", "use $notion-research-doc please", "notion-research-doc", true},
		{"paren-wrapped", "($notion-research-doc)", "notion-research-doc", true},
		{"dot-after", "$notion-research-doc.", "notion-research-doc", true},
		{"trailing-s", "$notion-research-docs", "notion-research-doc", false},
		{"underscore-suffix", "$notion-research-doc_extra", "notion-research-doc", false},
		{"end-boundary", "$alpha-skill", "alpha-skill", true},
		{"near-miss-x", "$alpha-skillx", "alpha-skill", false},
		{"second-exact", "$alpha-skillx and later $alpha-skill ", "alpha-skill", true},
		{"many-dollars", string(make([]byte, 0)) + repeatDollar(256) + " not-a-mention", "alpha-skill", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := textMentionsSkill(tt.text, tt.skill); got != tt.want {
				t.Fatalf("textMentionsSkill(%q, %q) = %v, want %v", tt.text, tt.skill, got, tt.want)
			}
		})
	}
}

func repeatDollar(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = '$'
	}
	return string(b)
}

func TestExtractToolMentions(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantNames []string
		wantPaths []string
	}{
		{
			name:      "plain and linked",
			text:      "use $alpha and [$beta](/tmp/beta)",
			wantNames: []string{"alpha", "beta"},
			wantPaths: []string{"/tmp/beta"},
		},
		{
			name:      "skips PATH env var",
			text:      "use $PATH and $alpha",
			wantNames: []string{"alpha"},
			wantPaths: nil,
		},
		{
			name:      "skips linked HOME env var",
			text:      "use [$HOME](/tmp/skill)",
			wantNames: nil,
			wantPaths: nil,
		},
		{
			name:      "skips XDG_CONFIG_HOME",
			text:      "use $XDG_CONFIG_HOME and $beta",
			wantNames: []string{"beta"},
			wantPaths: nil,
		},
		{
			name:      "requires sigil in link",
			text:      "[beta](/tmp/beta)",
			wantNames: nil,
			wantPaths: nil,
		},
		{
			name:      "missing parens falls back to plain",
			text:      "[$beta] /tmp/beta",
			wantNames: []string{"beta"},
			wantPaths: nil,
		},
		{
			name:      "empty parens falls back to plain",
			text:      "[$beta]()",
			wantNames: []string{"beta"},
			wantPaths: nil,
		},
		{
			name:      "trims linked path and allows spacing",
			text:      "use [$beta]   ( /tmp/beta )",
			wantNames: []string{"beta"},
			wantPaths: []string{"/tmp/beta"},
		},
		{
			name:      "stops at non-name chars",
			text:      "use $alpha.skill and $beta_extra",
			wantNames: []string{"alpha", "beta_extra"},
			wantPaths: nil,
		},
		{
			name:      "keeps plugin skill namespaces",
			text:      "use $slack:search and $alpha",
			wantNames: []string{"alpha", "slack:search"},
			wantPaths: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mentions := ExtractToolMentions(tt.text)
			if got := sortedKeys(mentions.Names()); !equalSlices(got, tt.wantNames) {
				t.Fatalf("names = %v, want %v", got, tt.wantNames)
			}
			if got := sortedKeys(mentions.Paths()); !equalSlices(got, tt.wantPaths) {
				t.Fatalf("paths = %v, want %v", got, tt.wantPaths)
			}
		})
	}
}

func equalSlices(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func TestToolKindForPath(t *testing.T) {
	tests := []struct {
		path string
		want ToolMentionKind
	}{
		{"app://chrome", ToolMentionKindApp},
		{"mcp://server", ToolMentionKindMcp},
		{"plugin://my-plugin", ToolMentionKindPlugin},
		{"skill://demo", ToolMentionKindSkill},
		{"/tmp/demo/SKILL.md", ToolMentionKindSkill},
		{"/tmp/demo/skill.md", ToolMentionKindSkill},
		{"/tmp/demo/notes.md", ToolMentionKindOther},
		{"plain", ToolMentionKindOther},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := ToolKindForPath(tt.path); got != tt.want {
				t.Fatalf("ToolKindForPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestAppIDAndPluginConfigFromPath(t *testing.T) {
	if id, ok := AppIDFromPath("app://chrome"); !ok || id != "chrome" {
		t.Fatalf("AppIDFromPath = (%q,%v), want (chrome,true)", id, ok)
	}
	if _, ok := AppIDFromPath("app://"); ok {
		t.Fatalf("AppIDFromPath(empty) should be false")
	}
	if _, ok := AppIDFromPath("mcp://x"); ok {
		t.Fatalf("AppIDFromPath(non-app) should be false")
	}
	if name, ok := PluginConfigNameFromPath("plugin://foo"); !ok || name != "foo" {
		t.Fatalf("PluginConfigNameFromPath = (%q,%v), want (foo,true)", name, ok)
	}
	if _, ok := PluginConfigNameFromPath("plugin://"); ok {
		t.Fatalf("PluginConfigNameFromPath(empty) should be false")
	}
}

func TestCollectExplicitSkillMentions(t *testing.T) {
	noConnectors := map[string]int{}

	tests := []struct {
		name      string
		inputs    []protocol.UserInput
		skills    []SkillMetadata
		disabled  map[abspath.AbsolutePathBuf]struct{}
		connector map[string]int
		want      []string
	}{
		{
			name: "text respects skill order",
			inputs: []protocol.UserInput{
				textInput("first $alpha-skill then $beta-skill"),
			},
			skills:    []SkillMetadata{makeSkill("beta-skill", "/tmp/beta"), makeSkill("alpha-skill", "/tmp/alpha")},
			connector: noConnectors,
			want:      []string{"beta-skill", "alpha-skill"},
		},
		{
			name: "prioritizes structured inputs",
			inputs: []protocol.UserInput{
				textInput("please run $alpha-skill"),
				skillInput("beta-skill", "/tmp/beta"),
			},
			skills:    []SkillMetadata{makeSkill("alpha-skill", "/tmp/alpha"), makeSkill("beta-skill", "/tmp/beta")},
			connector: noConnectors,
			want:      []string{"beta-skill", "alpha-skill"},
		},
		{
			name: "skips invalid structured and blocks plain fallback",
			inputs: []protocol.UserInput{
				textInput("please run $alpha-skill"),
				skillInput("alpha-skill", "/tmp/missing"),
			},
			skills:    []SkillMetadata{makeSkill("alpha-skill", "/tmp/alpha")},
			connector: noConnectors,
			want:      nil,
		},
		{
			name: "skips disabled structured and blocks plain fallback",
			inputs: []protocol.UserInput{
				textInput("please run $alpha-skill"),
				skillInput("alpha-skill", "/tmp/alpha"),
			},
			skills:    []SkillMetadata{makeSkill("alpha-skill", "/tmp/alpha")},
			disabled:  disabledSet("/tmp/alpha"),
			connector: noConnectors,
			want:      nil,
		},
		{
			name: "dedupes by path",
			inputs: []protocol.UserInput{
				textInput("use " + linkedSkillMention("alpha-skill", "/tmp/alpha") + " and " + linkedSkillMention("alpha-skill", "/tmp/alpha")),
			},
			skills:    []SkillMetadata{makeSkill("alpha-skill", "/tmp/alpha")},
			connector: noConnectors,
			want:      []string{"alpha-skill"},
		},
		{
			name: "skips ambiguous name",
			inputs: []protocol.UserInput{
				textInput("use $demo-skill and again $demo-skill"),
			},
			skills:    []SkillMetadata{makeSkill("demo-skill", "/tmp/alpha"), makeSkill("demo-skill", "/tmp/beta")},
			connector: noConnectors,
			want:      nil,
		},
		{
			name: "prefers linked path over name",
			inputs: []protocol.UserInput{
				textInput("use $demo-skill and " + linkedSkillMention("demo-skill", "/tmp/beta")),
			},
			skills:    []SkillMetadata{makeSkill("demo-skill", "/tmp/alpha"), makeSkill("demo-skill", "/tmp/beta")},
			connector: noConnectors,
			want:      []string{"demo-skill"},
		},
		{
			name: "skips plain name when connector matches",
			inputs: []protocol.UserInput{
				textInput("use $alpha-skill"),
			},
			skills:    []SkillMetadata{makeSkill("alpha-skill", "/tmp/alpha")},
			connector: map[string]int{"alpha-skill": 1},
			want:      nil,
		},
		{
			name: "allows explicit path with connector conflict",
			inputs: []protocol.UserInput{
				textInput("use " + linkedSkillMention("alpha-skill", "/tmp/alpha")),
			},
			skills:    []SkillMetadata{makeSkill("alpha-skill", "/tmp/alpha")},
			connector: map[string]int{"alpha-skill": 1},
			want:      []string{"alpha-skill"},
		},
		{
			name: "skips when linked path disabled",
			inputs: []protocol.UserInput{
				textInput("use " + linkedSkillMention("demo-skill", "/tmp/alpha")),
			},
			skills:    []SkillMetadata{makeSkill("demo-skill", "/tmp/alpha"), makeSkill("demo-skill", "/tmp/beta")},
			disabled:  disabledSet("/tmp/alpha"),
			connector: noConnectors,
			want:      nil,
		},
		{
			name: "prefers resource path",
			inputs: []protocol.UserInput{
				textInput("use " + linkedSkillMention("demo-skill", "/tmp/beta")),
			},
			skills:    []SkillMetadata{makeSkill("demo-skill", "/tmp/alpha"), makeSkill("demo-skill", "/tmp/beta")},
			connector: noConnectors,
			want:      []string{"demo-skill"},
		},
		{
			name: "skips missing path with no fallback (two skills)",
			inputs: []protocol.UserInput{
				textInput("use " + linkedSkillMention("demo-skill", "/tmp/missing")),
			},
			skills:    []SkillMetadata{makeSkill("demo-skill", "/tmp/alpha"), makeSkill("demo-skill", "/tmp/beta")},
			connector: noConnectors,
			want:      nil,
		},
		{
			name: "skips missing path without fallback (one skill)",
			inputs: []protocol.UserInput{
				textInput("use " + linkedSkillMention("demo-skill", "/tmp/missing")),
			},
			skills:    []SkillMetadata{makeSkill("demo-skill", "/tmp/alpha")},
			connector: noConnectors,
			want:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CollectExplicitSkillMentions(tt.inputs, tt.skills, tt.disabled, tt.connector)
			if names := skillNames(got); !equalSlices(names, tt.want) {
				t.Fatalf("selected = %v, want %v", names, tt.want)
			}
		})
	}
}

// TestCollectExplicitSkillMentionsSingleUnambiguousName verifies a lone
// unambiguous plain mention selects its skill, which the order/dedupe cases do
// not isolate.
func TestCollectExplicitSkillMentionsSingleUnambiguousName(t *testing.T) {
	skills := []SkillMetadata{makeSkill("alpha-skill", "/tmp/alpha")}
	inputs := []protocol.UserInput{textInput("please run $alpha-skill")}
	got := CollectExplicitSkillMentions(inputs, skills, nil, nil)
	if names := skillNames(got); !equalSlices(names, []string{"alpha-skill"}) {
		t.Fatalf("selected = %v, want [alpha-skill]", names)
	}
}
