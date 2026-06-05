package cli

import (
	"strings"
	"testing"
)

// TestCompletionScript checks each shell generator is recognized and emits the
// clap_complete v4.5.65 structural markers for codex. The full byte-for-byte
// equality check against the reference binary lives in the env-gated
// internal/paritytest.TestParityCompletion.
func TestCompletionScript(t *testing.T) {
	tests := []struct {
		shell    string
		wantOK   bool
		mustHave []string
	}{
		{shell: "bash", wantOK: true, mustHave: []string{
			"_codex() {",
			"complete -F _codex -o nosort -o bashdefault -o default codex",
			"complete -F _codex -o bashdefault -o default codex",
		}},
		{shell: "zsh", wantOK: true, mustHave: []string{
			"#compdef codex",
			"autoload -U is-at-least",
			"compdef _codex codex",
		}},
		{shell: "fish", wantOK: true, mustHave: []string{
			"function __fish_codex_global_optspecs",
			"complete -c codex",
		}},
		{shell: "powershell", wantOK: true, mustHave: []string{
			"Register-ArgumentCompleter",
		}},
		{shell: "elvish", wantOK: true, mustHave: []string{
			"edit:completion:arg-completer",
		}},
		{shell: "tcsh", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			script, ok := completionScript(tt.shell)
			if ok != tt.wantOK {
				t.Fatalf("completionScript(%q) ok = %v, want %v", tt.shell, ok, tt.wantOK)
			}
			if !tt.wantOK {
				if script != "" {
					t.Errorf("expected empty script for unsupported shell, got %d bytes", len(script))
				}
				return
			}
			for _, want := range tt.mustHave {
				if !strings.Contains(script, want) {
					t.Errorf("script for %q missing %q", tt.shell, want)
				}
			}
			// Every script must reference the `exec` subcommand somewhere.
			if !strings.Contains(script, "exec") {
				t.Errorf("script for %q missing subcommand names", tt.shell)
			}
		})
	}
}

// TestBashCompletionStructure asserts the bash generator reproduces the exact
// clap_complete header, the for-loop dispatch arm, a representative subcmd_details
// arm with a possible-value flag, and the trailing `complete` lines. These small
// structural extracts are the TDD anchors for the bash template port; the
// end-to-end byte check is TestParityCompletion.
func TestBashCompletionStructure(t *testing.T) {
	script := bashCompletionScript()

	wantFragments := []string{
		// Header preamble.
		"_codex() {\n    local i cur prev opts cmd\n    COMPREPLY=()\n",
		// for-loop dispatch root + a known subcommand arm.
		"            \",$1\")\n                cmd=\"codex\"\n                ;;",
		"            codex,apply)\n                cmd=\"codex__apply\"\n                ;;",
		// A subcmd_details arm carrying a possible-value flag (sandbox modes).
		"        codex__sandbox)\n",
		"COMPREPLY=($(compgen -W \"read-only workspace-write danger-full-access\" -- \"${cur}\"))",
		// A DirPath value hint (--add-dir uses plusdirs).
		"                        compopt -o plusdirs\n",
		// Trailing registration block.
		"if [[ \"${BASH_VERSINFO[0]}\" -eq 4 && \"${BASH_VERSINFO[1]}\" -ge 4 || \"${BASH_VERSINFO[0]}\" -gt 4 ]]; then",
	}
	for _, frag := range wantFragments {
		if !strings.Contains(script, frag) {
			t.Errorf("bash script missing structural fragment:\n%q", frag)
		}
	}

	// Sanity: every command-tree node id (e.g. codex__cloud) appears as a case
	// arm exactly where subcmd_details renders it.
	for _, node := range completionTree[1:] {
		if !strings.Contains(script, "        "+node.Fn+")\n") {
			t.Errorf("bash script missing subcmd_details arm for %q", node.Fn)
		}
	}
}

// TestInvalidShellError checks the clap-style invalid-value diagnostic body and
// the documented exit-code-2 behavior via the public command handler.
func TestInvalidShellError(t *testing.T) {
	var stdout, stderr strings.Builder
	streams := Streams{Stdout: &stdout, Stderr: &stderr}
	parsed := ParsedCommandLine{SubcommandArgs: []string{"notashell"}}
	code := runCompletionSubcommand(nil, parsed, streams)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("expected no stdout, got %q", stdout.String())
	}
	want := "error: invalid value 'notashell' for '[SHELL]'\n" +
		"  [possible values: bash, elvish, fish, powershell, zsh]\n" +
		"\n" +
		"For more information, try '--help'.\n"
	if stderr.String() != want {
		t.Errorf("stderr mismatch:\n got: %q\nwant: %q", stderr.String(), want)
	}
}
