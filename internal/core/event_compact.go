package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
)

// This file ports `compact.rs`: inline (Memento-style) auto-compaction.
//
// Compaction summarizes the conversation so far and replaces history with a
// compact summary plus the most recent user messages, so a long thread can keep
// running inside the model context window. It is driven by a token threshold
// (see auto_compact_window.go for the accounting) and runs at one of three
// phases relative to a turn: pre-turn, mid-turn, or as a standalone manual turn.
//
// The flow mirrors the Rust `run_compact_task_inner_impl`:
//  1. emit an ItemStarted event for a synthetic ContextCompaction turn item;
//  2. record the compaction prompt as a user input into a CLONE of history;
//  3. stream one (retryable) model request to completion, recording the model's
//     summary output into shared history;
//  4. rebuild a compact replacement history from the summary + recent user
//     messages (build_compacted_history), optionally re-injecting initial
//     context before the last real user message (mid-turn compaction);
//  5. replace shared history with the compact one, recompute token usage, persist
//     a `compacted` rollout line, and emit ItemCompleted + ContextCompacted +
//     a long-thread Warning event.
//
// DEFERRED (clearly-marked stubs below):
//   - compact_remote / compact_v2 (remote summarization task). Only the inline
//     Responses-API path is implemented; shouldUseRemoteCompactTask always
//     reports false here.
//   - analytics tracking (CompactionAnalyticsAttempt) — folded into no-op hooks.
//   - pre/post-compact hook execution (run_pre/post_compact_hooks) — invoked via
//     the injected HooksEngine as best-effort, non-blocking events.
//   - reference-context-item baseline tracking, turn-metadata header, inference
//     trace context, and rate-limit/server-reasoning side effects of the stream.

// SummarizationPrompt is the default inline-compaction prompt handed to the model
// when a turn does not override it. It is the verbatim port of the Rust
// `compact::SUMMARIZATION_PROMPT` (templates/compact/prompt.md).
const SummarizationPrompt = `You are performing a CONTEXT CHECKPOINT COMPACTION. Create a handoff summary for another LLM that will resume the task.

Include:
- Current progress and key decisions made
- Important context, constraints, or user preferences
- What remains to be done (clear next steps)
- Any critical data, examples, or references needed to continue

Be concise, structured, and focused on helping the next LLM seamlessly continue the work.`

// SummaryPrefix is prepended to the model's final assistant message to form the
// compaction summary. It is the verbatim port of the Rust `compact::SUMMARY_PREFIX`
// (templates/compact/summary_prefix.md).
const SummaryPrefix = `Another language model started to solve this problem and produced a summary of its thinking process. You also have access to the state of the tools that were used by that language model. Use this to build on the work that has already been done and avoid duplicating work. Here is the summary produced by the other language model, use the information in this summary to assist with your own analysis:`

// compactUserMessageMaxTokens bounds how many tokens of recent user messages are
// preserved in the rebuilt compact history. Mirrors the Rust
// `COMPACT_USER_MESSAGE_MAX_TOKENS`.
const compactUserMessageMaxTokens = 20_000

// longThreadWarningMessage is the verbatim heads-up emitted after each
// compaction, mirroring the Rust `WarningEvent` text in
// run_compact_task_inner_impl.
const longThreadWarningMessage = "Heads up: Long threads and multiple compactions can cause the model to be less accurate. Start a new thread when possible to keep threads small and targeted."

// noSummaryAvailablePlaceholder substitutes for an empty summary in the rebuilt
// history, mirroring the Rust "(no summary available)" fallback.
const noSummaryAvailablePlaceholder = "(no summary available)"

// streamClosedBeforeCompletedMsg is the error surfaced when a compaction stream
// ends without a Completed event, mirroring the Rust
// `CodexErr::Stream("stream closed before response.completed", None)`.
const streamClosedBeforeCompletedMsg = "stream closed before response.completed"

// InitialContextInjection controls whether compaction's replacement history must
// re-include the session's initial context. It is a faithful port of the Rust
// `InitialContextInjection` enum.
//
// Pre-turn/manual compaction uses DoNotInject: history is replaced with the
// summary and the reference baseline is cleared, so the next regular turn fully
// reinjects initial context. Mid-turn compaction uses BeforeLastUserMessage:
// the model is trained to see the compaction summary as the last item, so initial
// context is spliced in just above the last real user message.
type InitialContextInjection int

const (
	// InjectBeforeLastUserMessage splices initial context before the last real
	// user message (mid-turn compaction).
	InjectBeforeLastUserMessage InitialContextInjection = iota
	// DoNotInjectInitialContext replaces history with only the summary and recent
	// user messages (pre-turn / manual compaction).
	DoNotInjectInitialContext
)

// shouldUseRemoteCompactTask reports whether remote compaction should be used for
// a turn. The inline port always returns false.
//
// STUB: remote compaction (compact_remote / compact_v2) is deferred; the model
// provider's supports_remote_compaction flag is not consulted here.
func shouldUseRemoteCompactTask(_ *TurnContext) bool { return false }

// runInlineAutoCompactTask runs an inline auto-compaction using the turn's
// configured compaction prompt. It is the Go analogue of the Rust
// `run_inline_auto_compact_task` (the auto-triggered entry point used by the
// pre/mid/post-turn token-threshold checks).
func runInlineAutoCompactTask(
	ctx context.Context,
	sess *Session,
	tc *TurnContext,
	injection InitialContextInjection,
) error {
	prompt := tc.CompactPromptText()
	input := []protocol.UserInput{{
		Type: protocol.UserInputKindText,
		Text: prompt,
		// Compaction prompt is synthesized; no UI element ranges to preserve.
		TextElements: nil,
	}}
	return runCompactTaskInner(ctx, sess, tc, input, injection)
}

// runCompactTask runs a standalone, user-requested compaction turn. It emits a
// TurnStarted event first (mirroring the Rust `run_compact_task`) and always uses
// DoNotInjectInitialContext.
func runCompactTask(
	ctx context.Context,
	sess *Session,
	tc *TurnContext,
	input []protocol.UserInput,
) error {
	startedAt := nowSeconds()
	sess.SendEvent(tc.SubID, protocol.EventMsg{
		Type: protocol.EventMsgKindTurnStarted,
		TurnStarted: &protocol.TurnStartedEvent{
			TurnID:             tc.SubID,
			TraceID:            tc.TraceID,
			StartedAt:          &startedAt,
			ModelContextWindow: tc.ModelContextWindow,
			CollaborationMode:  tc.CollaborationMode.Mode,
		},
	})
	return runCompactTaskInner(ctx, sess, tc, input, DoNotInjectInitialContext)
}

// runCompactTaskInner orchestrates a single compaction turn: pre/post hooks
// around the streaming summarization and history replacement. It is the Go
// analogue of the Rust `run_compact_task_inner` (with the analytics attempt
// reduced to the injected hooks). It returns the summary suffix on success.
//
// STUB: CompactionAnalyticsAttempt (begin/track) is omitted; the trigger/reason/
// phase taxonomy is not threaded through. Hook stop-decisions are not honored
// (the HooksEngine here is fire-and-forget); a real implementation would abort on
// a PreCompact "stopped" outcome.
func runCompactTaskInner(
	ctx context.Context,
	sess *Session,
	tc *TurnContext,
	input []protocol.UserInput,
	injection InitialContextInjection,
) error {
	fireCompactHook(ctx, sess, "pre_compact")

	_, err := runCompactTaskImpl(ctx, sess, tc, input, injection)
	if err == nil {
		fireCompactHook(ctx, sess, "post_compact")
	}
	return err
}

// fireCompactHook fires a compaction lifecycle hook best-effort. The HooksEngine
// is fire-and-forget here (errors are swallowed) because hook stop-decisions are
// deferred (see runCompactTaskInner STUB note).
func fireCompactHook(ctx context.Context, sess *Session, phase string) {
	if sess.services.HooksEngine == nil {
		return
	}
	// STUB: PreCompact/PostCompact are not first-class HookEvents in the
	// turn-running subset; we reuse HookTurnEnd as the closest lifecycle point and
	// pass the phase as the payload so a concrete engine can disambiguate.
	_ = sess.services.HooksEngine.Fire(ctx, HookTurnEnd, map[string]string{"compaction_phase": phase})
}

// runCompactTaskImpl performs the streaming summarization and history rebuild. It
// is the Go analogue of the Rust `run_compact_task_inner_impl` and returns the
// summary suffix (the model's last assistant message) on success.
func runCompactTaskImpl(
	ctx context.Context,
	sess *Session,
	tc *TurnContext,
	input []protocol.UserInput,
	injection InitialContextInjection,
) (string, error) {
	// 1. Synthetic ContextCompaction turn item: emit ItemStarted up front.
	compactionItem := newContextCompactionTurnItem()
	emitTurnItemStarted(sess, tc, compactionItem)

	// 2. Build the compaction request on a CLONE of history so a failed/aborted
	//    compaction does not corrupt the live conversation. The prompt is recorded
	//    as a user message at the tail.
	cloned := sess.HistoryItems()
	if promptItem, ok := userInputToResponseItem(input); ok {
		cloned = append(cloned, promptItem)
	}

	// 3. Stream one model request to completion (with retry-on-trim for context
	//    overflow), recording the summary output into SHARED history.
	if err := drainCompactToCompleted(ctx, sess, tc, cloned); err != nil {
		// Aborted/interrupted compaction is reported by the caller; other errors
		// surface as an Error event to the client (mirroring the Rust error arm).
		if !errors.Is(err, ErrTurnAborted) && !errors.Is(err, context.Canceled) {
			emitTurnError(sess, tc, err)
		}
		return "", err
	}

	// 4. Rebuild the compact replacement history from the summary + recent users.
	historyItems := sess.HistoryItems()
	summarySuffix := getLastAssistantMessageFromTurn(historyItems)
	summaryText := SummaryPrefix + "\n" + summarySuffix
	userMessages := collectUserMessages(historyItems)

	newHistory := buildCompactedHistory(nil, userMessages, summaryText)

	var referenceContextItem *rollout.TurnContextItem
	if injection == InjectBeforeLastUserMessage {
		initialContext := sess.buildInitialContext(tc)
		newHistory = insertInitialContextBeforeLastRealUserOrSummary(newHistory, initialContext)
		if item, err := tc.ToTurnContextItem(); err == nil {
			referenceContextItem = &item
		}
	}

	// 5. Replace shared history, recompute token usage, persist + emit.
	replacement := append([]protocol.ResponseItem(nil), newHistory...)
	compacted := rollout.CompactedItem{
		Message:            summaryText,
		ReplacementHistory: &replacement,
	}
	sess.replaceCompactedHistory(ctx, newHistory, referenceContextItem, compacted)
	sess.recomputeTokenUsage(tc)

	emitTurnItemCompleted(sess, tc, compactionItem)
	sess.SendEvent(tc.SubID, protocol.EventMsg{
		Type:             protocol.EventMsgKindContextCompacted,
		ContextCompacted: &protocol.ContextCompactedEvent{},
	})
	sess.SendEvent(tc.SubID, protocol.EventMsg{
		Type:    protocol.EventMsgKindWarning,
		Warning: &protocol.WarningEvent{Message: longThreadWarningMessage},
	})

	return summarySuffix, nil
}

// drainCompactToCompleted streams one compaction request to completion against a
// CLONE of history (passed as turnHistory), recording each completed output item
// into SHARED history and folding the final token usage in. On a context-window
// overflow it trims the oldest item from the cloned history and retries; with a
// single item left it gives up. It is the Go analogue of the Rust retry loop in
// run_compact_task_inner_impl plus drain_to_completed.
//
// STUB: stream_max_retries backoff (general transient errors), turn-metadata
// header, and inference-trace context are deferred; transient stream errors are
// surfaced immediately rather than retried.
func drainCompactToCompleted(
	ctx context.Context,
	sess *Session,
	tc *TurnContext,
	turnHistory []protocol.ResponseItem,
) error {
	history := append([]protocol.ResponseItem(nil), turnHistory...)
	for {
		if ctx.Err() != nil {
			return ErrTurnAborted
		}
		prompt := Prompt{
			Input: append([]protocol.ResponseItem(nil), history...),
			Tools: nil,
		}
		if tc.BaseInstructions != "" {
			bi := tc.BaseInstructions
			prompt.BaseInstructionsOverride = &bi
		}

		err := drainCompactStreamOnce(ctx, sess, tc, prompt)
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrTurnAborted) || errors.Is(err, context.Canceled) {
			return ErrTurnAborted
		}
		if errors.Is(err, errContextWindowExceeded) {
			if len(history) > 1 {
				// Trim from the beginning to preserve cache (prefix-based) and keep
				// recent messages intact, mirroring the Rust remove_first_item path.
				history = history[1:]
				continue
			}
			return err
		}
		// STUB: transient stream errors would be retried with backoff in the Rust
		// implementation; here we surface them immediately.
		return err
	}
}

// errContextWindowExceeded is the sentinel for a model context-window overflow
// during compaction, mirroring the Rust `CodexErr::ContextWindowExceeded`. A
// concrete ModelClient may wrap it via fmt.Errorf("...: %w", errContextWindowExceeded).
var errContextWindowExceeded = errors.New("core: context window exceeded")

// drainCompactStreamOnce issues a single model request and consumes its stream,
// recording each completed output item into shared history and folding the final
// token usage in. It returns once the Completed event arrives (or an error). It
// is the Go analogue of the Rust `drain_to_completed`.
func drainCompactStreamOnce(ctx context.Context, sess *Session, tc *TurnContext, prompt Prompt) error {
	stream, err := sess.services.ModelClient.Stream(ctx, prompt)
	if err != nil {
		return fmt.Errorf("core: compaction stream failed: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return ErrTurnAborted
		case ev, ok := <-stream:
			if !ok {
				return fmt.Errorf("core: %s", streamClosedBeforeCompletedMsg)
			}
			done, herr := handleCompactStreamEvent(sess, tc, ev)
			if herr != nil {
				return herr
			}
			if done {
				return nil
			}
		}
	}
}

// handleCompactStreamEvent processes one stream event during compaction. Unlike a
// regular turn it does NOT emit per-item turn events or dispatch tool calls: it
// only records completed output items (the summary) and accounts tokens. It
// returns done=true on the Completed event. Mirrors the Rust `drain_to_completed`
// match arms (reduced: rate-limit / server-reasoning side effects are recorded on
// state for parity but not re-emitted).
func handleCompactStreamEvent(sess *Session, tc *TurnContext, ev api.ResponseEvent) (done bool, err error) {
	switch ev.Kind {
	case api.ResponseEventOutputItemDone:
		if ev.Item != nil {
			sess.RecordItems([]protocol.ResponseItem{*ev.Item})
		}
		return false, nil
	case api.ResponseEventServerReasoningIncluded:
		sess.WithState(func(st *SessionState) { st.SetServerReasoningIncluded(ev.ReasoningIncluded) })
		return false, nil
	case api.ResponseEventRateLimits:
		if ev.RateLimits != nil {
			sess.WithState(func(st *SessionState) { st.SetRateLimits(*ev.RateLimits) })
		}
		return false, nil
	case api.ResponseEventCompleted:
		if ev.TokenUsage != nil {
			sess.WithState(func(st *SessionState) {
				st.UpdateTokenInfoFromUsage(*ev.TokenUsage, tc.ModelContextWindow)
			})
		}
		return true, nil
	default:
		// Created, deltas, added-items, server-model, verifications, etag: ignored
		// during compaction (no client-visible item events for the summary turn).
		return false, nil
	}
}
