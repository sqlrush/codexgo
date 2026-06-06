package mcpserver

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// Tool names exposed via tools/list and accepted by tools/call.
const (
	toolNameCodex      = "codex"
	toolNameCodexReply = "codex-reply"
)

// codexToolCallParam is the client-supplied configuration for a "codex"
// tool-call. It mirrors the Rust CodexToolCallParam (kebab-case fields,
// deny_unknown_fields). Only "prompt" is required.
type codexToolCallParam struct {
	// Prompt is the initial user prompt to start the Codex conversation.
	Prompt string `json:"prompt"`
	// Model optionally overrides the model slug.
	Model *string `json:"model,omitempty"`
	// Cwd is the working directory for the session.
	Cwd *string `json:"cwd,omitempty"`
	// ApprovalPolicy selects the shell-command approval policy.
	ApprovalPolicy *string `json:"approval-policy,omitempty"`
	// Sandbox selects the sandbox mode.
	Sandbox *string `json:"sandbox,omitempty"`
	// Config carries individual config overrides.
	Config map[string]json.RawMessage `json:"config,omitempty"`
	// BaseInstructions overrides the default base instructions.
	BaseInstructions *string `json:"base-instructions,omitempty"`
	// DeveloperInstructions are injected as a developer-role message.
	DeveloperInstructions *string `json:"developer-instructions,omitempty"`
	// CompactPrompt overrides the prompt used when compacting.
	CompactPrompt *string `json:"compact-prompt,omitempty"`
}

// parseCodexToolCallParam decodes args into a codexToolCallParam, rejecting
// unknown fields to match the Rust deny_unknown_fields contract.
func parseCodexToolCallParam(args json.RawMessage) (codexToolCallParam, error) {
	var p codexToolCallParam
	if len(args) == 0 {
		return p, fmt.Errorf("missing arguments")
	}
	if err := strictUnmarshal(args, &p); err != nil {
		return p, err
	}
	if p.Prompt == "" {
		return p, fmt.Errorf("the `prompt` field is required")
	}
	return p, nil
}

// approvalPolicyFromTool maps the kebab-case approval-policy value to the core
// AskForApproval kind, mirroring the Rust CodexToolCallApprovalPolicy -> AskForApproval.
func approvalPolicyFromTool(v string) (protocol.AskForApprovalKind, error) {
	switch v {
	case "untrusted":
		return protocol.AskForApprovalUnlessTrusted, nil
	case "on-failure":
		return protocol.AskForApprovalOnFailure, nil
	case "on-request":
		return protocol.AskForApprovalOnRequest, nil
	case "never":
		return protocol.AskForApprovalNever, nil
	default:
		return "", fmt.Errorf("invalid approval-policy %q", v)
	}
}

// sandboxModeFromTool validates the kebab-case sandbox value, mirroring the Rust
// CodexToolCallSandboxMode -> SandboxMode.
func sandboxModeFromTool(v string) (protocol.SandboxMode, error) {
	switch v {
	case "read-only":
		return protocol.SandboxModeReadOnly, nil
	case "workspace-write":
		return protocol.SandboxModeWorkspaceWrite, nil
	case "danger-full-access":
		return protocol.SandboxModeDangerFullAccess, nil
	default:
		return "", fmt.Errorf("invalid sandbox %q", v)
	}
}

// toSessionConfig builds the per-session [core.SessionConfiguration] from the
// tool params overlaid on the assembly defaults. It is the reduced Go analogue of
// CodexToolCallParam::into_config: the turn-running subset of config derivation.
func (p codexToolCallParam) toSessionConfig(defaults toolDefaults) (string, core.SessionConfiguration, error) {
	cfg := core.SessionConfiguration{
		ProviderID: defaults.ProviderID,
		CollaborationMode: protocol.CollaborationMode{
			Settings: protocol.Settings{Model: defaults.Model},
		},
		Cwd:               defaults.Cwd,
		CodexHome:         defaults.CodexHome,
		BaseInstructions:  defaults.BaseInstructions,
		ApprovalPolicy:    protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest},
		ApprovalsReviewer: protocol.ApprovalsReviewerUser,
	}

	if p.Model != nil {
		cfg.CollaborationMode.Settings.Model = *p.Model
	}
	if p.Cwd != nil {
		cfg.Cwd = *p.Cwd
	}
	if p.ApprovalPolicy != nil {
		kind, err := approvalPolicyFromTool(*p.ApprovalPolicy)
		if err != nil {
			return "", core.SessionConfiguration{}, err
		}
		cfg.ApprovalPolicy = protocol.AskForApproval{Kind: kind}
	}
	if p.Sandbox != nil {
		if _, err := sandboxModeFromTool(*p.Sandbox); err != nil {
			return "", core.SessionConfiguration{}, err
		}
	}
	if p.BaseInstructions != nil {
		cfg.BaseInstructions = *p.BaseInstructions
	}
	if p.DeveloperInstructions != nil {
		v := *p.DeveloperInstructions
		cfg.DeveloperInstructions = &v
	}
	if p.CompactPrompt != nil {
		v := *p.CompactPrompt
		cfg.CompactPrompt = &v
	}
	return p.Prompt, cfg, nil
}

// codexReplyParam is the client-supplied configuration for a "codex-reply"
// tool-call. It mirrors the Rust CodexToolCallReplyParam (camelCase fields).
type codexReplyParam struct {
	// ConversationID is DEPRECATED: use ThreadID instead.
	ConversationID *string `json:"conversationId,omitempty"`
	// ThreadID is the thread id for this Codex session.
	ThreadID *string `json:"threadId,omitempty"`
	// Prompt is the next user prompt to continue the conversation.
	Prompt string `json:"prompt"`
}

// parseCodexReplyParam decodes args into a codexReplyParam.
func parseCodexReplyParam(args json.RawMessage) (codexReplyParam, error) {
	var p codexReplyParam
	if len(args) == 0 {
		return p, fmt.Errorf("the `threadId` and `prompt` fields are required")
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return p, err
	}
	if p.Prompt == "" {
		return p, fmt.Errorf("the `prompt` field is required")
	}
	return p, nil
}

// resolveThreadID returns the effective thread id, preferring threadId and
// falling back to the deprecated conversationId, mirroring
// CodexToolCallReplyParam::get_thread_id.
func (p codexReplyParam) resolveThreadID() (string, error) {
	if p.ThreadID != nil && *p.ThreadID != "" {
		return *p.ThreadID, nil
	}
	if p.ConversationID != nil && *p.ConversationID != "" {
		return *p.ConversationID, nil
	}
	return "", fmt.Errorf("either threadId or conversationId must be provided")
}

// toolDefaults are the per-server defaults applied to a codex tool-call before
// per-call overrides. They are the reduced analogue of the resolved base Config.
type toolDefaults struct {
	Model            string
	ProviderID       string
	Cwd              string
	CodexHome        string
	BaseInstructions string
}

// codexToolOutputSchema is the output schema shared by both tools: an object
// with a threadId and content string. Mirrors codex_tool_output_schema.
func codexToolOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"threadId": map[string]any{"type": "string"},
			"content":  map[string]any{"type": "string"},
		},
		"required": []any{"threadId", "content"},
	}
}

// toolDescriptor is one entry in the tools/list response. It mirrors the rmcp
// Tool shape: name, description, title, inputSchema, outputSchema.
type toolDescriptor struct {
	Name         string         `json:"name"`
	Title        string         `json:"title,omitempty"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
}

// codexTool returns the descriptor for the "codex" tool.
func codexTool() toolDescriptor {
	return toolDescriptor{
		Name:         toolNameCodex,
		Title:        "Codex",
		Description:  "Run a Codex session. Accepts configuration parameters matching the Codex Config struct.",
		InputSchema:  codexToolInputSchema(),
		OutputSchema: codexToolOutputSchema(),
	}
}

// codexReplyTool returns the descriptor for the "codex-reply" tool.
func codexReplyTool() toolDescriptor {
	return toolDescriptor{
		Name:         toolNameCodexReply,
		Title:        "Codex Reply",
		Description:  "Continue a Codex conversation by providing the thread id and prompt.",
		InputSchema:  codexReplyInputSchema(),
		OutputSchema: codexToolOutputSchema(),
	}
}

// codexToolInputSchema is the JSON schema for the "codex" tool inputs. It mirrors
// the verify_codex_tool_json_schema expectation: kebab-case property keys,
// additionalProperties false, prompt required.
func codexToolInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"prompt"},
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "The *initial user prompt* to start the Codex conversation.",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Optional override for the model name (e.g. 'gpt-5.2', 'gpt-5.2-codex').",
			},
			"cwd": map[string]any{
				"type":        "string",
				"description": "Working directory for the session. If relative, it is resolved against the server process's current working directory.",
			},
			"approval-policy": map[string]any{
				"type":        "string",
				"enum":        []any{"untrusted", "on-failure", "on-request", "never"},
				"description": "Approval policy for shell commands generated by the model: `untrusted`, `on-failure`, `on-request`, `never`.",
			},
			"sandbox": map[string]any{
				"type":        "string",
				"enum":        []any{"read-only", "workspace-write", "danger-full-access"},
				"description": "Sandbox mode: `read-only`, `workspace-write`, or `danger-full-access`.",
			},
			"config": map[string]any{
				"type":                 "object",
				"additionalProperties": true,
				"description":          "Individual config settings that will override what is in CODEXGO_HOME/config.toml.",
			},
			"base-instructions": map[string]any{
				"type":        "string",
				"description": "The set of instructions to use instead of the default ones.",
			},
			"developer-instructions": map[string]any{
				"type":        "string",
				"description": "Developer instructions that should be injected as a developer role message.",
			},
			"compact-prompt": map[string]any{
				"type":        "string",
				"description": "Prompt used when compacting the conversation.",
			},
		},
	}
}

// codexReplyInputSchema is the JSON schema for the "codex-reply" tool inputs.
func codexReplyInputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []any{"prompt"},
		"properties": map[string]any{
			"conversationId": map[string]any{
				"type":        "string",
				"description": "DEPRECATED: use threadId instead.",
			},
			"threadId": map[string]any{
				"type":        "string",
				"description": "The thread id for this Codex session. This field is required, but we keep it optional here for backward compatibility for clients that still use conversationId.",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "The *next user prompt* to continue the Codex conversation.",
			},
		},
	}
}
