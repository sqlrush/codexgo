package core

import (
	"context"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/pkg/api"
	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/tools"
)

func i64(v int64) *int64 { return &v }

func sessionWithUsage(t *testing.T, total int64, contextWindow *int64) *Session {
	t.Helper()
	sess, _ := newCompactSession(t, NewMockModelClient("gpt-test", nil), &recordingRecorder{}, cUserMsg("u"))
	sess.WithState(func(st *SessionState) {
		st.UpdateTokenInfoFromUsage(protocol.TokenUsage{InputTokens: total, TotalTokens: total}, contextWindow)
	})
	return sess
}

// TestContextWindowTokenStatus locks the 0.147 status derivation: scope
// (total / body_after_prefix), the buffered auto-compact limit, the hard cap and
// the remaining base window.
func TestContextWindowTokenStatus(t *testing.T) {
	cases := []struct {
		name          string
		total         int64
		limit         *int64
		window        *int64
		scope         protocol.AutoCompactTokenLimitScope
		budget        *TokenBudgetConfig
		wantReached   bool
		wantRemaining *int64
	}{
		{"no limits", 1_000_000, nil, nil, "", nil, false, nil},
		{"zero limit disables", 1_000_000, i64(0), nil, "", nil, false, nil},
		{"under limit", 5_000, i64(10_000), nil, "", nil, false, i64(5_000)},
		{"at limit", 10_000, i64(10_000), nil, "", nil, true, i64(0)},
		{"over limit", 12_000, i64(10_000), nil, "", nil, true, i64(0)},
		{"hard cap wins over remaining", 9_000, i64(10_000), i64(9_500), "", nil, false, i64(500)},
		{"hard cap reached", 9_500, i64(10_000), i64(9_500), "", nil, true, i64(0)},
		{"fallback buffer defers compaction", 10_500, i64(10_000), nil, "", &TokenBudgetConfig{AutoCompactFallbackPrompt: "wrap up", AutoCompactFallbackBufferTokens: 1_000}, false, i64(0)},
		{"buffered limit reached", 11_000, i64(10_000), nil, "", &TokenBudgetConfig{AutoCompactFallbackPrompt: "wrap up", AutoCompactFallbackBufferTokens: 1_000}, true, i64(0)},
		{"buffer ignored without fallback prompt", 10_500, i64(10_000), nil, "", &TokenBudgetConfig{AutoCompactFallbackBufferTokens: 1_000}, true, i64(0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sess := sessionWithUsage(t, tc.total, tc.window)
			turn := &TurnContext{SubID: "t", AutoCompactTokenLimit: tc.limit, ModelContextWindow: tc.window, AutoCompactTokenLimitScope: tc.scope, TokenBudget: tc.budget}
			st := contextWindowTokenStatus(sess, turn)
			if st.TokenLimitReached != tc.wantReached {
				t.Fatalf("TokenLimitReached = %v, want %v (%+v)", st.TokenLimitReached, tc.wantReached, st)
			}
			if (st.BaseWindowTokensRemaining == nil) != (tc.wantRemaining == nil) || (tc.wantRemaining != nil && *st.BaseWindowTokensRemaining != *tc.wantRemaining) {
				t.Fatalf("BaseWindowTokensRemaining = %v, want %v", st.BaseWindowTokensRemaining, tc.wantRemaining)
			}
		})
	}
	// body_after_prefix charges only tokens beyond the server-observed prefill.
	sess := sessionWithUsage(t, 8_000, nil) // prefill observed at 8000
	sess.WithState(func(st *SessionState) {
		st.UpdateTokenInfoFromUsage(protocol.TokenUsage{InputTokens: 9_000, TotalTokens: 9_000}, nil)
	})
	turn := &TurnContext{SubID: "t", AutoCompactTokenLimit: i64(1_500), AutoCompactTokenLimitScope: protocol.AutoCompactTokenLimitScopeBodyAfterPrefix}
	st := contextWindowTokenStatus(sess, turn)
	if st.AutoCompactScopeTokens != 1_000 || st.TokenLimitReached || st.AutoCompactWindowPrefillTokens == nil || *st.AutoCompactWindowPrefillTokens != 8_000 {
		t.Fatalf("body_after_prefix status = %+v, want scope 1000 under a 1500 limit with prefill 8000", st)
	}
}

// TestStartNewContextWindowRotatesWindowID asserts a new window mints a fresh
// UUIDv7 id (previous retained), replaces history with the initial context,
// bumps the window number and pushes the id to the model client.
func TestStartNewContextWindowRotatesWindowID(t *testing.T) {
	sess := sessionWithUsage(t, 5_000, nil)
	client := &windowIDRecordingClient{}
	sess.services.ModelClient = client
	first := sess.CurrentWindowID()
	if first == "" {
		t.Fatal("initial window id should be minted")
	}
	turn := &TurnContext{SubID: "t", Cwd: "/work"}
	n := sess.StartNewContextWindow(context.Background(), turn)
	second := sess.CurrentWindowID()
	if n != 1 || second == first || second == "" {
		t.Fatalf("window number = %d, ids %s → %s; want 1 and a fresh id", n, first, second)
	}
	var snap AutoCompactWindowSnapshot
	sess.WithState(func(st *SessionState) { snap = st.AutoCompactWindowSnapshot() })
	if snap.IDs.FirstWindowID != first || snap.IDs.PreviousWindowID == nil || *snap.IDs.PreviousWindowID != first || snap.IDs.WindowID != second {
		t.Fatalf("window ids = %+v, want first=%s previous=%s current=%s", snap.IDs, first, first, second)
	}
	if client.last != second {
		t.Fatalf("model client window id = %q, want %q", client.last, second)
	}
	for _, it := range sess.HistoryItems() {
		if it.IsUserMessage() && len(it.Content) > 0 && it.Content[0].Text == "u" {
			t.Fatalf("history should have been replaced by the initial context, still has %q", "u")
		}
	}
	// A pending new-window request is a one-shot.
	sess.RequestNewContextWindow()
	var taken, again bool
	sess.WithState(func(st *SessionState) {
		taken = st.TakeNewContextWindowRequest()
		again = st.TakeNewContextWindowRequest()
	})
	if !taken || again {
		t.Fatalf("new window request take = %v then %v, want true then false", taken, again)
	}
}

type windowIDRecordingClient struct {
	MockModelClient
	last string
}

func (c *windowIDRecordingClient) SetWindowID(id string) { c.last = id }

// TestRunTurnRollsOverWhenLimitReachedMidTurn asserts the loop compacts /
// rolls over between sampling requests when the model needs a follow-up and
// the token limit is reached (token-budget mode: fresh window, no summary
// request), then continues the turn.
func TestRunTurnRollsOverWhenLimitReachedMidTurn(t *testing.T) {
	// Request 1: needs follow-up (end_turn=false) and reports usage over the
	// limit. Request 2: completes.
	over := &protocol.TokenUsage{InputTokens: 20_000, TotalTokens: 20_000}
	mc := NewMockModelClient("gpt-test", nil,
		MockTurn{Events: []api.ResponseEvent{evCreated(), evMessageDone("m1", "working"), evCompleted(false, over)}},
		MockTurn{Events: []api.ResponseEvent{evCreated(), evMessageDone("m2", "done"), evCompleted(true, nil)}},
	)
	sess, evCh, cancel := turnTestSession(t, mc, &fakeToolRouter{})
	defer cancel()
	tc, _ := newTurnContext(sess.ctx, sess, "turn-1", nil)
	tc.AutoCompactTokenLimit = i64(10_000)
	tc.TokenBudget = &TokenBudgetConfig{}
	before := sess.CurrentWindowID()

	last := runTurn(sess.ctx, sess, tc, []turnInput{{UserContent: textInput("go")}})
	if last == nil || *last != "done" {
		t.Fatalf("turn should finish on the second request, got %v", last)
	}
	if got := len(mc.ReceivedPrompts()); got != 2 {
		t.Fatalf("sampling requests = %d, want 2", got)
	}
	if sess.CurrentWindowID() == before {
		t.Fatalf("mid-turn rollover should have started a new context window")
	}
	kinds := eventsByKind(drainEvents(evCh))
	if kinds[protocol.EventMsgKindItemStarted] < 1 || kinds[protocol.EventMsgKindItemCompleted] < 1 {
		t.Fatalf("expected a ContextCompaction item lifecycle, got %v", kinds)
	}
}

// TestNewContextToolRequestsWindowAtNextBoundary asserts the new_context tool
// records the request and the get_context_remaining tool reports the window.
func TestNewContextToolRequestsWindowAtNextBoundary(t *testing.T) {
	sess := sessionWithUsage(t, 4_000, nil)
	turn := &TurnContext{SubID: "t", AutoCompactTokenLimit: i64(10_000), TokenBudget: &TokenBudgetConfig{}}
	remaining := getContextRemainingExecutor{}
	if _, ok := remaining.Spec(&TurnContext{}); ok {
		t.Fatal("token-budget tools must be hidden without a token budget")
	}
	if _, ok := remaining.Spec(turn); !ok {
		t.Fatal("token-budget tools must be advertised with a token budget")
	}
	out, err := remaining.Handle(context.Background(), &ToolHandlerContext{Session: sess, Turn: turn, CallID: "c", Payload: tools.FunctionPayload(`{}`)})
	if err != nil {
		t.Fatalf("get_context_remaining: %v", err)
	}
	if got := out.LogPreview(); got != "You have 6000 tokens left in this context window." {
		t.Fatalf("remaining text = %q", got)
	}
	if _, err := (newContextWindowExecutor{}).Handle(context.Background(), &ToolHandlerContext{Session: sess, Turn: turn, CallID: "c2", Payload: tools.FunctionPayload(`{}`)}); err != nil {
		t.Fatalf("new_context: %v", err)
	}
	var requested bool
	sess.WithState(func(st *SessionState) { requested = st.TakeNewContextWindowRequest() })
	if !requested {
		t.Fatal("new_context should record a pending window request")
	}
}

// TestTokenBudgetReminderAndFallbackOncePerWindow asserts the reminder is
// recorded once when remaining <= threshold, the fallback prompt once when
// remaining hits zero and no rollover happens, and both re-arm on a new window.
func TestTokenBudgetReminderAndFallbackOncePerWindow(t *testing.T) {
	sess := sessionWithUsage(t, 9_500, nil)
	turn := &TurnContext{SubID: "t", Cwd: "/work", AutoCompactTokenLimit: i64(10_000), TokenBudget: &TokenBudgetConfig{
		ReminderThresholdTokens:   i64(1_000),
		ReminderMessageTemplate:   "only {tokens_remaining} left",
		AutoCompactFallbackPrompt: "FALLBACK",
	}}
	countDev := func(text string) int {
		n := 0
		for _, it := range sess.HistoryItems() {
			if it.Type == protocol.ResponseItemKindMessage && it.Role == "developer" && len(it.Content) > 0 && it.Content[0].Text == text {
				n++
			}
		}
		return n
	}
	maybeRecordTokenBudget(sess, turn, i64(500), true)
	maybeRecordTokenBudget(sess, turn, i64(400), true)
	if got := countDev("only 500 left"); got != 1 {
		t.Fatalf("reminder recorded %d times, want exactly once per window", got)
	}
	maybeRecordTokenBudget(sess, turn, i64(0), false)
	if countDev("FALLBACK") != 0 {
		t.Fatal("fallback must not be recorded when a rollover is coming")
	}
	maybeRecordTokenBudget(sess, turn, i64(0), true)
	maybeRecordTokenBudget(sess, turn, i64(0), true)
	if got := countDev("FALLBACK"); got != 1 {
		t.Fatalf("fallback recorded %d times, want exactly once per window", got)
	}
	sess.StartNewContextWindow(context.Background(), turn)
	maybeRecordTokenBudget(sess, turn, i64(500), true)
	if got := countDev("only 500 left"); got != 1 {
		t.Fatalf("reminder after new window recorded %d times in the fresh history, want 1", got)
	}
	_ = time.Second
}
