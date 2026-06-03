package goal

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// fakeStore is an in-memory ThreadGoalsStore + StateRuntime used in tests.
type fakeStore struct {
	mu          sync.Mutex
	goal        *StateThreadGoal
	preview     string
	previewSet  bool
	getErr      error
	insertErr   error
	updateErr   error
	accountErr  error
	usageErr    error
	accountCall int
	clock       time.Time
}

func newFakeStore() *fakeStore {
	return &fakeStore{clock: time.Unix(1_700_000_000, 0).UTC()}
}

func (f *fakeStore) ThreadGoals() ThreadGoalsStore { return f }

func (f *fakeStore) SetThreadPreviewIfEmpty(_ context.Context, _ protocol.ThreadID, preview string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.previewSet {
		return false, nil
	}
	f.preview = preview
	f.previewSet = true
	return true, nil
}

func (f *fakeStore) GetThreadGoal(_ context.Context, _ protocol.ThreadID) (*StateThreadGoal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	return cloneGoal(f.goal), nil
}

func (f *fakeStore) InsertThreadGoal(_ context.Context, threadID protocol.ThreadID, objective string, status StateThreadGoalStatus, tokenBudget *int64) (*StateThreadGoal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.insertErr != nil {
		return nil, f.insertErr
	}
	if f.goal != nil {
		return nil, nil
	}
	f.goal = &StateThreadGoal{
		ThreadID:    threadID,
		GoalID:      "goal-1",
		Objective:   objective,
		Status:      status,
		TokenBudget: tokenBudget,
		CreatedAt:   f.clock,
		UpdatedAt:   f.clock,
	}
	return cloneGoal(f.goal), nil
}

func (f *fakeStore) UpdateThreadGoal(_ context.Context, _ protocol.ThreadID, update GoalUpdate) (*StateThreadGoal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	if f.goal == nil {
		return nil, nil
	}
	if update.Status != nil {
		f.goal.Status = *update.Status
	}
	if update.Objective != nil {
		f.goal.Objective = *update.Objective
	}
	if update.TokenBudget != nil {
		f.goal.TokenBudget = *update.TokenBudget
	}
	f.goal.UpdatedAt = f.clock
	return cloneGoal(f.goal), nil
}

func (f *fakeStore) AccountThreadGoalUsage(_ context.Context, _ protocol.ThreadID, timeDelta, tokenDelta int64, _ GoalAccountingMode, _ *string) (GoalAccountingOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accountCall++
	if f.accountErr != nil {
		return GoalAccountingOutcome{}, f.accountErr
	}
	if f.goal == nil {
		return GoalAccountingOutcome{Kind: GoalAccountingOutcomeUnchanged}, nil
	}
	f.goal.TokensUsed += tokenDelta
	f.goal.TimeUsedSeconds += timeDelta
	if f.goal.TokenBudget != nil && f.goal.TokensUsed >= *f.goal.TokenBudget && f.goal.Status == StateGoalStatusActive {
		f.goal.Status = StateGoalStatusBudgetLimited
	}
	return GoalAccountingOutcome{Kind: GoalAccountingOutcomeUpdated, Goal: cloneGoal(f.goal)}, nil
}

func (f *fakeStore) UsageLimitActiveThreadGoal(_ context.Context, _ protocol.ThreadID) (*StateThreadGoal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.usageErr != nil {
		return nil, f.usageErr
	}
	if f.goal == nil {
		return nil, nil
	}
	f.goal.Status = StateGoalStatusUsageLimited
	return cloneGoal(f.goal), nil
}

func cloneGoal(g *StateThreadGoal) *StateThreadGoal {
	if g == nil {
		return nil
	}
	out := *g
	if g.TokenBudget != nil {
		v := *g.TokenBudget
		out.TokenBudget = &v
	}
	return &out
}

// fakeMetrics records the metric names emitted.
type fakeMetrics struct {
	mu         sync.Mutex
	counters   []string
	histograms []string
}

func (m *fakeMetrics) Counter(name string, _ int64, _ [][2]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters = append(m.counters, name)
}

func (m *fakeMetrics) Histogram(name string, _ int64, _ [][2]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.histograms = append(m.histograms, name)
}

// captureSink records emitted events.
type captureSink struct {
	mu     sync.Mutex
	events []protocol.Event
}

func (c *captureSink) Emit(e protocol.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *captureSink) all() []protocol.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]protocol.Event, len(c.events))
	copy(out, c.events)
	return out
}

// fakeThreadManager records injected steering items.
type fakeThreadManager struct {
	thread *fakeThread
	err    error
}

func (m *fakeThreadManager) GetThread(_ context.Context, _ protocol.ThreadID) (LiveThread, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.thread == nil {
		return nil, errors.New("no thread")
	}
	return m.thread, nil
}

type fakeThread struct {
	mu       sync.Mutex
	injected [][]protocol.ResponseItem
	injErr   error
}

func (t *fakeThread) InjectIfRunning(_ context.Context, items []protocol.ResponseItem) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.injErr != nil {
		return t.injErr
	}
	t.injected = append(t.injected, items)
	return nil
}

func (t *fakeThread) injections() [][]protocol.ResponseItem {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.injected
}
