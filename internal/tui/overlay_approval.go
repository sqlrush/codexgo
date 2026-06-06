package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ApprovalKind discriminates an [ApprovalRequest].
type ApprovalKind int

const (
	// ApprovalExec is a command-execution approval.
	ApprovalExec ApprovalKind = iota
	// ApprovalPatch is an apply-patch approval.
	ApprovalPatch
	// ApprovalPermissions is a permissions-grant request.
	ApprovalPermissions
	// ApprovalMcpElicitation is an MCP server elicitation prompt.
	ApprovalMcpElicitation
)

// ApprovalRequest is an agent request that needs user approval.
//
// Port of approval_overlay.rs ApprovalRequest (Exec / ApplyPatch / Permissions /
// McpElicitation), reduced to the fields the behavioral port renders and routes.
type ApprovalRequest struct {
	Kind        ApprovalKind
	ThreadID    string
	ThreadLabel string

	// ID is the approval id for Exec/Patch; CallID for Permissions.
	ID string
	// Command is the exec command argv (Exec).
	Command []string
	// Reason is an optional model-supplied justification.
	Reason string
	// Cwd is the patch working directory (Patch).
	Cwd string
	// PermissionRule is a human-readable description of the requested rule.
	PermissionRule string

	// Permissions is the opaque granted-profile payload echoed back on grant.
	Permissions json.RawMessage

	// ServerName / RequestID identify an MCP elicitation.
	ServerName string
	RequestID  string
	// Message is the elicitation prompt body.
	Message string
}

// approvalDecisionKind discriminates the decision an option carries.
type approvalDecisionKind int

const (
	decExec approvalDecisionKind = iota
	decPatch
	decPermissions
	decElicitation
)

// permissionsDecision enumerates the permissions-grant choices.
//
// Port of approval_overlay.rs PermissionsDecision.
type permissionsDecision int

const (
	grantForTurn permissionsDecision = iota
	grantForTurnStrict
	grantForSession
	denyPermissions
)

// approvalOption is one selectable answer in the approval modal.
//
// Port of approval_overlay.rs ApprovalOption.
type approvalOption struct {
	label     string
	kind      approvalDecisionKind
	execDec   string // ReviewDecisionKind value for exec/patch
	permsDec  permissionsDecision
	elicitDec string // McpServerElicitationAction value
	shortcuts []string
}

// ApprovalOverlay is the modal asking the user to approve or deny one or more
// requests. Requests beyond the first are queued and shown in turn.
//
// Port of approval_overlay.rs ApprovalOverlay. The list rendering/navigation is
// delegated to [ListSelectionOverlay]; this type owns request queueing, decision
// routing, and shortcut handling.
type ApprovalOverlay struct {
	current  *ApprovalRequest
	queue    []ApprovalRequest
	sender   *AppEventSender
	list     *ListSelectionOverlay
	options  []approvalOption
	complete bool // current request answered
	done     bool // overlay finished
	title    string
	header   []string
	// pending holds a deferred cancel command for the OnCtrlC path (drained
	// via PendingCmd by the overlay stack).
	pending tea.Cmd
}

// NewApprovalOverlay builds the overlay for an initial request.
//
// Port of ApprovalOverlay::new.
func NewApprovalOverlay(request ApprovalRequest, sender *AppEventSender) *ApprovalOverlay {
	o := &ApprovalOverlay{sender: sender}
	o.setCurrent(request)
	return o
}

// EnqueueRequest queues an additional request to show after the current one.
//
// Port of ApprovalOverlay::enqueue_request.
func (o *ApprovalOverlay) EnqueueRequest(req ApprovalRequest) {
	o.queue = append(o.queue, req)
}

func (o *ApprovalOverlay) setCurrent(request ApprovalRequest) {
	o.complete = false
	o.current = &request
	o.options, o.title = buildApprovalOptions(request)
	o.header = buildApprovalHeader(request)

	items := make([]SelectionItem, len(o.options))
	for i, opt := range o.options {
		shortcut := ""
		if len(opt.shortcuts) > 0 {
			shortcut = opt.shortcuts[0]
		}
		items[i] = SelectionItem{Name: opt.label, DisplayShortcut: shortcut}
	}
	header := append([]string{o.title, ""}, o.header...)
	o.list = NewListSelectionOverlay(SelectionViewParams{
		HeaderLines:     header,
		Items:           items,
		FooterHint:      "Press Enter to confirm or Esc to cancel",
		InitialSelected: -1,
	}, o.sender)
}

// applySelection records the decision and returns a command that delivers it.
// The sender call is DEFERRED into the returned tea.Cmd: routing a decision
// calls AppEventSender.Send → the unbuffered Program.Send, which deadlocks if
// invoked synchronously inside Update (same hazard fixed for the list overlay).
func (o *ApprovalOverlay) applySelection(idx int) tea.Cmd {
	if o.complete || o.current == nil {
		return nil
	}
	if idx < 0 || idx >= len(o.options) {
		return nil
	}
	cmd := o.routeDecision(o.options[idx])
	o.complete = true
	o.advanceQueue()
	return cmd
}

func (o *ApprovalOverlay) routeDecision(opt approvalOption) tea.Cmd {
	req := o.current
	s := o.sender
	switch opt.kind {
	case decExec:
		tid, id, dec := req.ThreadID, req.ID, opt.execDec
		return func() tea.Msg { s.ExecApproval(tid, id, dec); return nil }
	case decPatch:
		tid, id, dec := req.ThreadID, req.ID, opt.execDec
		return func() tea.Msg { s.PatchApproval(tid, id, dec); return nil }
	case decPermissions:
		return o.routePermissions(req, opt.permsDec)
	case decElicitation:
		tid, sn, rid, dec := req.ThreadID, req.ServerName, req.RequestID, opt.elicitDec
		return func() tea.Msg { s.ResolveElicitation(tid, sn, rid, dec, nil); return nil }
	}
	return nil
}

func (o *ApprovalOverlay) routePermissions(req *ApprovalRequest, dec permissionsDecision) tea.Cmd {
	scope := "turn"
	strict := false
	var granted json.RawMessage
	switch dec {
	case grantForTurn:
		granted = req.Permissions
	case grantForTurnStrict:
		granted = req.Permissions
		strict = true
	case grantForSession:
		granted = req.Permissions
		scope = "session"
	case denyPermissions:
		granted = nil
	}
	payload := map[string]any{
		"scope":              scope,
		"strict_auto_review": strict,
	}
	if granted != nil {
		payload["permissions"] = granted
	}
	raw, _ := json.Marshal(payload)
	s := o.sender
	tid, id := req.ThreadID, req.ID
	return func() tea.Msg { s.RequestPermissionsResponse(tid, id, raw); return nil }
}

func (o *ApprovalOverlay) advanceQueue() {
	if len(o.queue) > 0 {
		next := o.queue[len(o.queue)-1]
		o.queue = o.queue[:len(o.queue)-1]
		o.setCurrent(next)
		return
	}
	o.done = true
}

// cancelCurrent answers the current request with its safe-abort decision and
// finishes the overlay (discarding the queue).
//
// Port of ApprovalOverlay::cancel_current_request.
func (o *ApprovalOverlay) cancelCurrent() tea.Cmd {
	if o.done {
		return nil
	}
	var cmd tea.Cmd
	if !o.complete && o.current != nil {
		s := o.sender
		switch o.current.Kind {
		case ApprovalExec:
			tid, id := o.current.ThreadID, o.current.ID
			cmd = func() tea.Msg { s.ExecApproval(tid, id, string(reviewDecisionAbort)); return nil }
		case ApprovalPatch:
			tid, id := o.current.ThreadID, o.current.ID
			cmd = func() tea.Msg { s.PatchApproval(tid, id, string(reviewDecisionAbort)); return nil }
		case ApprovalPermissions:
			cmd = o.routePermissions(o.current, denyPermissions)
		case ApprovalMcpElicitation:
			tid, sn, rid := o.current.ThreadID, o.current.ServerName, o.current.RequestID
			cmd = func() tea.Msg { s.ResolveElicitation(tid, sn, rid, elicitationCancel, nil); return nil }
		}
	}
	o.queue = nil
	o.done = true
	return cmd
}

// HandleKey implements OverlayView. Decision/cancel sender calls are deferred
// into the returned command (see applySelection).
func (o *ApprovalOverlay) HandleKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	if key == "esc" {
		return o.cancelCurrent()
	}
	// Per-option shortcut match before list navigation.
	for i, opt := range o.options {
		for _, sc := range opt.shortcuts {
			if sc == key {
				return o.applySelection(i)
			}
		}
	}
	cmd := o.list.HandleKey(msg)
	if idx, ok := o.list.TakeLastSelectedIndex(); ok {
		if ac := o.applySelection(idx); ac != nil {
			cmd = tea.Batch(cmd, ac)
		}
	}
	return cmd
}

// OnCtrlC implements overlayCtrlC. The cancel decision is deferred into
// PendingCmd (the overlay stack drains it after the cancel-key branch).
func (o *ApprovalOverlay) OnCtrlC() CancellationEvent {
	o.pending = o.cancelCurrent()
	return CancellationHandled
}

// PendingCmd implements the overlay-stack deferred-callback seam.
func (o *ApprovalOverlay) PendingCmd() tea.Cmd {
	c := o.pending
	o.pending = nil
	return c
}

// IsComplete implements OverlayView.
func (o *ApprovalOverlay) IsComplete() bool { return o.done }

// TerminalTitleRequiresAction implements overlayTerminalTitle.
func (o *ApprovalOverlay) TerminalTitleRequiresAction() bool { return true }

// DesiredHeight implements OverlayView.
func (o *ApprovalOverlay) DesiredHeight(width int) int { return o.list.DesiredHeight(width) }

// View implements OverlayView.
func (o *ApprovalOverlay) View(theme Theme, area Rect) string { return o.list.View(theme, area) }

// reviewDecision* mirror protocol.ReviewDecisionKind string values without
// importing the protocol package into the option table.
const (
	reviewDecisionApproved           = "approved"
	reviewDecisionApprovedForSession = "approved_for_session"
	reviewDecisionDenied             = "denied"
	reviewDecisionAbort              = "abort"
)

const (
	elicitationAccept  = "accept"
	elicitationDecline = "decline"
	elicitationCancel  = "cancel"
)

// buildApprovalOptions builds the option table and title for a request.
//
// Port of ApprovalOverlay::build_options + exec/patch/permissions/elicitation
// option helpers (default available-decisions set).
func buildApprovalOptions(req ApprovalRequest) ([]approvalOption, string) {
	switch req.Kind {
	case ApprovalExec:
		return execOptions(), "Would you like to run the following command?"
	case ApprovalPatch:
		return patchOptions(), "Would you like to make the following edits?"
	case ApprovalPermissions:
		return permissionsOptions(), "Would you like to grant these permissions?"
	case ApprovalMcpElicitation:
		return elicitationOptions(), fmt.Sprintf("%s needs your approval.", req.ServerName)
	default:
		return nil, ""
	}
}

func execOptions() []approvalOption {
	return []approvalOption{
		{label: "Yes, proceed", kind: decExec, execDec: reviewDecisionApproved, shortcuts: []string{"y"}},
		{label: "Yes, and don't ask again for this command in this session", kind: decExec, execDec: reviewDecisionApprovedForSession, shortcuts: []string{"a"}},
		{label: "No, continue without running it", kind: decExec, execDec: reviewDecisionDenied, shortcuts: []string{"n"}},
		{label: "No, and tell Codex what to do differently", kind: decExec, execDec: reviewDecisionAbort, shortcuts: nil},
	}
}

func patchOptions() []approvalOption {
	return []approvalOption{
		{label: "Yes, proceed", kind: decPatch, execDec: reviewDecisionApproved, shortcuts: []string{"y"}},
		{label: "Yes, and don't ask again for these files", kind: decPatch, execDec: reviewDecisionApprovedForSession, shortcuts: []string{"a"}},
		{label: "No, and tell Codex what to do differently", kind: decPatch, execDec: reviewDecisionAbort, shortcuts: nil},
	}
}

func permissionsOptions() []approvalOption {
	return []approvalOption{
		{label: "Yes, grant these permissions for this turn", kind: decPermissions, permsDec: grantForTurn, shortcuts: []string{"y"}},
		{label: "Yes, grant for this turn with strict auto review", kind: decPermissions, permsDec: grantForTurnStrict, shortcuts: []string{"r"}},
		{label: "Yes, grant these permissions for this session", kind: decPermissions, permsDec: grantForSession, shortcuts: []string{"a"}},
		{label: "No, continue without permissions", kind: decPermissions, permsDec: denyPermissions, shortcuts: nil},
	}
}

// elicitationOptions builds MCP elicitation options. Esc is always mapped to
// cancel (a hard contract so dismissal never silently maps to "continue without
// info").
//
// Port of approval_overlay.rs elicitation_options.
func elicitationOptions() []approvalOption {
	return []approvalOption{
		{label: "Yes, provide the requested info", kind: decElicitation, elicitDec: elicitationAccept, shortcuts: []string{"y"}},
		{label: "No, but continue without it", kind: decElicitation, elicitDec: elicitationDecline, shortcuts: []string{"n"}},
		{label: "Cancel this request", kind: decElicitation, elicitDec: elicitationCancel, shortcuts: []string{"c"}},
	}
}

// buildApprovalHeader builds the request-specific header lines.
//
// Port of approval_overlay.rs build_header (text-only; no syntax highlighting).
func buildApprovalHeader(req ApprovalRequest) []string {
	var lines []string
	if req.ThreadLabel != "" {
		lines = append(lines, "Thread: "+req.ThreadLabel, "")
	}
	if req.Reason != "" {
		lines = append(lines, "Reason: "+req.Reason, "")
	}
	switch req.Kind {
	case ApprovalExec:
		if req.PermissionRule != "" {
			lines = append(lines, "Permission rule: "+req.PermissionRule, "")
		}
		lines = append(lines, "$ "+strings.Join(req.Command, " "))
	case ApprovalPermissions:
		if req.PermissionRule != "" {
			lines = append(lines, "Permission rule: "+req.PermissionRule)
		}
	case ApprovalMcpElicitation:
		lines = append(lines, "Server: "+req.ServerName, "", req.Message)
	}
	return lines
}

var _ OverlayView = (*ApprovalOverlay)(nil)
