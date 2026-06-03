package tui

import (
	"encoding/json"

	tea "github.com/charmbracelet/bubbletea"
)

// AppEventSender is the convenience sender for app events, the bubbletea analogue
// of codex-rs/tui/src/app_event_sender.rs `AppEventSender`.
//
// In the Rust port the sender wraps an mpsc channel. In bubbletea two delivery
// paths exist:
//
//   - Synchronous, from inside Update: return an [AppEvent] (or a [tea.Cmd] that
//     produces one) and the runtime re-delivers it. Use the package-level Cmd
//     helpers ([CodexOpCmd], [ExitCmd], ...) for this path.
//   - Asynchronous, from a goroutine (e.g. the engine pump): call [Send], which
//     forwards through the running [tea.Program].
//
// AppEventSender is safe for concurrent use once a program is attached.
type AppEventSender struct {
	// send forwards a message to the running program. It is nil until [Attach]
	// is called; Send is a no-op while unattached so early callers do not panic.
	send func(tea.Msg)
}

// NewAppEventSender constructs an unattached sender. Attach a running program
// with [AppEventSender.Attach] before async sends take effect.
func NewAppEventSender() *AppEventSender {
	return &AppEventSender{}
}

// Attach binds the sender to a running [tea.Program] so async [Send] calls are
// delivered into the model. Passing nil detaches.
func (s *AppEventSender) Attach(p *tea.Program) {
	if p == nil {
		s.send = nil
		return
	}
	s.send = p.Send
}

// attachFunc binds the sender to an arbitrary send function. Used by tests.
func (s *AppEventSender) attachFunc(send func(tea.Msg)) {
	s.send = send
}

// Send forwards an app event asynchronously to the running program. It is safe
// to call from any goroutine. If no program is attached the event is dropped
// (matching the Rust sender's swallow-and-log-on-failure behavior, minus the
// logging which the caller can add).
func (s *AppEventSender) Send(ev AppEvent) {
	if s.send == nil {
		return
	}
	s.send(ev)
}

// SendMsg forwards an arbitrary [tea.Msg] (e.g. a [CoreEventMsg]) asynchronously.
func (s *AppEventSender) SendMsg(msg tea.Msg) {
	if s.send == nil {
		return
	}
	s.send(msg)
}

// Interrupt forwards an interrupt op to the engine.
//
// Port of AppEventSender::interrupt.
func (s *AppEventSender) Interrupt() {
	s.Send(CodexOpEvent{Command: NewInterruptCommand()})
}

// Compact forwards a compact op to the engine.
//
// Port of AppEventSender::compact.
func (s *AppEventSender) Compact() {
	s.Send(CodexOpEvent{Command: NewCompactCommand()})
}

// SetThreadName forwards a rename op to the engine.
//
// Port of AppEventSender::set_thread_name.
func (s *AppEventSender) SetThreadName(name string) {
	s.Send(CodexOpEvent{Command: NewSetThreadNameCommand(name)})
}

// ExecApproval forwards an exec-approval response to the given thread.
//
// Port of AppEventSender::exec_approval.
func (s *AppEventSender) ExecApproval(threadID, id, decision string) {
	s.Send(SubmitThreadOpEvent{ThreadID: threadID, Command: NewExecApprovalCommand(id, "", decision)})
}

// PatchApproval forwards a patch-approval response to the given thread.
//
// Port of AppEventSender::patch_approval.
func (s *AppEventSender) PatchApproval(threadID, id, decision string) {
	s.Send(SubmitThreadOpEvent{ThreadID: threadID, Command: NewPatchApprovalCommand(id, decision)})
}

// RequestPermissionsResponse forwards a permissions-grant response to the given
// thread. The response payload carries the granted profile, scope, and strict
// auto-review flag as opaque JSON.
//
// Port of AppEventSender::request_permissions_response.
func (s *AppEventSender) RequestPermissionsResponse(threadID, callID string, response json.RawMessage) {
	s.Send(SubmitThreadOpEvent{
		ThreadID: threadID,
		Command: AppCommand{
			Kind:     AppCommandRequestPermissionsResponse,
			ID:       callID,
			Response: response,
		},
	})
}

// ResolveElicitation forwards an MCP elicitation decision to the given thread.
//
// Port of AppEventSender::resolve_elicitation.
func (s *AppEventSender) ResolveElicitation(threadID, serverName, requestID, action string, content json.RawMessage) {
	payload := map[string]any{
		"server_name": serverName,
		"request_id":  requestID,
		"action":      action,
	}
	if content != nil {
		payload["content"] = content
	}
	raw, _ := json.Marshal(payload)
	s.Send(SubmitThreadOpEvent{
		ThreadID: threadID,
		Command: AppCommand{
			Kind:     AppCommandResolveElicitation,
			Response: raw,
		},
	})
}

// UserInputAnswer forwards a tool request_user_input answer payload to the
// engine for the given turn.
//
// Port of AppEventSender::user_input_answer.
func (s *AppEventSender) UserInputAnswer(turnID string, response json.RawMessage) {
	s.Send(CodexOpEvent{Command: AppCommand{
		Kind:     AppCommandUserInputAnswer,
		TurnID:   turnID,
		Response: response,
	}})
}

// --- tea.Cmd helpers (synchronous path) -------------------------------------

// EventCmd wraps an [AppEvent] as a [tea.Cmd] for return from Update.
func EventCmd(ev AppEvent) tea.Cmd {
	return func() tea.Msg { return ev }
}

// CodexOpCmd returns a command that forwards an engine op.
func CodexOpCmd(cmd AppCommand) tea.Cmd {
	return EventCmd(CodexOpEvent{Command: cmd})
}

// ExitCmd returns a command that requests application exit.
func ExitCmd(mode ExitMode) tea.Cmd {
	return EventCmd(ExitEvent{Mode: mode})
}

// SubmitUserMessageCmd returns a command that submits a plain user message.
func SubmitUserMessageCmd(text string) tea.Cmd {
	return EventCmd(SubmitUserMessageEvent{Text: text})
}
