package skills

import (
	"testing"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

func testSkillMetadata(docPath abspath.AbsolutePathBuf) SkillMetadata {
	return SkillMetadata{
		Name:           "test-skill",
		Description:    "test",
		PathToSkillsMd: docPath,
		Scope:          SkillScopeUser,
	}
}

func TestScriptRunToken(t *testing.T) {
	tests := []struct {
		name   string
		tokens []string
		wantOK bool
		want   string
	}{
		{
			name:   "runner plus extension",
			tokens: []string{"python3", "-u", "scripts/fetch_comments.py"},
			wantOK: true,
			want:   "scripts/fetch_comments.py",
		},
		{
			name:   "python -c is not a script run",
			tokens: []string{"python3", "-c", "print(1)"},
			wantOK: false,
		},
		{
			name:   "bash script",
			tokens: []string{"bash", "scripts/run.sh"},
			wantOK: true,
			want:   "scripts/run.sh",
		},
		{
			name:   "node script with path",
			tokens: []string{"node", "/abs/scripts/tool.js"},
			wantOK: true,
			want:   "/abs/scripts/tool.js",
		},
		{
			name:   "unknown runner",
			tokens: []string{"go", "run", "main.go"},
			wantOK: false,
		},
		{
			name:   "runner without script arg",
			tokens: []string{"python3"},
			wantOK: false,
		},
		{
			name:   "non-script extension",
			tokens: []string{"python3", "notes.txt"},
			wantOK: false,
		},
		{
			name:   "empty tokens",
			tokens: nil,
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := scriptRunToken(tt.tokens)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Fatalf("token = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCommandReadsFile(t *testing.T) {
	tests := []struct {
		tokens []string
		want   bool
	}{
		{[]string{"cat", "SKILL.md"}, true},
		{[]string{"/usr/bin/sed", "-n", "1,5p", "SKILL.md"}, true},
		{[]string{"head", "SKILL.md"}, true},
		{[]string{"grep", "x", "SKILL.md"}, false},
		{nil, false},
	}
	for _, tt := range tests {
		got := commandReadsFile(tt.tokens)
		if got != tt.want {
			t.Errorf("commandReadsFile(%v) = %v, want %v", tt.tokens, got, tt.want)
		}
	}
}

func TestDetectSkillDocReadMatchesAbsolutePath(t *testing.T) {
	skillDocPath := mustAbs("/tmp/skill-test/SKILL.md")
	normalized := canonicalizeIfExists(skillDocPath)
	skill := testSkillMetadata(skillDocPath)
	outcome := &SkillLoadOutcome{
		implicitSkillsByScriptsDir: map[abspath.AbsolutePathBuf]SkillMetadata{},
		implicitSkillsByDocPath:    map[abspath.AbsolutePathBuf]SkillMetadata{normalized: skill},
	}

	tokens := []string{"cat", "/tmp/skill-test/SKILL.md", "|", "head"}
	found, ok := detectSkillDocRead(outcome, tokens, mustAbs("/tmp"))
	if !ok || found.Name != "test-skill" {
		t.Fatalf("detectSkillDocRead = (%+v, %v), want test-skill", found, ok)
	}
}

func TestDetectSkillScriptRunRelativePathFromSkillRoot(t *testing.T) {
	skillDocPath := mustAbs("/tmp/skill-test/SKILL.md")
	scriptsDir := canonicalizeIfExists(mustAbs("/tmp/skill-test/scripts"))
	skill := testSkillMetadata(skillDocPath)
	outcome := &SkillLoadOutcome{
		implicitSkillsByScriptsDir: map[abspath.AbsolutePathBuf]SkillMetadata{scriptsDir: skill},
		implicitSkillsByDocPath:    map[abspath.AbsolutePathBuf]SkillMetadata{},
	}

	tokens := []string{"python3", "scripts/fetch_comments.py"}
	found, ok := detectSkillScriptRun(outcome, tokens, mustAbs("/tmp/skill-test"))
	if !ok || found.Name != "test-skill" {
		t.Fatalf("detectSkillScriptRun = (%+v, %v), want test-skill", found, ok)
	}
}

func TestDetectSkillScriptRunAbsolutePathFromAnyWorkdir(t *testing.T) {
	skillDocPath := mustAbs("/tmp/skill-test/SKILL.md")
	scriptsDir := canonicalizeIfExists(mustAbs("/tmp/skill-test/scripts"))
	skill := testSkillMetadata(skillDocPath)
	outcome := &SkillLoadOutcome{
		implicitSkillsByScriptsDir: map[abspath.AbsolutePathBuf]SkillMetadata{scriptsDir: skill},
		implicitSkillsByDocPath:    map[abspath.AbsolutePathBuf]SkillMetadata{},
	}

	tokens := []string{"python3", "/tmp/skill-test/scripts/fetch_comments.py"}
	found, ok := detectSkillScriptRun(outcome, tokens, mustAbs("/tmp/other"))
	if !ok || found.Name != "test-skill" {
		t.Fatalf("detectSkillScriptRun = (%+v, %v), want test-skill", found, ok)
	}
}

func TestDetectImplicitSkillInvocationForCommand(t *testing.T) {
	skillDocPath := mustAbs("/tmp/skill-test/SKILL.md")
	scriptsDir := canonicalizeIfExists(mustAbs("/tmp/skill-test/scripts"))
	docPath := canonicalizeIfExists(skillDocPath)
	skill := testSkillMetadata(skillDocPath)
	outcome := &SkillLoadOutcome{
		implicitSkillsByScriptsDir: map[abspath.AbsolutePathBuf]SkillMetadata{scriptsDir: skill},
		implicitSkillsByDocPath:    map[abspath.AbsolutePathBuf]SkillMetadata{docPath: skill},
	}

	tests := []struct {
		name    string
		command string
		workdir abspath.AbsolutePathBuf
		wantOK  bool
	}{
		{
			name:    "script run relative",
			command: "python3 scripts/fetch_comments.py",
			workdir: mustAbs("/tmp/skill-test"),
			wantOK:  true,
		},
		{
			name:    "doc read absolute",
			command: "cat /tmp/skill-test/SKILL.md",
			workdir: mustAbs("/tmp"),
			wantOK:  true,
		},
		{
			name:    "unrelated command",
			command: "ls /tmp",
			workdir: mustAbs("/tmp"),
			wantOK:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found, ok := DetectImplicitSkillInvocationForCommand(outcome, tt.command, tt.workdir)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (found=%+v)", ok, tt.wantOK, found)
			}
			if ok && found.Name != "test-skill" {
				t.Fatalf("found.Name = %q, want test-skill", found.Name)
			}
		})
	}
}
