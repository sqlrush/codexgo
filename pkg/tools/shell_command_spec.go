package tools

import (
	"encoding/json"
)

// This file ports the model-visible shell tool specs from codex-core's
// tools/handlers/shell_spec.rs. gpt-5.5 (`shell_type = "shell_command"`) is
// presented the `shell_command` tool whose single required argument is the
// `command` STRING (a shell script run in the user's default shell), with the
// optional `workdir`, `timeout_ms`, `login`, and approval parameters. The older
// `exec_command` tool (a PTY command taking `cmd`) is also ported so models that
// use it keep the same contract.
//
// The field set + casing + strict:false are kept byte-faithful to codex so the
// tool the model sees is identical. Go marshals a map[string]JsonSchema in
// sorted-key order, matching the Rust BTreeMap the spec builds its properties in.

// CommandToolOptions mirrors the Rust `CommandToolOptions`: it toggles the
// optional `login` parameter and the escalated-permission approval parameters.
type CommandToolOptions struct {
	// AllowLoginShell adds the `login` boolean parameter.
	AllowLoginShell bool
	// ExecPermissionApprovalsEnabled adds the `with_additional_permissions`
	// sandbox value and the `additional_permissions` parameter.
	ExecPermissionApprovalsEnabled bool
}

// strPtr returns a pointer to s (used for the optional schema descriptions).
func strPtr(s string) *string { return &s }

// CreateShellCommandTool builds the `shell_command` ToolSpec, the model-visible
// shell tool for gpt-5.5. Mirrors Rust `create_shell_command_tool`: a single
// required `command` string plus optional `workdir`, `timeout_ms`, `login`, and
// approval parameters. strict is false and additionalProperties is false.
func CreateShellCommandTool(opts CommandToolOptions) ToolSpec {
	properties := map[string]JsonSchema{
		"command":    StringSchema(strPtr("Shell script to run in the user's default shell.")),
		"workdir":    StringSchema(strPtr("Working directory for the command. Defaults to the turn cwd.")),
		"timeout_ms": NumberSchema(strPtr("Maximum command runtime. Defaults to 10000 ms.")),
	}
	if opts.AllowLoginShell {
		properties["login"] = BooleanSchema(strPtr(
			"True runs with login shell semantics; false disables them. Defaults to true."))
	}
	for k, v := range createApprovalParameters(opts.ExecPermissionApprovalsEnabled) {
		properties[k] = v
	}

	const description = "Runs a shell command and returns its output.\n" +
		"- Always set the `workdir` param when using the shell_command function. Do not use `cd` unless absolutely necessary."

	return FunctionToolSpec(ResponsesApiTool{
		Name:        "shell_command",
		Description: description,
		Strict:      false,
		Parameters: ObjectSchema(
			properties,
			[]string{"command"},
			BoolAdditionalProperties(false),
		),
	})
}

// CreateExecCommandTool builds the `exec_command` ToolSpec, the PTY-oriented
// shell tool. Mirrors Rust `create_exec_command_tool`: a single required `cmd`
// string plus optional `workdir`, `shell`, `tty`, `yield_time_ms`,
// `max_output_tokens`, `login`, and approval parameters.
func CreateExecCommandTool(opts CommandToolOptions) ToolSpec {
	return CreateExecCommandToolWithEnvironmentID(opts, false)
}

// CreateExecCommandToolWithEnvironmentID builds the `exec_command` ToolSpec,
// optionally adding the `environment_id` parameter used in multi-environment
// turns. Mirrors Rust `create_exec_command_tool_with_environment_id`.
//
// The Rust spec also carries an output_schema (unified_exec_output_schema), but
// it is `#[serde(skip)]` on ResponsesApiTool — it never reaches the /responses
// request and is only consumed by code-mode, so it is not ported here.
func CreateExecCommandToolWithEnvironmentID(opts CommandToolOptions, includeEnvironmentID bool) ToolSpec {
	properties := map[string]JsonSchema{
		"cmd":     StringSchema(strPtr("Shell command to execute.")),
		"workdir": StringSchema(strPtr("Working directory for the command. Defaults to the turn cwd.")),
		"shell":   StringSchema(strPtr("Shell binary to launch. Defaults to the user's default shell.")),
		"tty": BooleanSchema(strPtr(
			"True allocates a PTY for the command; false or omitted uses plain pipes.")),
		"yield_time_ms": NumberSchema(strPtr(
			"Wait before yielding output. Defaults to 10000 ms; effective range is 250-30000 ms.")),
		"max_output_tokens": NumberSchema(strPtr(
			"Output token budget. Defaults to 10000 tokens; larger requests may be capped by policy.")),
	}
	if opts.AllowLoginShell {
		properties["login"] = BooleanSchema(strPtr(
			"True runs the shell with -l/-i semantics; false disables them. Defaults to true."))
	}
	if includeEnvironmentID {
		properties["environment_id"] = StringSchema(strPtr(
			"Environment id from <environment_context>. Omit to use the primary environment."))
	}
	for k, v := range createApprovalParameters(opts.ExecPermissionApprovalsEnabled) {
		properties[k] = v
	}

	return FunctionToolSpec(ResponsesApiTool{
		Name:        "exec_command",
		Description: "Runs a command in a PTY, returning output or a session ID for ongoing interaction.",
		Strict:      false,
		Parameters: ObjectSchema(
			properties,
			[]string{"cmd"},
			BoolAdditionalProperties(false),
		),
	})
}

// CreateWriteStdinTool builds the `write_stdin` ToolSpec, the companion of the
// UnifiedExec `exec_command` tool for interacting with a live PTY session.
// Mirrors Rust `create_write_stdin_tool`: a single required `session_id` number
// plus optional `chars`, `yield_time_ms`, and `max_output_tokens`. As with
// exec_command, the Rust output_schema is serde-skipped and not ported.
func CreateWriteStdinTool() ToolSpec {
	properties := map[string]JsonSchema{
		"session_id": NumberSchema(strPtr(
			"Identifier of the running unified exec session.")),
		"chars": StringSchema(strPtr(
			"Bytes to write to stdin. Defaults to empty, which polls without writing.")),
		"yield_time_ms": NumberSchema(strPtr(
			"Wait before yielding output. Non-empty writes default to 250 ms and cap at 30000 ms; empty polls wait 5000-300000 ms by default.")),
		"max_output_tokens": NumberSchema(strPtr(
			"Output token budget. Defaults to 10000 tokens; larger requests may be capped by policy.")),
	}

	return FunctionToolSpec(ResponsesApiTool{
		Name:        "write_stdin",
		Description: "Writes characters to an existing unified exec session and returns recent output.",
		Strict:      false,
		Parameters: ObjectSchema(
			properties,
			[]string{"session_id"},
			BoolAdditionalProperties(false),
		),
	})
}

// createApprovalParameters mirrors Rust `create_approval_parameters`: the
// per-command sandbox override enum, justification, and prefix_rule (plus
// additional_permissions when escalated approvals are enabled).
func createApprovalParameters(execPermissionApprovalsEnabled bool) map[string]JsonSchema {
	sandboxValues := []json.RawMessage{json.RawMessage(`"use_default"`)}
	if execPermissionApprovalsEnabled {
		sandboxValues = append(sandboxValues, json.RawMessage(`"with_additional_permissions"`))
	}
	sandboxValues = append(sandboxValues, json.RawMessage(`"require_escalated"`))
	sandboxDescription := "Per-command sandbox override. Defaults to `use_default`; use `require_escalated` for unsandboxed execution."
	if execPermissionApprovalsEnabled {
		sandboxDescription = "Per-command sandbox override. Defaults to `use_default`; use `with_additional_permissions` with `additional_permissions`, or `require_escalated` for unsandboxed execution."
	}

	props := map[string]JsonSchema{
		"sandbox_permissions": StringEnumSchema(sandboxValues, strPtr(sandboxDescription)),
		"justification": StringSchema(strPtr(
			"User-facing approval question for `require_escalated`; omit otherwise.")),
		"prefix_rule": ArraySchema(StringSchema(nil), strPtr(
			`Reusable approval prefix for `+"`cmd`"+`, only with `+"`sandbox_permissions: \"require_escalated\"`"+`; for example ["git", "pull"].`)),
	}
	return props
}

// ShellCommandToolCallParams is the decoded argument shape for a `shell_command`
// function call. Mirrors Rust `ShellCommandToolCallParams` (protocol/models.rs):
// the required `command` STRING plus the optional fields. `timeout` is accepted
// as an alias for `timeout_ms`.
//
// SandboxPermissions and AdditionalPermissions are kept as raw JSON: the
// permission/sandbox-escalation area owns their typed shapes; this surface only
// needs the command, workdir, login, and timeout to drive execution.
type ShellCommandToolCallParams struct {
	Command               string          `json:"command"`
	Workdir               *string         `json:"workdir,omitempty"`
	Login                 *bool           `json:"login,omitempty"`
	TimeoutMs             *uint64         `json:"timeout_ms,omitempty"`
	SandboxPermissions    json.RawMessage `json:"sandbox_permissions,omitempty"`
	PrefixRule            []string        `json:"prefix_rule,omitempty"`
	AdditionalPermissions json.RawMessage `json:"additional_permissions,omitempty"`
	Justification         *string         `json:"justification,omitempty"`
}

// UnmarshalJSON decodes ShellCommandToolCallParams, honoring the `timeout` alias
// for `timeout_ms` (matching the Rust `#[serde(alias = "timeout")]`).
func (p *ShellCommandToolCallParams) UnmarshalJSON(data []byte) error {
	type alias ShellCommandToolCallParams
	var raw struct {
		alias
		Timeout *uint64 `json:"timeout"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = ShellCommandToolCallParams(raw.alias)
	if p.TimeoutMs == nil && raw.Timeout != nil {
		p.TimeoutMs = raw.Timeout
	}
	return nil
}
