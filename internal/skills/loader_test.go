package skills

import (
	"context"
	"testing"
)

// skillFrontmatter builds a SKILL.md with the given name and description.
func skillFrontmatter(name, description string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# Body\n"
}

func TestExtractFrontmatter(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		want     string
		wantOK   bool
	}{
		{
			name:     "simple",
			contents: "---\nname: x\n---\n\nbody",
			want:     "name: x",
			wantOK:   true,
		},
		{
			name:     "multi line",
			contents: "---\nname: x\ndescription: y\n---\nbody",
			want:     "name: x\ndescription: y",
			wantOK:   true,
		},
		{
			name:     "missing opener",
			contents: "name: x\n---\n",
			wantOK:   false,
		},
		{
			name:     "unterminated",
			contents: "---\nname: x\n",
			wantOK:   false,
		},
		{
			name:     "empty frontmatter",
			contents: "---\n---\n",
			wantOK:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := extractFrontmatter(tt.contents)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeSingleLine(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"hello world", "hello world"},
		{"  hello   world  ", "hello world"},
		{"line one\nline two", "line one line two"},
		{"\t tabbed \t value", "tabbed value"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := sanitizeSingleLine(tt.in); got != tt.want {
			t.Errorf("sanitizeSingleLine(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLoadSkillsBasic(t *testing.T) {
	fs := newMemFS()
	fs.addFile("/tmp/skills/demo/SKILL.md", skillFrontmatter("demo-skill", "long description"))

	outcome := LoadSkillsFromRoots(context.Background(), []SkillRoot{{
		Path:       mustAbs("/tmp/skills"),
		Scope:      SkillScopeUser,
		FileSystem: fs,
	}})

	if len(outcome.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", outcome.Errors)
	}
	if len(outcome.Skills) != 1 {
		t.Fatalf("got %d skills, want 1", len(outcome.Skills))
	}
	skill := outcome.Skills[0]
	if skill.Name != "demo-skill" || skill.Description != "long description" {
		t.Fatalf("unexpected skill: %+v", skill)
	}
	if skill.Scope != SkillScopeUser {
		t.Fatalf("scope = %v, want user", skill.Scope)
	}
	if skill.PathToSkillsMd.String() != "/tmp/skills/demo/SKILL.md" {
		t.Fatalf("path = %q", skill.PathToSkillsMd.String())
	}
}

func TestLoadSkillsFallbackName(t *testing.T) {
	fs := newMemFS()
	fs.addFile("/tmp/skills/directory-derived/SKILL.md", "---\ndescription: fallback name\n---\n\n# Body\n")

	outcome := LoadSkillsFromRoots(context.Background(), []SkillRoot{{
		Path:       mustAbs("/tmp/skills"),
		Scope:      SkillScopeUser,
		FileSystem: fs,
	}})
	if len(outcome.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", outcome.Errors)
	}
	if len(outcome.Skills) != 1 {
		t.Fatalf("got %d skills", len(outcome.Skills))
	}
	if outcome.Skills[0].Name != "directory-derived" {
		t.Fatalf("name = %q, want directory-derived", outcome.Skills[0].Name)
	}
}

func TestLoadSkillsShortDescription(t *testing.T) {
	fs := newMemFS()
	fs.addFile("/tmp/skills/demo/SKILL.md",
		"---\nname: demo-skill\ndescription: long description\nmetadata:\n  short-description: short summary\n---\n\n# Body\n")

	outcome := LoadSkillsFromRoots(context.Background(), []SkillRoot{{
		Path:       mustAbs("/tmp/skills"),
		Scope:      SkillScopeUser,
		FileSystem: fs,
	}})
	if len(outcome.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", outcome.Errors)
	}
	skill := outcome.Skills[0]
	if skill.ShortDescription == nil || *skill.ShortDescription != "short summary" {
		t.Fatalf("short description = %v", skill.ShortDescription)
	}
}

func TestLoadSkillsLengthLimits(t *testing.T) {
	fs := newMemFS()
	tooLong := repeatRune('x', maxDescriptionLen+1)
	fs.addFile("/tmp/skills/too-long/SKILL.md", skillFrontmatter("too-long", tooLong))

	outcome := LoadSkillsFromRoots(context.Background(), []SkillRoot{{
		Path:       mustAbs("/tmp/skills"),
		Scope:      SkillScopeUser,
		FileSystem: fs,
	}})
	if len(outcome.Skills) != 0 {
		t.Fatalf("expected no skills, got %d", len(outcome.Skills))
	}
	if len(outcome.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(outcome.Errors))
	}
	if !contains(outcome.Errors[0].Message, "invalid description") {
		t.Fatalf("error = %q", outcome.Errors[0].Message)
	}
}

func TestLoadSkillsSystemScopeSuppressesErrors(t *testing.T) {
	fs := newMemFS()
	// Missing description -> parse error, but system scope suppresses errors.
	fs.addFile("/tmp/sys/broken/SKILL.md", "---\nname: broken\n---\n\n# Body\n")

	outcome := LoadSkillsFromRoots(context.Background(), []SkillRoot{{
		Path:       mustAbs("/tmp/sys"),
		Scope:      SkillScopeSystem,
		FileSystem: fs,
	}})
	if len(outcome.Errors) != 0 {
		t.Fatalf("system scope should suppress errors, got %v", outcome.Errors)
	}
	if len(outcome.Skills) != 0 {
		t.Fatalf("expected no skills, got %d", len(outcome.Skills))
	}
}

func TestLoadSkillsDeduplicatesByPathPreferringFirstRoot(t *testing.T) {
	fs := newMemFS()
	fs.addFile("/tmp/shared/demo/SKILL.md", skillFrontmatter("demo", "shared"))

	outcome := LoadSkillsFromRoots(context.Background(), []SkillRoot{
		{Path: mustAbs("/tmp/shared"), Scope: SkillScopeRepo, FileSystem: fs},
		{Path: mustAbs("/tmp/shared"), Scope: SkillScopeUser, FileSystem: fs},
	})
	if len(outcome.Skills) != 1 {
		t.Fatalf("expected 1 deduped skill, got %d", len(outcome.Skills))
	}
	// First root (repo) wins.
	if outcome.Skills[0].Scope != SkillScopeRepo {
		t.Fatalf("scope = %v, want repo (first root wins)", outcome.Skills[0].Scope)
	}
}

func TestLoadSkillsSortOrder(t *testing.T) {
	fs := newMemFS()
	fs.addFile("/tmp/u/zeta/SKILL.md", skillFrontmatter("zeta", "z"))
	fs.addFile("/tmp/u/alpha/SKILL.md", skillFrontmatter("alpha", "a"))
	fs.addFile("/tmp/r/beta/SKILL.md", skillFrontmatter("beta", "b"))

	outcome := LoadSkillsFromRoots(context.Background(), []SkillRoot{
		{Path: mustAbs("/tmp/u"), Scope: SkillScopeUser, FileSystem: fs},
		{Path: mustAbs("/tmp/r"), Scope: SkillScopeRepo, FileSystem: fs},
	})
	// scope_rank: repo(0) < user(1); within scope, by name.
	wantNames := []string{"beta", "alpha", "zeta"}
	if len(outcome.Skills) != len(wantNames) {
		t.Fatalf("got %d skills", len(outcome.Skills))
	}
	for i, want := range wantNames {
		if outcome.Skills[i].Name != want {
			t.Fatalf("skill[%d] = %q, want %q", i, outcome.Skills[i].Name, want)
		}
	}
}

func TestLoadSkillsIgnoresHiddenDirs(t *testing.T) {
	fs := newMemFS()
	fs.addFile("/tmp/skills/.hidden/SKILL.md", skillFrontmatter("hidden", "x"))
	fs.addFile("/tmp/skills/visible/SKILL.md", skillFrontmatter("visible", "y"))

	outcome := LoadSkillsFromRoots(context.Background(), []SkillRoot{{
		Path:       mustAbs("/tmp/skills"),
		Scope:      SkillScopeUser,
		FileSystem: fs,
	}})
	if len(outcome.Skills) != 1 || outcome.Skills[0].Name != "visible" {
		t.Fatalf("expected only visible skill, got %+v", outcome.Skills)
	}
}

func TestLoadSkillsMaxScanDepth(t *testing.T) {
	fs := newMemFS()
	// Depth 1..7 from root; skill at depth 7 should be skipped (> maxScanDepth=6).
	deep := "/tmp/skills/a/b/c/d/e/f/g/SKILL.md"
	fs.addFile(deep, skillFrontmatter("deep", "x"))
	shallow := "/tmp/skills/a/SKILL.md"
	fs.addFile(shallow, skillFrontmatter("shallow", "y"))

	outcome := LoadSkillsFromRoots(context.Background(), []SkillRoot{{
		Path:       mustAbs("/tmp/skills"),
		Scope:      SkillScopeUser,
		FileSystem: fs,
	}})
	names := map[string]bool{}
	for _, s := range outcome.Skills {
		names[s.Name] = true
	}
	if !names["shallow"] {
		t.Fatalf("expected shallow skill to load")
	}
	if names["deep"] {
		t.Fatalf("deep skill beyond max scan depth should be skipped")
	}
}

func repeatRune(r rune, n int) string {
	out := make([]rune, n)
	for i := range out {
		out[i] = r
	}
	return string(out)
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
