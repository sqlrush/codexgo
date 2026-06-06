package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// ErrTurnAborted is the sentinel returned when a turn's sampling request is
// cancelled by an interrupt or shutdown. It mirrors the Rust `CodexErr::TurnAborted`
// and is reported to the client via a TurnAborted event rather than an Error event.
var ErrTurnAborted = errors.New("core: turn aborted")

// turnInput is a single piece of input handed to a turn: either a typed user
// input batch or a pre-built conversation response item. It is a reduced port of
// the Rust `TurnInput` enum (the turn-running subset).
type turnInput struct {
	// UserContent, when non-nil, is a batch of user-supplied input items.
	UserContent []protocol.UserInput
	// ClientID correlates a user-input batch with a client message id.
	ClientID *string
	// ResponseItem, when non-nil, is a pre-built conversation item to inject.
	ResponseItem *protocol.ResponseItem
}

// samplingResult is the outcome of a single model sampling request. It mirrors
// the Rust `SamplingRequestResult`.
type samplingResult struct {
	// NeedsFollowUp reports whether the turn must issue another model request
	// (e.g. because a tool call produced output the model must observe).
	NeedsFollowUp bool
	// LastAgentMessage is the most recent assistant text, if any.
	LastAgentMessage *string
}

// runTurn runs a single turn end-to-end against the session's [ModelClient]: it
// records the turn's input into history, then loops issuing model sampling
// requests, streaming deltas, dispatching tool calls, and folding tool outputs
// back into history until the model signals it is done (no follow-up needed) or
// the turn is cancelled. It returns the last assistant message, if any.
//
// runTurn is the Go analogue of the Rust `run_turn`, reduced to the turn-running
// subset: pre-sampling/auto compaction, skills/plugins injection, hooks, goals,
// and the mailbox/steer machinery are deferred (see STUB notes).
func runTurn(ctx context.Context, sess *Session, tc *TurnContext, input []turnInput) *string {
	// Record the turn's input items into shared history before the first sampling
	// request, mirroring the Rust record/drain ordering (turn-input subset).
	recordTurnInput(sess, input)

	// A2: pre-sampling auto-compaction. When the running token total has reached
	// the model's auto-compaction budget, replace history with a summary before
	// the first sampling request (port of run_pre_sampling_compact). Failures are
	// non-fatal — the turn proceeds on the un-compacted history.
	maybePreSamplingAutoCompact(ctx, sess, tc)

	// STUB: build_skills_and_plugins, run_pre_sampling_compact,
	// run_pending_session_start_hooks, run_hooks_and_record_inputs,
	// merge_connector_selection, and track_turn_resolved_config_analytics are
	// deferred to the skills/compaction/hooks/analytics area agents. We do record
	// the previous-turn settings so a later model switch can diff against them.
	sess.WithState(func(st *SessionState) {
		realtime := tc.RealtimeActive
		st.SetPreviousTurnSettings(&PreviousTurnSettings{
			Model:          tc.ModelSlug,
			RealtimeActive: &realtime,
		})
	})

	var lastAgentMessage *string
	for {
		// Honor cancellation before issuing the next request.
		if ctx.Err() != nil {
			return lastAgentMessage
		}

		out, err := runSamplingRequest(ctx, sess, tc)
		if err != nil {
			if errors.Is(err, ErrTurnAborted) || errors.Is(err, context.Canceled) {
				// Aborted turns are reported via a TurnAborted event by the task
				// runner, not an Error event.
				return lastAgentMessage
			}
			// Surface the error to the client and let the user continue.
			emitTurnError(sess, tc, err)
			return lastAgentMessage
		}

		if out.LastAgentMessage != nil {
			lastAgentMessage = out.LastAgentMessage
		}

		if !out.NeedsFollowUp {
			return lastAgentMessage
		}
		// Otherwise loop: history already carries the tool outputs the model
		// must observe on the next request.
	}
}

// recordTurnInput folds a turn's input items into shared conversation history.
// User-input batches are converted into a single user message; pre-built
// response items are recorded as-is. Mirrors the Rust record-pending-input path
// (turn-running subset).
func recordTurnInput(sess *Session, input []turnInput) {
	var items []protocol.ResponseItem
	for _, in := range input {
		switch {
		case in.ResponseItem != nil:
			items = append(items, *in.ResponseItem)
		case len(in.UserContent) > 0:
			if item, ok := userInputToResponseItem(in.UserContent); ok {
				items = append(items, item)
			}
		}
	}
	if len(items) > 0 {
		sess.RecordItems(items)
		// STUB: persistence of input items to the rollout recorder mirrors the
		// Rust record_conversation_items; we record into history here and let the
		// task runner persist a turn-context record. Per-item rollout persistence
		// is delegated to the rollout-reconstruction area.
	}
}

// userInputToResponseItem converts a batch of user input items into a single
// user-role message response item. Image/skill/mention items are reduced to
// their text surface (STUB: image and skill handling are deferred). Returns
// (item, false) when there is no text content to record.
func userInputToResponseItem(content []protocol.UserInput) (protocol.ResponseItem, bool) {
	var contentItems []protocol.ContentItem
	for _, ui := range content {
		switch ui.Type {
		case protocol.UserInputKindText:
			if ui.Text != "" {
				contentItems = append(contentItems, protocol.ContentItem{
					Type: protocol.ContentItemKindInputText,
					Text: ui.Text,
				})
			}
		case protocol.UserInputKindImage:
			if ui.ImageURL != "" {
				contentItems = append(contentItems, protocol.ContentItem{
					Type:     protocol.ContentItemKindInputImage,
					ImageURL: ui.ImageURL,
					Detail:   ui.Detail,
				})
			}
		default:
			// STUB: local_image, skill, and mention inputs are resolved by the
			// skills/injection area agents; the text surface (if any) is included.
			if ui.Text != "" {
				contentItems = append(contentItems, protocol.ContentItem{
					Type: protocol.ContentItemKindInputText,
					Text: ui.Text,
				})
			}
		}
	}
	if len(contentItems) == 0 {
		return protocol.ResponseItem{}, false
	}
	return protocol.ResponseItem{
		Type:    protocol.ResponseItemKindMessage,
		Role:    "user",
		Content: contentItems,
	}, true
}

// runSamplingRequest issues one model request and processes its event stream,
// emitting deltas/turn-item events, dispatching tool calls, and folding outputs
// back into history. It returns whether the model needs a follow-up request and
// the last assistant message observed. Mirrors the Rust `run_sampling_request`
// + `try_run_sampling_request` (reduced: no retry loop, plan-mode parsers, or
// extension contributors).
//
// STUB: stream retry/backoff, plan-mode streaming, citation handling, server
// model warnings, model verifications, and the turn-diff tracker are deferred.
func runSamplingRequest(ctx context.Context, sess *Session, tc *TurnContext) (samplingResult, error) {
	prompt := buildPrompt(sess, tc)

	stream, err := sess.services.ModelClient.Stream(ctx, prompt)
	if err != nil {
		return samplingResult{}, fmt.Errorf("core: model stream failed: %w", err)
	}

	var (
		needsFollowUp       bool
		lastAgentMessage    *string
		toolOutputs         []protocol.ResponseItem
		activeItem          *protocol.ResponseItem
		shouldEmitTokens    bool
		completedTokenUsage *protocol.TokenUsage
	)

	for {
		select {
		case <-ctx.Done():
			return samplingResult{}, ErrTurnAborted
		case ev, ok := <-stream:
			if !ok {
				// Stream closed without a Completed event.
				return samplingResult{}, fmt.Errorf("core: stream closed before response.completed")
			}
			done, ferr := handleStreamEvent(ctx, sess, tc, ev, &streamState{
				needsFollowUp:       &needsFollowUp,
				lastAgentMessage:    &lastAgentMessage,
				toolOutputs:         &toolOutputs,
				activeItem:          &activeItem,
				shouldEmitTokens:    &shouldEmitTokens,
				completedTokenUsage: &completedTokenUsage,
			})
			if ferr != nil {
				return samplingResult{}, ferr
			}
			if done {
				// Completed: fold tool outputs into history, account tokens, emit.
				if len(toolOutputs) > 0 {
					sess.RecordItems(toolOutputs)
				}
				if shouldEmitTokens {
					accountAndEmitTokenCount(sess, tc, completedTokenUsage)
				}
				if ctx.Err() != nil {
					return samplingResult{}, ErrTurnAborted
				}
				return samplingResult{NeedsFollowUp: needsFollowUp, LastAgentMessage: lastAgentMessage}, nil
			}
		}
	}
}

// streamState bundles the mutable accumulators threaded through stream-event
// handling so handleStreamEvent stays small and testable.
type streamState struct {
	needsFollowUp       *bool
	lastAgentMessage    **string
	toolOutputs         *[]protocol.ResponseItem
	activeItem          **protocol.ResponseItem
	shouldEmitTokens    *bool
	completedTokenUsage **protocol.TokenUsage
}

// handleStreamEvent processes a single [api.ResponseEvent], emitting protocol
// events and accumulating state. It returns done=true when the stream's
// Completed event has been observed. Mirrors the Rust stream `match event` arm
// (reduced surface).
func handleStreamEvent(ctx context.Context, sess *Session, tc *TurnContext, ev api.ResponseEvent, st *streamState) (done bool, err error) {
	switch ev.Kind {
	case api.ResponseEventCreated:
		return false, nil

	case api.ResponseEventOutputItemAdded:
		if ev.Item != nil {
			item := *ev.Item
			*st.activeItem = &item
			emitItemStarted(sess, tc, &item)
		}
		return false, nil

	case api.ResponseEventOutputTextDelta:
		emitAgentMessageDelta(sess, tc, *st.activeItem, ev.Delta)
		return false, nil

	case api.ResponseEventReasoningSummaryDelta:
		emitReasoningSummaryDelta(sess, tc, *st.activeItem, ev.Delta, ev.SummaryIndex)
		return false, nil

	case api.ResponseEventReasoningContentDelta:
		emitReasoningRawDelta(sess, tc, *st.activeItem, ev.Delta, ev.ContentIndex)
		return false, nil

	case api.ResponseEventReasoningSummaryPartAdded:
		emitReasoningSectionBreak(sess, tc, *st.activeItem, ev.SummaryIndex)
		return false, nil

	case api.ResponseEventOutputItemDone:
		if ev.Item != nil {
			if perr := handleOutputItemDone(ctx, sess, tc, *ev.Item, st); perr != nil {
				return false, perr
			}
			*st.activeItem = nil
		}
		return false, nil

	case api.ResponseEventRateLimits:
		if ev.RateLimits != nil {
			sess.WithState(func(s *SessionState) { s.SetRateLimits(*ev.RateLimits) })
		}
		*st.shouldEmitTokens = true
		return false, nil

	case api.ResponseEventServerReasoningIncluded:
		sess.WithState(func(s *SessionState) { s.SetServerReasoningIncluded(ev.ReasoningIncluded) })
		return false, nil

	case api.ResponseEventCompleted:
		*st.completedTokenUsage = ev.TokenUsage
		*st.shouldEmitTokens = true
		if ev.EndTurn != nil && !*ev.EndTurn {
			*st.needsFollowUp = true
		}
		return true, nil

	default:
		// ServerModel, ModelVerifications, ModelsEtag, ToolCallInputDelta are
		// STUBbed: their handling (server-model warnings, verification events,
		// catalog refresh, tool-argument diff streaming) is deferred.
		return false, nil
	}
}
