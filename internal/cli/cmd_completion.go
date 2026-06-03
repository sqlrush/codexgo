package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// completionShells lists the shells `codex completion` can generate scripts for,
// matching clap_complete::Shell (bash, elvish, fish, powershell, zsh).
var completionShells = []string{"bash", "elvish", "fish", "powershell", "zsh"}

// runCompletionSubcommand handles `codex completion [SHELL]`, generating a shell
// completion script. The default shell is bash, matching the Rust
// CompletionCommand default.
func runCompletionSubcommand(_ context.Context, parsed ParsedCommandLine, streams Streams) int {
	shell := "bash"
	for _, arg := range parsed.SubcommandArgs {
		switch arg {
		case "-h", "--help":
			printCompletionHelp(streams.Stdout)
			return 0
		default:
			if strings.HasPrefix(arg, "-") {
				fmt.Fprintf(streams.Stderr, "error: unexpected flag: %s\n", arg)
				return 1
			}
			shell = arg
		}
	}

	script, ok := completionScript(shell)
	if !ok {
		fmt.Fprintf(streams.Stderr, "error: unsupported shell %q (expected one of: %s)\n", shell, strings.Join(completionShells, ", "))
		return 1
	}
	fmt.Fprint(streams.Stdout, script)
	return 0
}

// completionScript returns the completion script for the given shell, plus
// whether the shell is recognized.
func completionScript(shell string) (string, bool) {
	names := subcommandNames()
	switch shell {
	case "bash":
		return bashCompletion(names), true
	case "zsh":
		return zshCompletion(names), true
	case "fish":
		return fishCompletion(names), true
	case "powershell":
		return powershellCompletion(names), true
	case "elvish":
		return elvishCompletion(names), true
	default:
		return "", false
	}
}

// subcommandNames returns the canonical subcommand names in display order for
// completion candidate lists.
func subcommandNames() []string {
	names := make([]string, 0, len(subcommandSummaries))
	for _, s := range subcommandSummaries {
		names = append(names, s.Name)
	}
	return names
}

func bashCompletion(names []string) string {
	return fmt.Sprintf(`_codex() {
    local cur cmds
    cur="${COMP_WORDS[COMP_CWORD]}"
    cmds="%s"
    if [ "$COMP_CWORD" -eq 1 ]; then
        COMPREPLY=( $(compgen -W "${cmds}" -- "${cur}") )
        return 0
    fi
    return 0
}
complete -F _codex codex
`, strings.Join(names, " "))
}

func zshCompletion(names []string) string {
	return fmt.Sprintf(`#compdef codex
_codex() {
    local -a cmds
    cmds=(%s)
    if (( CURRENT == 2 )); then
        _describe 'command' cmds
    fi
}
compdef _codex codex
`, strings.Join(names, " "))
}

func fishCompletion(names []string) string {
	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "complete -c codex -n '__fish_use_subcommand' -f -a '%s'\n", name)
	}
	return b.String()
}

func powershellCompletion(names []string) string {
	candidates := make([]string, 0, len(names))
	for _, name := range names {
		candidates = append(candidates, fmt.Sprintf("'%s'", name))
	}
	return fmt.Sprintf(`Register-ArgumentCompleter -Native -CommandName codex -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    @(%s) | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
        [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
    }
}
`, strings.Join(candidates, ", "))
}

func elvishCompletion(names []string) string {
	candidates := make([]string, 0, len(names))
	for _, name := range names {
		candidates = append(candidates, fmt.Sprintf("'%s'", name))
	}
	return fmt.Sprintf(`set edit:completion:arg-completer[codex] = {|@words|
    if (== (count $words) 2) {
        put %s
    }
}
`, strings.Join(candidates, " "))
}

func printCompletionHelp(w io.Writer) {
	fmt.Fprintln(w, "Generate shell completion scripts")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex completion [SHELL]")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Shells: %s (default: bash)\n", strings.Join(completionShells, ", "))
}
