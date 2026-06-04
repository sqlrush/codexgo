package tools

import (
	"encoding/json"
	"testing"
)

// TestCreateShellCommandToolSpec asserts the model-visible shell_command tool
// schema is byte-faithful to codex 0.136.0's create_shell_command_tool (the
// non-Windows form with login shells allowed). Properties serialize in
// sorted-key order (matching the Rust BTreeMap) with strict:false and
// additionalProperties:false.
func TestCreateShellCommandToolSpec(t *testing.T) {
	t.Parallel()
	spec := CreateShellCommandTool(CommandToolOptions{AllowLoginShell: true})
	got, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal shell_command spec: %v", err)
	}

	want := `{"type":"function","name":"shell_command","description":"Runs a shell command and returns its output.\n- Always set the ` + "`workdir`" + ` param when using the shell_command function. Do not use ` + "`cd`" + ` unless absolutely necessary.","strict":false,"parameters":{"type":"object","properties":{"command":{"type":"string","description":"Shell script to run in the user's default shell."},"justification":{"type":"string","description":"User-facing approval question for ` + "`require_escalated`" + `; omit otherwise."},"login":{"type":"boolean","description":"True runs with login shell semantics; false disables them. Defaults to true."},"prefix_rule":{"type":"array","description":"Reusable approval prefix for ` + "`cmd`" + `, only with ` + "`sandbox_permissions: \\\"require_escalated\\\"`" + `; for example [\"git\", \"pull\"].","items":{"type":"string"}},"sandbox_permissions":{"type":"string","description":"Per-command sandbox override. Defaults to ` + "`use_default`" + `; use ` + "`require_escalated`" + ` for unsandboxed execution.","enum":["use_default","require_escalated"]},"timeout_ms":{"type":"number","description":"Maximum command runtime. Defaults to 10000 ms."},"workdir":{"type":"string","description":"Working directory for the command. Defaults to the turn cwd."}},"required":["command"],"additionalProperties":false}}`

	if string(got) != want {
		t.Errorf("shell_command spec mismatch\n got: %s\nwant: %s", got, want)
	}
}

// TestCreateExecCommandToolName asserts exec_command keeps the `cmd` required
// argument (distinct from shell_command's `command`).
func TestCreateExecCommandToolName(t *testing.T) {
	t.Parallel()
	spec := CreateExecCommandTool(CommandToolOptions{})
	if spec.Name() != "exec_command" {
		t.Fatalf("name = %q, want exec_command", spec.Name())
	}
	if spec.Function == nil {
		t.Fatal("exec_command spec is not a function tool")
	}
	if len(spec.Function.Parameters.Required) != 1 || spec.Function.Parameters.Required[0] != "cmd" {
		t.Errorf("required = %v, want [cmd]", spec.Function.Parameters.Required)
	}
	if spec.Function.Strict {
		t.Errorf("strict = true, want false")
	}
}

// TestShellCommandToolCallParamsTimeoutAlias verifies the `timeout` alias for
// `timeout_ms` is honored when decoding shell_command arguments.
func TestShellCommandToolCallParamsTimeoutAlias(t *testing.T) {
	t.Parallel()
	var p ShellCommandToolCallParams
	if err := json.Unmarshal([]byte(`{"command":"ls","timeout":5000}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Command != "ls" {
		t.Errorf("command = %q, want ls", p.Command)
	}
	if p.TimeoutMs == nil || *p.TimeoutMs != 5000 {
		t.Errorf("timeout_ms = %v, want 5000", p.TimeoutMs)
	}
}
