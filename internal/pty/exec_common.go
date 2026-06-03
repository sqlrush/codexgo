package pty

import (
	"os/exec"
	"sort"
)

// envSlice converts an environment map into the "KEY=VALUE" slice that exec.Cmd
// expects. The child's environment is fully replaced (codex clears the inherited
// environment before applying the provided map). Keys are sorted so the result
// is deterministic, which does not affect process semantics.
func envSlice(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k := range env {
		out = append(out, k)
	}
	sort.Strings(out)
	for i, k := range out {
		out[i] = k + "=" + env[k]
	}
	return out
}

// applyArg0 overrides argv[0] for the child when arg0 is non-nil. exec.Cmd
// derives Args from (path, args...); rewriting Args[0] sets the argv[0] the
// child observes without changing which binary is executed. Mirrors the arg0
// handling in the Rust spawn helpers.
func applyArg0(cmd *exec.Cmd, arg0 *string) {
	if arg0 == nil {
		return
	}
	if len(cmd.Args) == 0 {
		cmd.Args = []string{*arg0}
		return
	}
	cmd.Args[0] = *arg0
}

// exitCodeFromErr extracts the integer exit code from the error returned by
// cmd.Wait. A nil error means code 0; an *exec.ExitError carries the code;
// anything else maps to -1, matching the Rust helpers which use -1 on failure.
func exitCodeFromErr(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if code := exitErr.ExitCode(); code >= 0 {
			return code
		}
	}
	return -1
}
