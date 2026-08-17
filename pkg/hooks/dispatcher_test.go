package hooks

import (
	"testing"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

func testSourcePath(t *testing.T) abspath.AbsolutePathBuf {
	t.Helper()
	p, err := abspath.FromAbsolutePathChecked("/tmp/hooks.json")
	if err != nil {
		t.Fatalf("source path: %v", err)
	}
	return p
}

func makeHandler(t *testing.T, eventName protocol.HookEventName, matcher *string, command string, displayOrder int64) ConfiguredHandler {
	t.Helper()
	return ConfiguredHandler{
		EventName:    eventName,
		Matcher:      matcher,
		Command:      command,
		TimeoutSec:   5,
		SourcePath:   testSourcePath(t),
		Source:       protocol.HookSourceUser,
		DisplayOrder: displayOrder,
	}
}

func orders(handlers []ConfiguredHandler) []int64 {
	out := make([]int64, len(handlers))
	for i := range handlers {
		out[i] = handlers[i].DisplayOrder
	}
	return out
}

func TestSelectHandlers(t *testing.T) {
	tests := []struct {
		name       string
		handlers   []ConfiguredHandler
		eventName  protocol.HookEventName
		input      *string
		wantOrders []int64
	}{
		{
			name: "stop keeps duplicates",
			handlers: []ConfiguredHandler{
				makeHandler(t, protocol.HookEventNameStop, nil, "echo same", 0),
				makeHandler(t, protocol.HookEventNameStop, nil, "echo same", 1),
			},
			eventName:  protocol.HookEventNameStop,
			input:      nil,
			wantOrders: []int64{0, 1},
		},
		{
			name: "session start overlapping matchers",
			handlers: []ConfiguredHandler{
				makeHandler(t, protocol.HookEventNameSessionStart, sp("start.*"), "echo a", 0),
				makeHandler(t, protocol.HookEventNameSessionStart, sp("^startup$"), "echo b", 1),
			},
			eventName:  protocol.HookEventNameSessionStart,
			input:      sp("startup"),
			wantOrders: []int64{0, 1},
		},
		{
			name: "compact matches trigger",
			handlers: []ConfiguredHandler{
				makeHandler(t, protocol.HookEventNamePreCompact, sp("manual"), "echo m", 0),
				makeHandler(t, protocol.HookEventNamePreCompact, sp("auto"), "echo a", 1),
			},
			eventName:  protocol.HookEventNamePreCompact,
			input:      sp("manual"),
			wantOrders: []int64{0},
		},
		{
			name: "pre tool use star matches all",
			handlers: []ConfiguredHandler{
				makeHandler(t, protocol.HookEventNamePreToolUse, sp("*"), "echo s", 0),
				makeHandler(t, protocol.HookEventNamePreToolUse, sp("^Edit$"), "echo e", 1),
			},
			eventName:  protocol.HookEventNamePreToolUse,
			input:      sp("Bash"),
			wantOrders: []int64{0},
		},
		{
			name: "user prompt submit ignores invalid matcher",
			handlers: []ConfiguredHandler{
				makeHandler(t, protocol.HookEventNameUserPromptSubmit, sp("^hello"), "echo 1", 0),
				makeHandler(t, protocol.HookEventNameUserPromptSubmit, sp("["), "echo 2", 1),
			},
			eventName:  protocol.HookEventNameUserPromptSubmit,
			input:      nil,
			wantOrders: []int64{0, 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectHandlers(tt.handlers, tt.eventName, tt.input)
			gotOrders := orders(got)
			if len(gotOrders) != len(tt.wantOrders) {
				t.Fatalf("selected orders = %v, want %v", gotOrders, tt.wantOrders)
			}
			for i := range tt.wantOrders {
				if gotOrders[i] != tt.wantOrders[i] {
					t.Fatalf("selected orders = %v, want %v", gotOrders, tt.wantOrders)
				}
			}
		})
	}
}

func TestSelectHandlersAliasesMatchOncePerHandler(t *testing.T) {
	handlers := []ConfiguredHandler{
		makeHandler(t, protocol.HookEventNamePreToolUse, sp("^apply_patch$"), "echo ap", 0),
		makeHandler(t, protocol.HookEventNamePreToolUse, sp("^Write$"), "echo w", 1),
		makeHandler(t, protocol.HookEventNamePreToolUse, sp("^Edit$"), "echo e", 2),
		makeHandler(t, protocol.HookEventNamePreToolUse, sp("apply_patch|Write|Edit"), "echo c", 3),
	}
	got := selectHandlersForMatcherInputs(handlers, protocol.HookEventNamePreToolUse, []string{"apply_patch", "Write", "Edit"})
	want := []int64{0, 1, 2, 3}
	gotOrders := orders(got)
	if len(gotOrders) != len(want) {
		t.Fatalf("selected orders = %v, want %v", gotOrders, want)
	}
	for i := range want {
		if gotOrders[i] != want[i] {
			t.Fatalf("selected orders = %v, want %v", gotOrders, want)
		}
	}
}

func TestSelectHandlersRegexAlternation(t *testing.T) {
	handlers := []ConfiguredHandler{
		makeHandler(t, protocol.HookEventNamePreToolUse, sp("Edit|Write"), "echo c", 0),
	}
	if got := selectHandlers(handlers, protocol.HookEventNamePreToolUse, sp("Edit")); len(got) != 1 {
		t.Errorf("Edit should match, got %d", len(got))
	}
	if got := selectHandlers(handlers, protocol.HookEventNamePreToolUse, sp("Write")); len(got) != 1 {
		t.Errorf("Write should match, got %d", len(got))
	}
	if got := selectHandlers(handlers, protocol.HookEventNamePreToolUse, sp("Bash")); len(got) != 0 {
		t.Errorf("Bash should not match, got %d", len(got))
	}
}

func TestScopeForEvent(t *testing.T) {
	tests := []struct {
		event protocol.HookEventName
		want  protocol.HookScope
	}{
		{protocol.HookEventNameSessionStart, protocol.HookScopeThread},
		{protocol.HookEventNameSubagentStart, protocol.HookScopeThread},
		{protocol.HookEventNamePreToolUse, protocol.HookScopeTurn},
		{protocol.HookEventNameStop, protocol.HookScopeTurn},
		{protocol.HookEventNameSubagentStop, protocol.HookScopeTurn},
	}
	for _, tt := range tests {
		if got := scopeForEvent(tt.event); got != tt.want {
			t.Errorf("scopeForEvent(%s) = %s, want %s", tt.event, got, tt.want)
		}
	}
}

func TestRunIDFormat(t *testing.T) {
	h := makeHandler(t, protocol.HookEventNamePreToolUse, nil, "echo", 3)
	want := "pre-tool-use:3:" + testSourcePath(t).String()
	if got := h.RunID(); got != want {
		t.Errorf("RunID() = %q, want %q", got, want)
	}
}
