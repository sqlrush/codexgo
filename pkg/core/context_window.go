package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/rollout"
)

// This file ports `session/context_window.rs` + `session/token_budget.rs` +
// `compact_token_budget.rs` (upstream 0.147; spec 50 D0.3): the per-request
// context-window accounting that decides when a turn must roll over into a
// fresh context window (or compact), the token-budget reminder / fallback
// prompt, and the `new_context_window` request honored at the next sampling
// boundary. Window identities are UUIDv7 and travel in the
// `x-codex-window-id` header.

// TokenBudgetConfig mirrors the Rust `TokenBudgetConfig` (config.token_budget).
type TokenBudgetConfig struct {
	// ReminderThresholdTokens: when the remaining base window is at or below
	// this, a reminder is recorded once per window. nil = no reminder.
	ReminderThresholdTokens *int64
	// ReminderMessageTemplate is the reminder text; "{tokens_remaining}" is
	// substituted. Empty uses a default.
	ReminderMessageTemplate string
	// GuidanceMessage is appended to the reminder when set.
	GuidanceMessage string
	// AutoCompactFallbackPrompt is recorded once per window when the remaining
	// window hits zero and no rollover will happen.
	AutoCompactFallbackPrompt string
	// AutoCompactFallbackBufferTokens is added to the auto-compact limit to form
	// the buffered limit that forces compaction (reserved only when a fallback
	// prompt exists).
	AutoCompactFallbackBufferTokens int64
}

// fallbackBufferTokens mirrors `TokenBudgetConfig::fallback_buffer_tokens`.
func (c *TokenBudgetConfig) fallbackBufferTokens() int64 {
	if c == nil || c.AutoCompactFallbackPrompt == "" {
		return 0
	}
	return maxInt64(c.AutoCompactFallbackBufferTokens, 0)
}

// ContextWindowTokenStatus is the post-sampling view of the context window.
// Mirrors Rust `ContextWindowTokenStatus`.
type ContextWindowTokenStatus struct {
	// ActiveContextTokens is the full active context usage, independent of the
	// configured auto-compact scope.
	ActiveContextTokens int64
	// AutoCompactScopeTokens is the usage counted against the auto-compact
	// limit for the current scope.
	AutoCompactScopeTokens int64
	AutoCompactScopeLimit  *int64
	FullContextWindowLimit *int64
	// BaseWindowTokensRemaining is the remaining budget against the base
	// (unbuffered) window, capped by the full context window.
	BaseWindowTokensRemaining      *int64
	AutoCompactWindowPrefillTokens *int64
	// FullContextWindowLimitReached: the model's hard cap is reached.
	FullContextWindowLimitReached bool
	// TokenLimitReached: the buffered auto-compact limit or the hard cap is
	// reached — the turn must compact / roll over before sampling again.
	TokenLimitReached bool
}

func tokensRemaining(limit *int64, used int64) *int64 {
	if limit == nil {
		return nil
	}
	rem := maxInt64(*limit-used, 0)
	return &rem
}

func minOptional(vals ...*int64) *int64 {
	var out *int64
	for _, v := range vals {
		if v == nil {
			continue
		}
		if out == nil || *v < *out {
			c := *v
			out = &c
		}
	}
	return out
}

// contextWindowTokenStatus computes the window status from the session's
// running usage and the turn's limits. Mirrors `context_window_token_status`.
func contextWindowTokenStatus(sess *Session, tc *TurnContext) ContextWindowTokenStatus {
	var active int64
	var window AutoCompactWindowSnapshot
	sess.WithState(func(st *SessionState) {
		active = st.GetTotalTokenUsage()
		window = st.AutoCompactWindowSnapshot()
	})

	scopeTokens := active
	scopeLimit := tc.AutoCompactTokenLimit
	var prefill *int64
	if tc.AutoCompactTokenLimitScope == protocol.AutoCompactTokenLimitScopeBodyAfterPrefix {
		baseline := active
		if window.PrefillInputTokens != nil {
			baseline = *window.PrefillInputTokens
		}
		scopeTokens = maxInt64(active-baseline, 0)
		prefill = window.PrefillInputTokens
	}
	if scopeLimit != nil && *scopeLimit <= 0 {
		scopeLimit = nil
	}
	fullLimit := tc.ModelContextWindow

	baseRemaining := minOptional(tokensRemaining(scopeLimit, scopeTokens), tokensRemaining(fullLimit, active))

	var bufferedLimit *int64
	if scopeLimit != nil {
		b := *scopeLimit + tc.TokenBudget.fallbackBufferTokens()
		bufferedLimit = &b
	}
	fullReached := fullLimit != nil && active >= *fullLimit
	limitReached := (bufferedLimit != nil && scopeTokens >= *bufferedLimit) || fullReached

	return ContextWindowTokenStatus{
		ActiveContextTokens:            active,
		AutoCompactScopeTokens:         scopeTokens,
		AutoCompactScopeLimit:          scopeLimit,
		FullContextWindowLimit:         fullLimit,
		BaseWindowTokensRemaining:      baseRemaining,
		AutoCompactWindowPrefillTokens: prefill,
		FullContextWindowLimitReached:  fullReached,
		TokenLimitReached:              limitReached,
	}
}

// defaultTokenBudgetReminderTemplate is the built-in reminder wording.
const defaultTokenBudgetReminderTemplate = "You have about {tokens_remaining} tokens of context left before a new context window starts. Wrap up the current step and keep your working notes concise."

// maybeRecordTokenBudget records the once-per-window reminder when the
// remaining base window is at or below the threshold, and the fallback prompt
// when it hits zero and no rollover will happen. Mirrors
// `token_budget::maybe_record`.
func maybeRecordTokenBudget(sess *Session, tc *TurnContext, baseRemaining *int64, allowFallback bool) {
	cfg := tc.TokenBudget
	if cfg == nil || baseRemaining == nil {
		return
	}
	if cfg.ReminderThresholdTokens != nil && *baseRemaining <= *cfg.ReminderThresholdTokens {
		var due bool
		sess.WithState(func(st *SessionState) { due = st.ClaimTokenBudgetReminder() })
		if due {
			tmpl := cfg.ReminderMessageTemplate
			if tmpl == "" {
				tmpl = defaultTokenBudgetReminderTemplate
			}
			text := strings.ReplaceAll(tmpl, "{tokens_remaining}", fmt.Sprintf("%d", *baseRemaining))
			if cfg.GuidanceMessage != "" {
				text += "\n\n" + cfg.GuidanceMessage
			}
			sess.RecordItems([]protocol.ResponseItem{developerTextMessage(text)})
		}
	}
	if !allowFallback || *baseRemaining != 0 || cfg.AutoCompactFallbackPrompt == "" {
		return
	}
	var due bool
	sess.WithState(func(st *SessionState) { due = st.ClaimAutoCompactFallback() })
	if due {
		sess.RecordItems([]protocol.ResponseItem{developerTextMessage(cfg.AutoCompactFallbackPrompt)})
	}
}

// StartNewContextWindow installs a fresh context window (0.147
// `start_new_context_window`): the window accounting advances (new UUIDv7 id),
// history is replaced with the initial context, a compacted rollout line with
// the window chain metadata plus a turn-context snapshot are persisted, token
// usage is recomputed and the model client is told the new window id. It
// returns the new window number.
func (s *Session) StartNewContextWindow(ctx context.Context, tc *TurnContext) uint64 {
	var windowNumber uint64
	var ids AutoCompactWindowIDs
	s.WithState(func(st *SessionState) { windowNumber, ids = st.StartNewContextWindow() })
	contextItems := s.buildInitialContext(tc)
	s.WithState(func(st *SessionState) { st.ReplaceHistory(contextItems) })
	replacement := append([]protocol.ResponseItem(nil), contextItems...)
	s.recordRollout(ctx, rollout.NewCompactedItem(rollout.CompactedItem{
		ReplacementHistory: &replacement,
		WindowNumber:       &windowNumber,
		FirstWindowID:      &ids.FirstWindowID,
		PreviousWindowID:   ids.PreviousWindowID,
		WindowID:           &ids.WindowID,
	}))
	if item, err := tc.ToTurnContextItem(); err == nil {
		s.recordRollout(ctx, rollout.NewTurnContextItem(item))
	}
	s.recomputeTokenUsage(tc)
	s.syncWindowID(ids.WindowID)
	return windowNumber
}

// WindowIDSetter is implemented by model clients that stamp the current context
// window id on requests (`x-codex-window-id`); the session calls it whenever a
// new window starts.
type WindowIDSetter interface {
	SetWindowID(id string)
}

// syncWindowID pushes the active window id to the model client when it can
// carry one.
func (s *Session) syncWindowID(id string) {
	if setter, ok := s.services.ModelClient.(WindowIDSetter); ok {
		setter.SetWindowID(id)
	}
}

// CurrentWindowID returns the active context window id.
func (s *Session) CurrentWindowID() string {
	var id string
	s.WithState(func(st *SessionState) { id = st.CurrentWindowID() })
	return id
}

// RequestNewContextWindow records a `new_context_window` request; the running
// turn honors it at its next sampling boundary.
func (s *Session) RequestNewContextWindow() {
	s.WithState(func(st *SessionState) { st.RequestNewContextWindow() })
}

// runInlineTokenBudgetCompact is the token-budget compaction lifecycle
// (`compact_token_budget::run_inline_auto_compact_task`): a ContextCompaction
// item wraps installing a fresh context window instead of summarizing.
func runInlineTokenBudgetCompact(ctx context.Context, sess *Session, tc *TurnContext) {
	item := newContextCompactionTurnItem()
	EmitTurnItemStarted(sess, tc, item)
	sess.StartNewContextWindow(ctx, tc)
	EmitTurnItemCompleted(sess, tc, item)
}

// runAutoCompact is the mid-turn compaction dispatch (`run_auto_compact`,
// reduced: no remote compaction): token-budget mode installs a fresh window;
// otherwise the inline summarizing compaction runs with the initial context
// re-injected before the last user message.
func runAutoCompact(ctx context.Context, sess *Session, tc *TurnContext) error {
	if tc.TokenBudget != nil {
		runInlineTokenBudgetCompact(ctx, sess, tc)
		return nil
	}
	return runInlineAutoCompactTask(ctx, sess, tc, InjectBeforeLastUserMessage)
}
