package shellcmd

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// This file ports codex apply-patch/src/invocation.rs's heredoc detection
// (extract_apply_patch_from_bash + parse_shell_script + maybe_parse_apply_patch)
// to Go. It recognizes an `apply_patch <<'EOF' ... EOF` heredoc (optionally
// preceded by `cd <path> &&`) embedded in a shell command so the shell tool can
// route it to the patch applier instead of literally spawning apply_patch.
//
// The Rust implementation uses tree-sitter-bash with a strict query; we use
// mvdan.cc/sh (already a dependency) the same way bash.go does, applying the same
// conservative rules: the heredoc-redirected statement must be the ONLY top-level
// statement, the connector before apply_patch (if any) must be `&&`, `cd` takes
// exactly one positional argument, and apply_patch takes no arguments.

// applyPatchCommandNames are the recognized apply-patch command words, matching
// the Rust APPLY_PATCH_COMMANDS.
var applyPatchCommandNames = map[string]bool{
	"apply_patch": true,
	"applypatch":  true,
}

// ApplyPatchHeredoc is a detected apply_patch heredoc invocation: the heredoc
// body (the patch text, with the trailing newline trimmed to match Rust) and an
// optional `cd` workdir from the `cd <path> && apply_patch` form.
type ApplyPatchHeredoc struct {
	// Body is the heredoc patch text (trailing '\n' trimmed).
	Body string
	// Workdir is the relative path from a leading `cd <path> &&`, or "" when the
	// direct `apply_patch <<EOF` form was used.
	Workdir string
}

// ExtractApplyPatchHeredoc inspects a shell argv (e.g. ["/bin/zsh","-lc",script])
// and returns the embedded apply_patch heredoc when the script is exactly an
// `apply_patch <<'EOF' ... EOF` (or `cd <path> && apply_patch <<'EOF' ... EOF`)
// invocation. It returns ok == false for any other command shape.
//
// It is the Go analogue of the heredoc arm of codex's maybe_parse_apply_patch:
// only the conservative, single-statement forms match, so unrelated commands
// fall through to normal execution.
func ExtractApplyPatchHeredoc(argv []string) (ApplyPatchHeredoc, bool) {
	script, ok := applyPatchShellScript(argv)
	if !ok {
		return ApplyPatchHeredoc{}, false
	}
	file, ok := parseShell(script)
	if !ok {
		return ApplyPatchHeredoc{}, false
	}
	// The redirected statement must be the only top-level statement.
	if len(file.Stmts) != 1 {
		return ApplyPatchHeredoc{}, false
	}
	return extractFromStmt(file.Stmts[0])
}

// applyPatchShellScript resolves the script token from a shell invocation,
// mirroring the Rust parse_shell_script. It accepts the bash-family forms
// [shell, -lc|-c, script]; the heredoc parser then validates the script body.
func applyPatchShellScript(argv []string) (string, bool) {
	if _, script, ok := ExtractBashCommand(argv); ok {
		return script, true
	}
	return "", false
}

// extractFromStmt validates a single top-level statement against the two allowed
// forms and returns the heredoc on a match.
func extractFromStmt(stmt *syntax.Stmt) (ApplyPatchHeredoc, bool) {
	if stmt == nil || stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown {
		return ApplyPatchHeredoc{}, false
	}
	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		// Direct form: `apply_patch <<'EOF' ... EOF`. The heredoc redirect lives on
		// the statement, and the call must be exactly the apply_patch name.
		if !isApplyPatchCall(cmd) {
			return ApplyPatchHeredoc{}, false
		}
		body, ok := singleHeredocBody(stmt.Redirs)
		if !ok {
			return ApplyPatchHeredoc{}, false
		}
		return ApplyPatchHeredoc{Body: trimHeredoc(body)}, true
	case *syntax.BinaryCmd:
		// `cd <path> && apply_patch <<'EOF' ... EOF`.
		if cmd.Op != syntax.AndStmt {
			return ApplyPatchHeredoc{}, false
		}
		workdir, ok := cdSingleArg(cmd.X)
		if !ok {
			return ApplyPatchHeredoc{}, false
		}
		applyStmt := cmd.Y
		if applyStmt == nil || applyStmt.Negated || applyStmt.Background {
			return ApplyPatchHeredoc{}, false
		}
		applyCall, ok := applyStmt.Cmd.(*syntax.CallExpr)
		if !ok || !isApplyPatchCall(applyCall) {
			return ApplyPatchHeredoc{}, false
		}
		body, ok := singleHeredocBody(applyStmt.Redirs)
		if !ok {
			return ApplyPatchHeredoc{}, false
		}
		return ApplyPatchHeredoc{Body: trimHeredoc(body), Workdir: workdir}, true
	default:
		return ApplyPatchHeredoc{}, false
	}
}

// isApplyPatchCall reports whether call is exactly `apply_patch` (or
// `applypatch`) with no additional arguments and no assignment prefix.
func isApplyPatchCall(call *syntax.CallExpr) bool {
	if call == nil || len(call.Assigns) != 0 || len(call.Args) != 1 {
		return false
	}
	name, ok := literalWord(call.Args[0])
	if !ok {
		return false
	}
	return applyPatchCommandNames[name]
}

// cdSingleArg validates that stmt is exactly `cd <path>` (one positional literal
// argument, no redirects, no assignments) and returns the path. Mirrors the Rust
// cd-arm constraints (a single word/string argument).
func cdSingleArg(stmt *syntax.Stmt) (string, bool) {
	if stmt == nil || stmt.Negated || stmt.Background || len(stmt.Redirs) != 0 {
		return "", false
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Assigns) != 0 || len(call.Args) != 2 {
		return "", false
	}
	name, ok := literalWord(call.Args[0])
	if !ok || name != "cd" {
		return "", false
	}
	path, ok := literalWord(call.Args[1])
	if !ok {
		return "", false
	}
	return path, true
}

// singleHeredocBody returns the heredoc body when redirs is exactly one `<<`
// here-document redirect. Any other redirect set (none, multiple, or a non-heredoc
// redirect) is rejected.
func singleHeredocBody(redirs []*syntax.Redirect) (string, bool) {
	if len(redirs) != 1 {
		return "", false
	}
	r := redirs[0]
	if r.Op != syntax.Hdoc && r.Op != syntax.DashHdoc {
		return "", false
	}
	if r.Hdoc == nil {
		return "", false
	}
	var b strings.Builder
	syntax.NewPrinter().Print(&b, r.Hdoc)
	return b.String(), true
}

// trimHeredoc trims the single trailing newline from a heredoc body, matching the
// Rust trim_end_matches('\n') applied to the captured heredoc text. Rust trims
// ALL trailing newlines; mvdan emits exactly one trailing newline for a heredoc
// body, so a single TrimRight of '\n' matches.
func trimHeredoc(body string) string {
	return strings.TrimRight(body, "\n")
}
