package core

import (
	"context"

	"github.com/sqlrush/codexgo/pkg/modelsmanager"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// newTurnContext builds the per-turn read-only [TurnContext] for a submission,
// applying an optional settings update to the session configuration first. It
// resolves model identity, context window, tools, and the effective settings the
// turn runs against. Mirrors the Rust `Session::new_turn_with_sub_id`
// (turn-running subset).
//
// The returned TurnContext is immutable after construction. resolveSettings, when
// non-nil, is applied to produce the effective configuration for this turn.
func newTurnContext(ctx context.Context, sess *Session, subID string, update *SessionSettingsUpdate) (*TurnContext, error) {
	cfg := sess.CloneConfiguration()
	if update != nil {
		cfg = sess.UpdateConfiguration(*update)
	}

	tc := &TurnContext{
		SubID:                        subID,
		RealtimeActive:               false,
		ModelSlug:                    cfg.Model(),
		Cwd:                          cfg.Cwd,
		BaseInstructions:             cfg.BaseInstructions,
		DeveloperInstructions:        cfg.DeveloperInstructions,
		UserInstructions:             cfg.UserInstructions,
		CompactPrompt:                cfg.CompactPrompt,
		CollaborationMode:            cfg.CollaborationMode,
		Personality:                  cfg.Personality,
		ApprovalPolicy:               cfg.ApprovalPolicy,
		ApprovalsReviewer:            cfg.ApprovalsReviewer,
		SandboxMode:                  cfg.SandboxMode,
		NetworkAccessEnabled:         cfg.NetworkAccessEnabled,
		WindowsSandboxLevel:          cfg.WindowsSandboxLevel,
		Environments:                 append([]protocol.TurnEnvironmentSelection(nil), cfg.Environments...),
		DynamicTools:                 append([]protocol.DynamicToolSpec(nil), cfg.DynamicTools...),
		AppServerClientName:          cfg.AppServerClientName,
		ReasoningEffort:              cfg.CollaborationMode.Settings.ReasoningEffort,
		Features:                     cfg.Features,
		WebSearchMode:                cfg.WebSearchMode,
		ProviderCapabilities:         cfg.ProviderCapabilities,
		StreamMaxRetries:             cfg.StreamMaxRetries,
		AutoCompactTokenLimitScope:   cfg.AutoCompactTokenLimitScope,
		TokenBudget:                  cfg.TokenBudget,
		ExperimentalRequestUserInput: cfg.ExperimentalRequestUserInput,
		SessionSource:                cfg.SessionSource,
	}
	if cfg.ModelReasoningSummary != nil {
		tc.ReasoningSummary = *cfg.ModelReasoningSummary
	}
	// Resolve the provider capability upper bounds for the turn. An explicit
	// config override (non-zero) wins; otherwise the provider id is mapped
	// through the installed resolver (modelproviderinfo.CapabilitiesForProvider
	// in the assembled binary, all-true otherwise). Mirrors reading
	// turn_context.provider.capabilities() in spec_plan.
	if tc.ProviderCapabilities == (ProviderCapabilities{}) {
		tc.ProviderCapabilities = resolveProviderCapabilities(cfg.ProviderID)
	}
	if update != nil && update.FinalOutputJSONSchema != nil {
		tc.FinalOutputJSONSchema = *update.FinalOutputJSONSchema
	}

	// Resolve opaque model metadata + the effective context window.
	if mm := sess.services.ModelsManager; mm != nil {
		if mi, err := mm.ModelInfo(ctx, tc.ModelSlug); err == nil {
			tc.ModelInfo = mi
			// Surface the auto-compaction budget as an explicit field so runTurn
			// does not have to interpret the opaque ModelInfo. The concrete type
			// is modelsmanager.ModelInfo in the assembled binary.
			if info, ok := mi.(modelsmanager.ModelInfo); ok {
				tc.AutoCompactTokenLimit = info.AutoCompactTokenLimitResolved()
			}
		}
	}
	if mc := sess.services.ModelClient; mc != nil {
		tc.ModelContextWindow = mc.ContextWindow()
	}

	// Resolve the turn's visible tool set.
	if tr := sess.services.ToolRouter; tr != nil {
		specs, err := tr.SpecsForTurn(ctx, tc)
		if err != nil {
			return nil, err
		}
		tc.Tools = specs
	}

	// Load the opaque skills outcome for the turn.
	if sm := sess.services.SkillsManager; sm != nil {
		if skills, err := sm.SkillsForTurn(ctx, tc); err == nil {
			tc.Skills = skills
		}
	}

	return tc, nil
}

// spawnTask installs a new active turn for the given kind and runs it on a
// dedicated goroutine. It cancels any previously running task (replace
// semantics), emits TurnStarted, runs runFn, then emits the terminal
// TurnComplete/TurnAborted event and clears the active-turn slot. Mirrors the
// Rust `Session::spawn_task` (turn-running subset).
//
// runFn receives the task's cancellation context and returns the last assistant
// message, if any.
func spawnTask(sess *Session, tc *TurnContext, kind TaskKind, runFn func(ctx context.Context) *string) {
	// Replace any existing task: cancel it and clear its waiters.
	if prev := sess.ActiveTurn(); prev != nil && prev.Task != nil {
		prev.Task.Cancel()
		prev.State.ClearPendingWaiters()
	}

	taskCtx, cancel := context.WithCancel(sess.ctx)
	task := &RunningTask{
		Kind:        kind,
		TurnContext: tc,
		Cancel:      cancel,
		ctx:         taskCtx,
		done:        make(chan struct{}),
	}
	at := &ActiveTurn{Task: task, State: NewTurnState()}
	sess.setActiveTurn(at)

	// Snapshot token usage at turn start for body-after-prefix accounting.
	sess.WithState(func(st *SessionState) {
		if info := st.TokenInfo(); info != nil {
			at.State.SetTokenUsageAtTurnStart(info.TotalTokenUsage)
		}
	})

	emitTurnStarted(sess, tc)

	go func() {
		defer task.markDone()
		defer cancel()
		defer sess.clearActiveTurnIf(at)

		// Fire the turn-start hook (best effort).
		if h := sess.services.HooksEngine; h != nil {
			_ = h.Fire(taskCtx, HookTurnStart, tc.SubID)
		}

		last := runFn(taskCtx)

		// Fire the turn-end hook (best effort).
		if h := sess.services.HooksEngine; h != nil {
			_ = h.Fire(taskCtx, HookTurnEnd, tc.SubID)
		}

		if taskCtx.Err() != nil {
			emitTurnAborted(sess, tc, protocol.TurnAbortReasonInterrupted)
			return
		}
		emitTurnComplete(sess, tc, last)
	}()
}

// clearActiveTurnIf clears the active-turn slot only when it still points at the
// given turn, so a task that has been replaced does not clobber its successor.
func (s *Session) clearActiveTurnIf(at *ActiveTurn) {
	s.activeTurnMu.Lock()
	defer s.activeTurnMu.Unlock()
	if s.activeTurn == at {
		s.activeTurn = nil
	}
}

// emitTurnStarted emits the TurnStarted lifecycle event. Mirrors the Rust
// turn-started emission.
func emitTurnStarted(sess *Session, tc *TurnContext) {
	started := nowSeconds()
	sess.SendEvent(tc.SubID, protocol.EventMsg{
		Type: protocol.EventMsgKindTurnStarted,
		TurnStarted: &protocol.TurnStartedEvent{
			TurnID:             tc.SubID,
			TraceID:            tc.TraceID,
			StartedAt:          &started,
			ModelContextWindow: tc.ModelContextWindow,
			CollaborationMode:  tc.CollaborationMode.Mode,
		},
	})
}

// emitTurnComplete emits the TurnComplete lifecycle event carrying the last
// assistant message. Mirrors the Rust turn-complete emission.
func emitTurnComplete(sess *Session, tc *TurnContext, lastAgentMessage *string) {
	completedAt := nowSeconds()
	sess.SendEvent(tc.SubID, protocol.EventMsg{
		Type: protocol.EventMsgKindTurnComplete,
		TurnComplete: &protocol.TurnCompleteEvent{
			TurnID:           tc.SubID,
			LastAgentMessage: lastAgentMessage,
			CompletedAt:      &completedAt,
		},
	})
}

// emitTurnAborted emits the TurnAborted lifecycle event with the given reason.
// Mirrors the Rust turn-aborted emission.
func emitTurnAborted(sess *Session, tc *TurnContext, reason protocol.TurnAbortReason) {
	completedAt := nowSeconds()
	turnID := tc.SubID
	sess.SendEvent(tc.SubID, protocol.EventMsg{
		Type: protocol.EventMsgKindTurnAborted,
		TurnAborted: &protocol.TurnAbortedEvent{
			TurnID:      &turnID,
			Reason:      reason,
			CompletedAt: &completedAt,
		},
	})
}
