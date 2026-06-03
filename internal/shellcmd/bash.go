package shellcmd

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// ExtractBashCommand resolves a bash-family shell invocation into its (shell,
// script) pair. The command must be exactly three tokens: a shell, a flag, and
// a script, where the flag is "-lc" or "-c" (the "-l" prefix is the login-shell
// marker) and the shell resolves to zsh, bash, or sh.
//
// It returns the shell token, the script token, and true on a match; otherwise
// it returns empty strings and false. This is the arg0 / login-shell detection
// codex performs before parsing a script. Mirrors extract_bash_command in
// bash.rs.
func ExtractBashCommand(command []string) (shell string, script string, ok bool) {
	if len(command) != 3 {
		return "", "", false
	}
	shell, flag, script := command[0], command[1], command[2]
	if flag != "-lc" && flag != "-c" {
		return "", "", false
	}
	st, found := DetectShellType(shell)
	if !found {
		return "", "", false
	}
	switch st {
	case ShellTypeZsh, ShellTypeBash, ShellTypeSh:
		return shell, script, true
	default:
		return "", "", false
	}
}

// IsLoginShellFlag reports whether a shell flag requests a login shell, i.e. the
// flag carries the "-l" marker recognized by ExtractBashCommand ("-lc").
func IsLoginShellFlag(flag string) bool {
	return flag == "-lc"
}

// parseShell parses a bash script with mvdan.cc/sh, returning the syntax tree
// or an error. It uses the bash language variant to match tree-sitter-bash's
// acceptance of bash constructs. The second return reports parse success in the
// same spirit as try_parse_shell in bash.rs.
func parseShell(script string) (*syntax.File, bool) {
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash), syntax.KeepComments(false))
	file, err := parser.Parse(strings.NewReader(script), "")
	if err != nil {
		return nil, false
	}
	return file, true
}

// TryParseWordOnlyCommandsSequence parses a script which may contain multiple
// simple commands joined only by the safe operators "&&", "||", ";", and "|".
//
// It returns the sequence of command argv vectors when every command is a
// plain word-only command and the tree contains no disallowed constructs
// (parentheses/subshells, redirections, substitutions, expansions, control
// flow, assignments, etc.). Otherwise it returns nil, false.
//
// Mirrors try_parse_word_only_commands_sequence in bash.rs. The returned slices
// are freshly allocated; the input is not mutated.
func TryParseWordOnlyCommandsSequence(file *syntax.File) ([][]string, bool) {
	var commands [][]string
	for _, stmt := range file.Stmts {
		words, ok := collectWordOnlyStmt(stmt)
		if !ok {
			return nil, false
		}
		commands = append(commands, words...)
	}
	return commands, true
}

// ParseShellLcPlainCommands returns the sequence of plain commands within a
// `bash -lc "..."`, `zsh -lc "..."` (or `-c`) invocation when the script only
// contains word-only commands joined by safe operators. Returns nil, false
// otherwise. Mirrors parse_shell_lc_plain_commands in bash.rs.
func ParseShellLcPlainCommands(command []string) ([][]string, bool) {
	_, script, ok := ExtractBashCommand(command)
	if !ok {
		return nil, false
	}
	file, ok := parseShell(script)
	if !ok {
		return nil, false
	}
	return TryParseWordOnlyCommandsSequence(file)
}

// collectWordOnlyStmt validates a statement and flattens it into the sequence of
// word-only commands it contains. A statement may be a single CallExpr or a
// BinaryCmd tree joined exclusively by "&&", "||", or "|". Statements that are
// negated, backgrounded, coprocessed, disowned, or that carry redirections are
// rejected (mirroring the punctuation/redirect rejection in bash.rs).
func collectWordOnlyStmt(stmt *syntax.Stmt) ([][]string, bool) {
	if stmt == nil {
		return nil, false
	}
	if stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown {
		return nil, false
	}
	if len(stmt.Redirs) != 0 {
		return nil, false
	}
	return collectWordOnlyCommand(stmt.Cmd)
}

// collectWordOnlyCommand handles the two allowed command shapes: a BinaryCmd
// joined by a safe operator, or a plain CallExpr. Any other command type
// (subshells, blocks, if/for/while/case, function declarations, arithmetic,
// process substitution, etc.) is rejected.
func collectWordOnlyCommand(cmd syntax.Command) ([][]string, bool) {
	switch c := cmd.(type) {
	case *syntax.BinaryCmd:
		switch c.Op {
		case syntax.AndStmt, syntax.OrStmt, syntax.Pipe:
			// allowed connector
		default:
			return nil, false
		}
		left, ok := collectWordOnlyStmt(c.X)
		if !ok {
			return nil, false
		}
		right, ok := collectWordOnlyStmt(c.Y)
		if !ok {
			return nil, false
		}
		return append(left, right...), true
	case *syntax.CallExpr:
		words, ok := parsePlainCall(c)
		if !ok {
			return nil, false
		}
		return [][]string{words}, true
	default:
		return nil, false
	}
}

// parsePlainCall extracts the literal argv of a CallExpr, rejecting any
// variable assignment prefix (FOO=bar cmd) and any argument that contains an
// expansion or substitution. Mirrors parse_plain_command_from_node in bash.rs,
// including its handling of concatenated mixed-quote words (e.g. -g"*.py").
func parsePlainCall(call *syntax.CallExpr) ([]string, bool) {
	if call == nil {
		return nil, false
	}
	// Reject variable-assignment prefixes such as `FOO=bar ls`.
	if len(call.Assigns) != 0 {
		return nil, false
	}
	words := make([]string, 0, len(call.Args))
	for _, arg := range call.Args {
		literal, ok := literalWord(arg)
		if !ok {
			return nil, false
		}
		words = append(words, literal)
	}
	if len(words) == 0 {
		return nil, false
	}
	return words, true
}

// literalWord renders a Word to its literal string when every part is a plain
// literal, a single-quoted string, or a double-quoted string containing only
// literal text. Any expansion (parameter, command, arithmetic, process
// substitution, backquotes, glob) makes the word non-literal and is rejected.
// Concatenated parts are joined, matching the "concatenation" handling in
// bash.rs.
func literalWord(word *syntax.Word) (string, bool) {
	if word == nil || len(word.Parts) == 0 {
		return "", false
	}
	var b strings.Builder
	for _, part := range word.Parts {
		s, ok := literalPart(part)
		if !ok {
			return "", false
		}
		b.WriteString(s)
	}
	return b.String(), true
}

// literalPart renders a single word part to its literal text, or reports false
// if the part introduces an expansion/substitution.
func literalPart(part syntax.WordPart) (string, bool) {
	switch p := part.(type) {
	case *syntax.Lit:
		return p.Value, true
	case *syntax.SglQuoted:
		// $'...' performs escape processing and is not a plain literal.
		if p.Dollar {
			return "", false
		}
		return p.Value, true
	case *syntax.DblQuoted:
		// $"..." performs locale translation; reject like the Rust port which
		// only accepts plain double quotes around literal content.
		if p.Dollar {
			return "", false
		}
		var b strings.Builder
		for _, inner := range p.Parts {
			lit, ok := inner.(*syntax.Lit)
			if !ok {
				return "", false
			}
			b.WriteString(lit.Value)
		}
		return b.String(), true
	default:
		return "", false
	}
}
