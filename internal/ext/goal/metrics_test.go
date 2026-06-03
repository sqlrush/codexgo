package goal

import "testing"

func statusPtr(s StateThreadGoalStatus) *StateThreadGoalStatus { return &s }

func TestMetricsNilClientIsNoOp(t *testing.T) {
	m := NewGoalMetrics(nil)
	// None of these should panic with a nil client.
	m.recordCreated()
	m.recordResumed()
	m.recordResumedIfStatusChanged(statusPtr(StateGoalStatusPaused), StateGoalStatusActive)
	m.recordTerminalIfStatusChanged(statusPtr(StateGoalStatusActive), &StateThreadGoal{Status: StateGoalStatusComplete})
}

func TestRecordResumedIfStatusChanged(t *testing.T) {
	tests := []struct {
		name     string
		previous *StateThreadGoalStatus
		current  StateThreadGoalStatus
		want     bool
	}{
		{"paused to active", statusPtr(StateGoalStatusPaused), StateGoalStatusActive, true},
		{"blocked to active", statusPtr(StateGoalStatusBlocked), StateGoalStatusActive, true},
		{"usage-limited to active", statusPtr(StateGoalStatusUsageLimited), StateGoalStatusActive, true},
		{"active to active", statusPtr(StateGoalStatusActive), StateGoalStatusActive, false},
		{"nil previous", nil, StateGoalStatusActive, false},
		{"to non-active", statusPtr(StateGoalStatusPaused), StateGoalStatusComplete, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			metrics := &fakeMetrics{}
			NewGoalMetrics(metrics).recordResumedIfStatusChanged(tc.previous, tc.current)
			got := len(metrics.counters) == 1 && metrics.counters[0] == goalResumedMetric
			if got != tc.want {
				t.Errorf("resumed recorded = %v, want %v (counters=%v)", got, tc.want, metrics.counters)
			}
		})
	}
}

func TestRecordTerminalIfStatusChanged(t *testing.T) {
	tests := []struct {
		name        string
		previous    *StateThreadGoalStatus
		status      StateThreadGoalStatus
		wantCounter string
		wantHist    bool
	}{
		{"blocked", statusPtr(StateGoalStatusActive), StateGoalStatusBlocked, goalBlockedMetric, true},
		{"usage-limited", statusPtr(StateGoalStatusActive), StateGoalStatusUsageLimited, goalUsageLimitedMetric, true},
		{"budget-limited", statusPtr(StateGoalStatusActive), StateGoalStatusBudgetLimited, goalBudgetLimitedMetric, true},
		{"complete", statusPtr(StateGoalStatusActive), StateGoalStatusComplete, goalCompletedMetric, true},
		{"no change", statusPtr(StateGoalStatusComplete), StateGoalStatusComplete, "", false},
		{"active not terminal", statusPtr(StateGoalStatusPaused), StateGoalStatusActive, "", false},
		{"paused not terminal", statusPtr(StateGoalStatusActive), StateGoalStatusPaused, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			metrics := &fakeMetrics{}
			goal := &StateThreadGoal{Status: tc.status, TokensUsed: 10, TimeUsedSeconds: 5}
			NewGoalMetrics(metrics).recordTerminalIfStatusChanged(tc.previous, goal)
			if tc.wantCounter == "" {
				if len(metrics.counters) != 0 {
					t.Errorf("unexpected counters %v", metrics.counters)
				}
				return
			}
			if len(metrics.counters) != 1 || metrics.counters[0] != tc.wantCounter {
				t.Errorf("counters = %v, want [%s]", metrics.counters, tc.wantCounter)
			}
			if tc.wantHist && len(metrics.histograms) != 2 {
				t.Errorf("histograms = %v, want token-count + duration", metrics.histograms)
			}
		})
	}
}
