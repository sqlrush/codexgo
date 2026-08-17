package mcpserver

import (
	"encoding/json"
	"strings"

	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// elicitationMethod is the server->client request method used to obtain
// approvals (rmcp elicitation/create).
const elicitationMethod = "elicitation/create"

// execApprovalElicitParams conforms to the MCP elicitation request params shape
// plus the codex-specific correlation fields. It is the faithful port of
// ExecApprovalElicitRequestParams.
type execApprovalElicitParams struct {
	Message         string                   `json:"message"`
	RequestedSchema map[string]any           `json:"requestedSchema"`
	ThreadID        string                   `json:"threadId"`
	Elicitation     string                   `json:"codex_elicitation"`
	MCPToolCallID   string                   `json:"codex_mcp_tool_call_id"`
	EventID         string                   `json:"codex_event_id"`
	CallID          string                   `json:"codex_call_id"`
	Command         []string                 `json:"codex_command"`
	Cwd             string                   `json:"codex_cwd"`
	ParsedCmd       []protocol.ParsedCommand `json:"codex_parsed_cmd"`
}

// patchApprovalElicitParams is the elicitation params for an apply-patch
// approval. Faithful port of PatchApprovalElicitRequestParams.
type patchApprovalElicitParams struct {
	Message         string                         `json:"message"`
	RequestedSchema map[string]any                 `json:"requestedSchema"`
	ThreadID        string                         `json:"threadId"`
	Elicitation     string                         `json:"codex_elicitation"`
	MCPToolCallID   string                         `json:"codex_mcp_tool_call_id"`
	EventID         string                         `json:"codex_event_id"`
	CallID          string                         `json:"codex_call_id"`
	Reason          *string                        `json:"codex_reason,omitempty"`
	GrantRoot       *string                        `json:"codex_grant_root,omitempty"`
	Changes         map[string]protocol.FileChange `json:"codex_changes"`
}

// approvalResponse is the client's decision reply to an elicitation/create
// approval request. It mirrors ExecApprovalResponse/PatchApprovalResponse.
type approvalResponse struct {
	Decision protocol.ReviewDecision `json:"decision"`
}

// emptyObjectSchema is the trivial schema sent in approval requests, matching the
// Rust json!({"type":"object","properties":{}}).
func emptyObjectSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

// handleExecApprovalRequest issues an elicitation/create request for an exec
// approval and, on a separate goroutine, submits the decision back to the
// thread. It is the faithful port of handle_exec_approval_request.
func handleExecApprovalRequest(
	sender *outgoingSender,
	thread *core.CodexThread,
	threadID string,
	toolCallID string,
	ev protocol.ExecApprovalRequestEvent,
	eventID string,
) {
	approvalID := ev.EffectiveApprovalID()
	message := "Allow Codex to run `" + shellJoin(ev.Command) + "` in `" + string(ev.Cwd) + "`?"

	params := execApprovalElicitParams{
		Message:         message,
		RequestedSchema: emptyObjectSchema(),
		ThreadID:        threadID,
		Elicitation:     "exec-approval",
		MCPToolCallID:   toolCallID,
		EventID:         eventID,
		CallID:          ev.CallID,
		Command:         ev.Command,
		Cwd:             string(ev.Cwd),
		ParsedCmd:       ev.ParsedCmd,
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return
	}

	respCh := sender.sendRequest(elicitationMethod, raw)
	go awaitExecApproval(thread, approvalID, eventID, respCh)
}

// awaitExecApproval waits for the client's decision and submits an ExecApproval
// op. A failed or undecodable response is treated as a denial, matching the Rust
// conservative default.
func awaitExecApproval(thread *core.CodexThread, approvalID, eventID string, respCh <-chan json.RawMessage) {
	decision := decodeDecision(<-respCh)
	turnID := eventID
	_, _ = thread.Submit(protocol.Op{
		Type:     protocol.OpExecApproval,
		ID:       approvalID,
		TurnID:   &turnID,
		Decision: &decision,
	})
}

// handlePatchApprovalRequest issues an elicitation/create request for an
// apply-patch approval and submits the decision back to the thread. Faithful
// port of handle_patch_approval_request.
func handlePatchApprovalRequest(
	sender *outgoingSender,
	thread *core.CodexThread,
	threadID string,
	toolCallID string,
	ev protocol.ApplyPatchApprovalRequestEvent,
	eventID string,
) {
	approvalID := ev.CallID

	var lines []string
	if ev.Reason != nil {
		lines = append(lines, *ev.Reason)
	}
	lines = append(lines, "Allow Codex to apply proposed code changes?")

	params := patchApprovalElicitParams{
		Message:         strings.Join(lines, "\n"),
		RequestedSchema: emptyObjectSchema(),
		ThreadID:        threadID,
		Elicitation:     "patch-approval",
		MCPToolCallID:   toolCallID,
		EventID:         eventID,
		CallID:          ev.CallID,
		Reason:          ev.Reason,
		GrantRoot:       ev.GrantRoot,
		Changes:         ev.Changes,
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return
	}

	respCh := sender.sendRequest(elicitationMethod, raw)
	go awaitPatchApproval(thread, approvalID, respCh)
}

// awaitPatchApproval waits for the client's decision and submits a PatchApproval
// op. A failed or undecodable response is treated as a denial.
func awaitPatchApproval(thread *core.CodexThread, approvalID string, respCh <-chan json.RawMessage) {
	decision := decodeDecision(<-respCh)
	_, _ = thread.Submit(protocol.Op{
		Type:     protocol.OpPatchApproval,
		ID:       approvalID,
		Decision: &decision,
	})
}

// decodeDecision parses an approvalResponse from raw, defaulting to a denial on
// any decode failure (the conservative reference default).
func decodeDecision(raw json.RawMessage) protocol.ReviewDecision {
	if len(raw) == 0 {
		return protocol.NewReviewDecisionDenied()
	}
	var resp approvalResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return protocol.NewReviewDecisionDenied()
	}
	if resp.Decision.Kind == "" {
		return protocol.NewReviewDecisionDenied()
	}
	return resp.Decision
}

// shellJoin joins command argv into a single shell-ish string. It quotes
// arguments containing whitespace, approximating shlex::try_join for display.
func shellJoin(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\n\"'\\") {
			quoted = append(quoted, "'"+strings.ReplaceAll(a, "'", `'\''`)+"'")
			continue
		}
		quoted = append(quoted, a)
	}
	return strings.Join(quoted, " ")
}
