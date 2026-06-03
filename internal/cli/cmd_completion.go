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

// topLevelFlags lists the documented top-level flags offered as completion
// candidates. It mirrors the flattened MultitoolCli option groups (see
// printTopLevelHelp). Each entry is the long form; short aliases are added
// separately where shells benefit from them.
//
// NOTE ON CLAP PARITY: clap's generated completion scripts are far richer than
// this enumeration (clap emits ~5900 lines for codex covering every subcommand's
// own flags, value hints, file/dir completion, and nested subcommand state
// machines). This generator intentionally covers the full top-level subcommand
// set and the documented top-level flags only; per-subcommand flag completion and
// value hints are the remaining gap versus clap's output. Closing that gap would
// require modeling each subcommand's flag table here.
var topLevelFlags = []string{
	"--config",
	"--profile",
	"--enable",
	"--disable",
	"--remote",
	"--remote-auth-token-env",
	"--strict-config",
	"--help",
	"--version",
}

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
	flags := topLevelFlags
	switch shell {
	case "bash":
		return bashCompletion(names, flags), true
	case "zsh":
		return zshCompletion(names, flags), true
	case "fish":
		return fishCompletion(names, flags), true
	case "powershell":
		return powershellCompletion(names, flags), true
	case "elvish":
		return elvishCompletion(names, flags), true
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

func bashCompletion(names, flags []string) string {
	return fmt.Sprintf(`_codex() {
    local cur prev cmds flags
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    cmds="%s"
    flags="%s"
    if [ "$COMP_CWORD" -eq 1 ]; then
        if [[ "$cur" == -* ]]; then
            COMPREPLY=( $(compgen -W "${flags}" -- "${cur}") )
        else
            COMPREPLY=( $(compgen -W "${cmds} ${flags}" -- "${cur}") )
        fi
        return 0
    fi
    # Subsequent words: offer flags; per-subcommand flag completion is the
    # remaining gap versus clap's generated scripts.
    COMPREPLY=( $(compgen -W "${flags}" -- "${cur}") )
    return 0
}
complete -F _codex codex
`, strings.Join(names, " "), strings.Join(flags, " "))
}

func zshCompletion(names, flags []string) string {
	return fmt.Sprintf(`#compdef codex
_codex() {
    local -a cmds flags
    cmds=(%s)
    flags=(%s)
    if (( CURRENT == 2 )); then
        _describe 'command' cmds
        _describe 'flag' flags
    else
        _describe 'flag' flags
    fi
}
compdef _codex codex
`, strings.Join(names, " "), strings.Join(flags, " "))
}

func fishCompletion(names, flags []string) string {
	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "complete -c codex -n '__fish_use_subcommand' -f -a '%s'\n", name)
	}
	for _, flag := range flags {
		// `-l NAME` registers the long flag; the literal `--NAME` form is kept in
		// a trailing comment so the documented flag is discoverable in the script.
		fmt.Fprintf(&b, "complete -c codex -l '%s' # %s\n", strings.TrimLeft(flag, "-"), flag)
	}
	return b.String()
}

func powershellCompletion(names, flags []string) string {
	candidates := make([]string, 0, len(names)+len(flags))
	for _, name := range names {
		candidates = append(candidates, fmt.Sprintf("'%s'", name))
	}
	for _, flag := range flags {
		candidates = append(candidates, fmt.Sprintf("'%s'", flag))
	}
	return fmt.Sprintf(`Register-ArgumentCompleter -Native -CommandName codex -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    @(%s) | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
        [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
    }
}
`, strings.Join(candidates, ", "))
}

func elvishCompletion(names, flags []string) string {
	candidates := make([]string, 0, len(names)+len(flags))
	for _, name := range names {
		candidates = append(candidates, fmt.Sprintf("'%s'", name))
	}
	for _, flag := range flags {
		candidates = append(candidates, fmt.Sprintf("'%s'", flag))
	}
	return fmt.Sprintf(`set edit:completion:arg-completer[codex] = {|@words|
    if (== (count $words) 2) {
        put %s
    } else {
        put %s
    }
}
`, strings.Join(candidates, " "), strings.Join(elvishFlagCandidates(flags), " "))
}

// elvishFlagCandidates renders only the flag candidates for the non-first-word
// completion branch.
func elvishFlagCandidates(flags []string) []string {
	out := make([]string, 0, len(flags))
	for _, flag := range flags {
		out = append(out, fmt.Sprintf("'%s'", flag))
	}
	return out
}

func printCompletionHelp(w io.Writer) {
	fmt.Fprintln(w, "Generate shell completion scripts")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex completion [SHELL]")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Shells: %s (default: bash)\n", strings.Join(completionShells, ", "))
}
