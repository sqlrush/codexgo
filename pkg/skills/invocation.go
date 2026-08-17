package skills

import (
	"path/filepath"
	"strings"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// BuildImplicitSkillPathIndexes builds the two lookup indexes used to detect
// implicit skill invocations: one keyed by each skill's scripts/ directory and
// one keyed by each skill's SKILL.md path (both canonicalized when they exist).
// It mirrors the Rust `build_implicit_skill_path_indexes`.
func BuildImplicitSkillPathIndexes(skills []SkillMetadata) (byScriptsDir, byDocPath map[abspath.AbsolutePathBuf]SkillMetadata) {
	byScriptsDir = make(map[abspath.AbsolutePathBuf]SkillMetadata)
	byDocPath = make(map[abspath.AbsolutePathBuf]SkillMetadata)
	for _, skill := range skills {
		docPath := canonicalizeIfExists(skill.PathToSkillsMd)
		byDocPath[docPath] = skill
		if skillDir, ok := skill.PathToSkillsMd.Parent(); ok {
			scriptsDir := canonicalizeIfExists(skillDir.Join("scripts"))
			byScriptsDir[scriptsDir] = skill
		}
	}
	return byScriptsDir, byDocPath
}

// DetectImplicitSkillInvocationForCommand reports the skill, if any, that a shell
// command implicitly invokes: either by running a script under a skill's
// scripts/ directory or by reading a skill's SKILL.md. It mirrors the Rust
// `detect_implicit_skill_invocation_for_command`. workdir resolves relative
// paths in the command.
func DetectImplicitSkillInvocationForCommand(outcome *SkillLoadOutcome, command string, workdir abspath.AbsolutePathBuf) (SkillMetadata, bool) {
	workdir = canonicalizeIfExists(workdir)
	tokens := tokenizeCommand(command)

	if skill, ok := detectSkillScriptRun(outcome, tokens, workdir); ok {
		return skill, true
	}
	return detectSkillDocRead(outcome, tokens, workdir)
}

// tokenizeCommand splits a command into tokens with POSIX shell rules, falling
// back to whitespace splitting when the command cannot be tokenized, mirroring
// the Rust use of `shlex::split`.
func tokenizeCommand(command string) []string {
	if tokens, ok := shlexSplit(command); ok {
		return tokens
	}
	return strings.Fields(command)
}

// scriptRunners is the set of interpreters recognized for implicit script runs.
var scriptRunners = map[string]struct{}{
	"python": {}, "python3": {}, "bash": {}, "zsh": {}, "sh": {},
	"node": {}, "deno": {}, "ruby": {}, "perl": {}, "pwsh": {},
}

// scriptExtensions is the set of script file extensions recognized for implicit
// script runs.
var scriptExtensions = []string{".py", ".sh", ".js", ".ts", ".rb", ".pl", ".ps1"}

// scriptRunToken returns the script path argument of an interpreter invocation
// (e.g. the ".py" file after "python3"), or ok=false when the command is not a
// recognized script run. It mirrors the Rust `script_run_token`.
func scriptRunToken(tokens []string) (string, bool) {
	if len(tokens) == 0 {
		return "", false
	}
	runner := asciiLower(commandBasename(tokens[0]))
	runner = strings.TrimSuffix(runner, ".exe")
	if _, ok := scriptRunners[runner]; !ok {
		return "", false
	}

	var scriptToken string
	found := false
	for _, token := range tokens[1:] {
		if token == "--" || strings.HasPrefix(token, "-") {
			continue
		}
		scriptToken = token
		found = true
		break
	}
	if !found {
		return "", false
	}
	lowered := asciiLower(scriptToken)
	for _, ext := range scriptExtensions {
		if strings.HasSuffix(lowered, ext) {
			return scriptToken, true
		}
	}
	return "", false
}

func detectSkillScriptRun(outcome *SkillLoadOutcome, tokens []string, workdir abspath.AbsolutePathBuf) (SkillMetadata, bool) {
	scriptToken, ok := scriptRunToken(tokens)
	if !ok {
		return SkillMetadata{}, false
	}
	scriptPath := canonicalizeIfExists(workdir.Join(scriptToken))
	for _, ancestor := range scriptPath.Ancestors() {
		if skill, ok := outcome.implicitSkillsByScriptsDir[ancestor]; ok {
			return skill, true
		}
	}
	return SkillMetadata{}, false
}

func detectSkillDocRead(outcome *SkillLoadOutcome, tokens []string, workdir abspath.AbsolutePathBuf) (SkillMetadata, bool) {
	if !commandReadsFile(tokens) {
		return SkillMetadata{}, false
	}
	for _, token := range tokens[1:] {
		if strings.HasPrefix(token, "-") {
			continue
		}
		candidate := canonicalizeIfExists(workdir.Join(token))
		if skill, ok := outcome.implicitSkillsByDocPath[candidate]; ok {
			return skill, true
		}
	}
	return SkillMetadata{}, false
}

// fileReaders is the set of programs treated as reading a file's contents.
var fileReaders = map[string]struct{}{
	"cat": {}, "sed": {}, "head": {}, "tail": {},
	"less": {}, "more": {}, "bat": {}, "awk": {},
}

func commandReadsFile(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	program := asciiLower(commandBasename(tokens[0]))
	_, ok := fileReaders[program]
	return ok
}

// commandBasename returns the final path component of a command, mirroring the
// Rust `command_basename`.
func commandBasename(command string) string {
	base := filepath.Base(command)
	if base == "." || base == string(filepath.Separator) {
		return command
	}
	return base
}
