package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// keyMsg builds a tea.KeyMsg for the given key string for tests.
func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// captureSender returns a sender that records every AppEvent it would send.
func captureSender() (*AppEventSender, *[]AppEvent) {
	var got []AppEvent
	s := NewAppEventSender()
	s.attachFunc(func(m tea.Msg) {
		if ev, ok := m.(AppEvent); ok {
			got = append(got, ev)
		}
	})
	return s, &got
}

// --- ScrollState -----------------------------------------------------------

func TestScrollStateWrapNavigation(t *testing.T) {
	s := NewScrollState()
	const length, vis = 10, 5

	s.ClampSelection(length)
	if s.Selected != 0 {
		t.Fatalf("ClampSelection: got %d, want 0", s.Selected)
	}
	s.MoveUpWrap(length)
	s.EnsureVisible(length, vis)
	if s.Selected != length-1 {
		t.Fatalf("MoveUpWrap from 0: got %d, want %d", s.Selected, length-1)
	}
	if s.ScrollTop > s.Selected {
		t.Fatalf("ScrollTop %d should be <= selected %d", s.ScrollTop, s.Selected)
	}
	s.MoveDownWrap(length)
	s.EnsureVisible(length, vis)
	if s.Selected != 0 || s.ScrollTop != 0 {
		t.Fatalf("MoveDownWrap wrap: sel=%d top=%d, want 0/0", s.Selected, s.ScrollTop)
	}
}

func TestScrollStatePageAndJump(t *testing.T) {
	s := NewScrollState()
	const length, vis = 10, 4
	s.ClampSelection(length)
	s.PageDownClamped(length, vis)
	if s.Selected != 4 {
		t.Fatalf("page down: got %d want 4", s.Selected)
	}
	s.JumpBottom(length, vis)
	if s.Selected != 9 {
		t.Fatalf("jump bottom: got %d want 9", s.Selected)
	}
	s.JumpTop(length, vis)
	if s.Selected != 0 {
		t.Fatalf("jump top: got %d want 0", s.Selected)
	}
}

func TestScrollStateEmptyClears(t *testing.T) {
	s := ScrollState{Selected: 3, ScrollTop: 2}
	s.MoveDownWrap(0)
	if s.HasSelection() {
		t.Fatalf("empty list should clear selection")
	}
}

// --- slash command gating --------------------------------------------------

func TestSlashCommandNameAndAlias(t *testing.T) {
	if SlashStop.Command() != "stop" {
		t.Fatalf("stop command name: %q", SlashStop.Command())
	}
	if cmd, ok := ParseSlashCommandName("clean"); !ok || cmd != SlashStop {
		t.Fatalf("clean alias should resolve to stop, got %v ok=%v", cmd, ok)
	}
	if cmd, ok := ParseSlashCommandName("pet"); !ok || cmd != SlashPets {
		t.Fatalf("pet alias should resolve to pets, got %v ok=%v", cmd, ok)
	}
}

func TestBuiltinsForInputSideConversation(t *testing.T) {
	flags := BuiltinCommandFlags{
		CollaborationModesEnabled:   true,
		ConnectorsEnabled:           true,
		PluginsCommandEnabled:       true,
		GoalCommandEnabled:          true,
		PersonalityCommandEnabled:   true,
		RealtimeConversationEnabled: true,
		AudioDeviceSelectionEnabled: true,
		AllowElevateSandbox:         true,
		SideConversationActive:      true,
	}
	got := BuiltinsForInput(flags)
	want := []SlashCommand{SlashIde, SlashCopy, SlashRaw, SlashDiff, SlashMention, SlashStatus}
	if len(got) != len(want) {
		t.Fatalf("side-conversation builtins: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("side-conversation builtins[%d]: got %v want %v", i, got[i], want[i])
		}
	}
}

func TestFindBuiltinCommandRespectsGating(t *testing.T) {
	all := BuiltinCommandFlags{
		CollaborationModesEnabled:   true,
		ConnectorsEnabled:           true,
		PluginsCommandEnabled:       true,
		GoalCommandEnabled:          true,
		PersonalityCommandEnabled:   true,
		RealtimeConversationEnabled: true,
		AudioDeviceSelectionEnabled: true,
		AllowElevateSandbox:         true,
	}
	if _, ok := FindBuiltinCommand("clear", all); !ok {
		t.Fatalf("clear should resolve for dispatch")
	}
	noGoal := all
	noGoal.GoalCommandEnabled = false
	if _, ok := FindBuiltinCommand("goal", noGoal); ok {
		t.Fatalf("goal should be hidden when disabled")
	}
}

// --- command popup ---------------------------------------------------------

func popupFlags() BuiltinCommandFlags {
	return BuiltinCommandFlags{
		CollaborationModesEnabled:   true,
		ConnectorsEnabled:           true,
		PluginsCommandEnabled:       true,
		GoalCommandEnabled:          true,
		PersonalityCommandEnabled:   true,
		RealtimeConversationEnabled: true,
		AudioDeviceSelectionEnabled: true,
	}
}

func TestCommandPopupFiltersInit(t *testing.T) {
	p := NewCommandPopup(popupFlags())
	p.OnComposerTextChange("/in")
	found := false
	for _, c := range p.filteredItems() {
		if c.Command() == "init" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected /init among filtered commands")
	}
}

func TestCommandPopupExactSelectsCommand(t *testing.T) {
	p := NewCommandPopup(popupFlags())
	p.OnComposerTextChange("/init")
	cmd, ok := p.SelectedItem()
	if !ok || cmd.Command() != "init" {
		t.Fatalf("exact match should select init, got %v ok=%v", cmd, ok)
	}
}

func TestCommandPopupModelFirstForMo(t *testing.T) {
	p := NewCommandPopup(popupFlags())
	p.OnComposerTextChange("/mo")
	items := p.filteredItems()
	if len(items) == 0 || items[0].Command() != "model" {
		t.Fatalf("model should be first match for /mo, got %v", items)
	}
}

func TestCommandPopupHidesDebugAndAlias(t *testing.T) {
	p := NewCommandPopup(popupFlags())
	p.OnComposerTextChange("/")
	for _, c := range p.filteredItems() {
		if strings.HasPrefix(c.Command(), "debug") {
			t.Fatalf("debug commands must be hidden from popup")
		}
		if c == SlashQuit || c == SlashBtw {
			t.Fatalf("alias %v should be hidden in empty filter", c)
		}
	}
	// Quit becomes visible when typed by prefix.
	p.OnComposerTextChange("/qu")
	hasQuit := false
	for _, c := range p.filteredItems() {
		if c == SlashQuit {
			hasQuit = true
		}
	}
	if !hasQuit {
		t.Fatalf("quit should appear for /qu prefix")
	}
}

func TestCommandPopupEnterAccepts(t *testing.T) {
	p := NewCommandPopup(popupFlags())
	p.OnComposerTextChange("/model")
	p.HandleKey(keyMsg("enter"))
	cmd, ok := p.Accepted()
	if !ok || cmd != SlashModel {
		t.Fatalf("enter should accept model, got %v ok=%v", cmd, ok)
	}
	if !p.IsComplete() {
		t.Fatalf("popup should be complete after accept")
	}
}

// --- file search popup -----------------------------------------------------

func TestFileSearchPopupStaleResultsIgnored(t *testing.T) {
	p := NewFileSearchPopup()
	p.SetQuery("foo")
	p.SetMatches("bar", []FileMatch{{Path: "x.go"}}) // stale
	if len(p.matches) != 0 {
		t.Fatalf("stale matches should be ignored")
	}
	p.SetMatches("foo", []FileMatch{{Path: "x.go"}, {Path: "y.go"}})
	if len(p.matches) != 2 {
		t.Fatalf("matching results should be applied, got %d", len(p.matches))
	}
}

func TestFileSearchPopupCapsRows(t *testing.T) {
	p := NewFileSearchPopup()
	p.SetQuery("f")
	var matches []FileMatch
	for i := 0; i < MaxPopupRows+3; i++ {
		matches = append(matches, FileMatch{Path: "f.go"})
	}
	p.SetMatches("f", matches)
	if len(p.matches) != MaxPopupRows {
		t.Fatalf("matches should be capped at %d, got %d", MaxPopupRows, len(p.matches))
	}
	if p.DesiredHeight(40) != MaxPopupRows {
		t.Fatalf("height should be %d", MaxPopupRows)
	}
}

func TestFileSearchPopupSelectAndAccept(t *testing.T) {
	p := NewFileSearchPopup()
	p.SetQuery("f")
	p.SetMatches("f", []FileMatch{{Path: "a.go"}, {Path: "b.go"}})
	p.HandleKey(keyMsg("down")) // select index 1
	p.HandleKey(keyMsg("enter"))
	path, ok := p.Accepted()
	if !ok || path != "b.go" {
		t.Fatalf("accept should yield b.go, got %q ok=%v", path, ok)
	}
}

// --- list selection overlay ------------------------------------------------

func TestListSelectionAcceptRunsAction(t *testing.T) {
	sender, _ := captureSender()
	ran := false
	v := NewListSelectionOverlay(SelectionViewParams{
		Items: []SelectionItem{
			{Name: "One", DismissOnSelect: true, Actions: []SelectionAction{func(*AppEventSender) { ran = true }}},
			{Name: "Two"},
		},
		InitialSelected: 0,
	}, sender)
	v.HandleKey(keyMsg("enter"))
	if !ran {
		t.Fatalf("accept should run the item action")
	}
	if v.Completion() != CompletionAccepted {
		t.Fatalf("dismiss-on-select item should accept the view")
	}
	idx, ok := v.TakeLastSelectedIndex()
	if !ok || idx != 0 {
		t.Fatalf("last selected idx should be 0, got %d ok=%v", idx, ok)
	}
}

func TestListSelectionEscCancels(t *testing.T) {
	sender, _ := captureSender()
	cancelled := false
	v := NewListSelectionOverlay(SelectionViewParams{
		Items:           []SelectionItem{{Name: "One"}},
		InitialSelected: 0,
		OnCancel:        func(*AppEventSender) { cancelled = true },
	}, sender)
	if v.OnCtrlC() != CancellationHandled {
		t.Fatalf("OnCtrlC should be handled")
	}
	if !cancelled || v.Completion() != CompletionCancelled {
		t.Fatalf("cancel callback should fire and view cancel")
	}
}

func TestListSelectionSkipsDisabled(t *testing.T) {
	sender, _ := captureSender()
	v := NewListSelectionOverlay(SelectionViewParams{
		Items: []SelectionItem{
			{Name: "Enabled1"},
			{Name: "Disabled", IsDisabled: true},
			{Name: "Enabled2"},
		},
		InitialSelected: 0,
	}, sender)
	v.HandleKey(keyMsg("down")) // skip disabled, land on Enabled2
	if got := v.SelectedIndex(); got != 2 {
		t.Fatalf("down should skip disabled to index 2, got %d", got)
	}
}

func TestListSelectionSearchFilters(t *testing.T) {
	sender, _ := captureSender()
	v := NewListSelectionOverlay(SelectionViewParams{
		IsSearchable: true,
		Items: []SelectionItem{
			{Name: "Apple", SearchValue: "apple"},
			{Name: "Banana", SearchValue: "banana"},
		},
		InitialSelected: -1,
	}, sender)
	v.HandleKey(keyMsg("b"))
	if v.visibleLen() != 1 {
		t.Fatalf("search 'b' should match one item, got %d", v.visibleLen())
	}
	if v.SelectedIndex() != 1 {
		t.Fatalf("selection should land on Banana (idx 1), got %d", v.SelectedIndex())
	}
}

func TestListSelectionNumberKeyJumps(t *testing.T) {
	sender, _ := captureSender()
	got2 := false
	v := NewListSelectionOverlay(SelectionViewParams{
		Items: []SelectionItem{
			{Name: "One"},
			{Name: "Two", Actions: []SelectionAction{func(*AppEventSender) { got2 = true }}},
		},
		InitialSelected: -1,
	}, sender)
	v.HandleKey(keyMsg("2"))
	if !got2 {
		t.Fatalf("number key 2 should accept the second item")
	}
}

// --- approval overlay ------------------------------------------------------

func TestApprovalExecApproveEmitsDecision(t *testing.T) {
	sender, got := captureSender()
	o := NewApprovalOverlay(ApprovalRequest{
		Kind: ApprovalExec, ThreadID: "t1", ID: "call-1", Command: []string{"ls"},
	}, sender)
	o.HandleKey(keyMsg("y")) // approve shortcut
	if !o.IsComplete() {
		t.Fatalf("overlay should be complete after decision")
	}
	if len(*got) != 1 {
		t.Fatalf("expected one decision event, got %d", len(*got))
	}
	ev, ok := (*got)[0].(SubmitThreadOpEvent)
	if !ok || ev.ThreadID != "t1" || ev.Command.Kind != AppCommandExecApproval {
		t.Fatalf("expected exec approval to thread t1, got %#v", (*got)[0])
	}
	if ev.Command.Decision != reviewDecisionApproved {
		t.Fatalf("decision should be approved, got %q", ev.Command.Decision)
	}
}

func TestApprovalEscCancelsWithAbort(t *testing.T) {
	sender, got := captureSender()
	o := NewApprovalOverlay(ApprovalRequest{
		Kind: ApprovalExec, ThreadID: "t1", ID: "call-1", Command: []string{"rm"},
	}, sender)
	if o.OnCtrlC() != CancellationHandled {
		t.Fatalf("ctrl+c should be handled")
	}
	if !o.IsComplete() {
		t.Fatalf("overlay should be done after cancel")
	}
	ev := (*got)[0].(SubmitThreadOpEvent)
	if ev.Command.Decision != reviewDecisionAbort {
		t.Fatalf("cancel should emit abort, got %q", ev.Command.Decision)
	}
}

func TestApprovalQueueAdvances(t *testing.T) {
	sender, got := captureSender()
	o := NewApprovalOverlay(ApprovalRequest{Kind: ApprovalExec, ThreadID: "t", ID: "a", Command: []string{"x"}}, sender)
	o.EnqueueRequest(ApprovalRequest{Kind: ApprovalPatch, ThreadID: "t", ID: "b"})
	o.HandleKey(keyMsg("y")) // answer first; advance to queued patch
	if o.IsComplete() {
		t.Fatalf("overlay should not be complete while a queued request remains")
	}
	o.HandleKey(keyMsg("y")) // approve patch
	if !o.IsComplete() {
		t.Fatalf("overlay should complete after queue drained")
	}
	if len(*got) != 2 {
		t.Fatalf("expected two decisions, got %d", len(*got))
	}
	second := (*got)[1].(SubmitThreadOpEvent)
	if second.Command.Kind != AppCommandPatchApproval {
		t.Fatalf("second decision should be a patch approval")
	}
}

func TestApprovalElicitationCancelOnEsc(t *testing.T) {
	sender, got := captureSender()
	o := NewApprovalOverlay(ApprovalRequest{
		Kind: ApprovalMcpElicitation, ThreadID: "t", ServerName: "srv", RequestID: "r1", Message: "approve?",
	}, sender)
	o.HandleKey(keyMsg("esc"))
	ev := (*got)[0].(SubmitThreadOpEvent)
	if ev.Command.Kind != AppCommandResolveElicitation {
		t.Fatalf("expected resolve elicitation, got %#v", ev.Command)
	}
	if !strings.Contains(string(ev.Command.Response), elicitationCancel) {
		t.Fatalf("esc should cancel elicitation, got %s", ev.Command.Response)
	}
}

// --- request_user_input overlay --------------------------------------------

func TestUserInputOptionSubmit(t *testing.T) {
	sender, got := captureSender()
	o := NewRequestUserInputOverlay(UserInputRequest{
		TurnID: "turn-1",
		Questions: []UserInputQuestion{{
			ID: "q1", Question: "Pick", Options: []UserInputOption{{Label: "A"}, {Label: "B"}},
		}},
	}, sender)
	o.HandleKey(keyMsg("down"))  // select B
	o.HandleKey(keyMsg("enter")) // submit (last question)
	if !o.IsComplete() {
		t.Fatalf("overlay should complete after submitting the only question")
	}
	if len(*got) != 1 {
		t.Fatalf("expected one answer event, got %d", len(*got))
	}
	ev := (*got)[0].(CodexOpEvent)
	if ev.Command.Kind != AppCommandUserInputAnswer || ev.Command.TurnID != "turn-1" {
		t.Fatalf("expected user-input answer for turn-1, got %#v", ev.Command)
	}
	if !strings.Contains(string(ev.Command.Response), "\"B\"") {
		t.Fatalf("answer payload should contain selected label B, got %s", ev.Command.Response)
	}
}

func TestUserInputInterruptOnCtrlC(t *testing.T) {
	sender, got := captureSender()
	o := NewRequestUserInputOverlay(UserInputRequest{
		TurnID:    "turn-1",
		Questions: []UserInputQuestion{{ID: "q1", Question: "Notes?"}},
	}, sender)
	if o.OnCtrlC() != CancellationHandled {
		t.Fatalf("ctrl+c should be handled")
	}
	if !o.IsComplete() {
		t.Fatalf("overlay should finish on interrupt")
	}
	ev := (*got)[0].(CodexOpEvent)
	if ev.Command.Kind != AppCommandInterrupt {
		t.Fatalf("ctrl+c should interrupt the turn, got %#v", ev.Command)
	}
}

func TestUserInputQueueFIFO(t *testing.T) {
	sender, _ := captureSender()
	o := NewRequestUserInputOverlay(UserInputRequest{
		TurnID:    "turn-1",
		Questions: []UserInputQuestion{{ID: "q1", Question: "a", Options: []UserInputOption{{Label: "x"}}}},
	}, sender)
	o.EnqueueRequest(UserInputRequest{
		TurnID:    "turn-2",
		Questions: []UserInputQuestion{{ID: "q2", Question: "b", Options: []UserInputOption{{Label: "y"}}}},
	})
	o.submitAnswers()
	if o.request.TurnID != "turn-2" {
		t.Fatalf("queue should advance FIFO to turn-2, got %q", o.request.TurnID)
	}
}

// --- elicitation form overlay ----------------------------------------------

func TestElicitationFormAccept(t *testing.T) {
	sender, got := captureSender()
	o := NewElicitationFormOverlay(ElicitationFormRequest{
		ThreadID: "t", ServerName: "srv", RequestID: "r1", Message: "fill",
		Fields: []ElicitationField{
			{ID: "name", Label: "Name", Kind: ElicitationText},
		},
	}, sender)
	o.HandleKey(keyMsg("h"))
	o.HandleKey(keyMsg("i"))
	o.HandleKey(keyMsg("enter")) // last field -> accept
	if !o.IsComplete() {
		t.Fatalf("form should complete on accept")
	}
	ev := (*got)[0].(SubmitThreadOpEvent)
	if ev.Command.Kind != AppCommandResolveElicitation {
		t.Fatalf("expected resolve elicitation")
	}
	if !strings.Contains(string(ev.Command.Response), elicitationAccept) {
		t.Fatalf("accept action expected, got %s", ev.Command.Response)
	}
}

func TestElicitationFormEscCancels(t *testing.T) {
	sender, got := captureSender()
	o := NewElicitationFormOverlay(ElicitationFormRequest{
		ThreadID: "t", ServerName: "srv", RequestID: "r1",
		Fields: []ElicitationField{{ID: "x", Kind: ElicitationText}},
	}, sender)
	o.HandleKey(keyMsg("esc"))
	ev := (*got)[0].(SubmitThreadOpEvent)
	if !strings.Contains(string(ev.Command.Response), elicitationCancel) {
		t.Fatalf("esc should cancel, got %s", ev.Command.Response)
	}
}

// --- overlay stack ---------------------------------------------------------

func TestOverlayStackPushPrune(t *testing.T) {
	sender, _ := captureSender()
	s := NewOverlayStack()
	if !s.IsEmpty() {
		t.Fatalf("new stack should be empty")
	}
	popup := NewCommandPopup(popupFlags())
	popup.OnComposerTextChange("/model")
	s = s.Push(popup)
	if s.Len() != 1 {
		t.Fatalf("stack should have one view")
	}
	// Accept the popup; HandleKey should prune the completed view.
	s, _ = s.HandleKey(keyMsg("enter"))
	if !s.IsEmpty() {
		t.Fatalf("completed view should be pruned from the stack")
	}
	_ = sender
}

func TestOverlayStackRoutesToTop(t *testing.T) {
	sender, got := captureSender()
	s := NewOverlayStack().Push(NewApprovalOverlay(ApprovalRequest{
		Kind: ApprovalExec, ThreadID: "t", ID: "c", Command: []string{"ls"},
	}, sender))
	// Esc routes through cancellation since approval does not prefer esc-to-key.
	s, _ = s.HandleKey(keyMsg("esc"))
	if !s.IsEmpty() {
		t.Fatalf("approval should be pruned after esc cancel")
	}
	if len(*got) == 0 {
		t.Fatalf("esc should have routed a decision to the engine")
	}
}

// --- render smoke tests ----------------------------------------------------

func renderTheme() Theme { return DefaultTheme(Capabilities{}) }

func TestRenderRowsEmptyAndPopulated(t *testing.T) {
	theme := renderTheme()
	if out := renderRows(theme, nil, NewScrollState(), MaxPopupRows, "empty"); !strings.Contains(out, "empty") {
		t.Fatalf("empty row set should render the empty message, got %q", out)
	}
	rows := []DisplayRow{
		{Name: "alpha", MatchIndices: []int{0, 1}, Description: "first"},
		{Name: "beta", IsDisabled: true, DisabledReason: "nope"},
	}
	st := NewScrollState()
	st.Selected = 0
	out := renderRows(theme, rows, st, MaxPopupRows, "empty")
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Fatalf("rows should render their names, got %q", out)
	}
}

func TestOverlayViewsRenderWithoutPanic(t *testing.T) {
	theme := renderTheme()
	sender, _ := captureSender()
	area := Rect{Width: 60, Height: 12}

	views := []OverlayView{
		func() OverlayView { p := NewCommandPopup(popupFlags()); p.OnComposerTextChange("/m"); return p }(),
		func() OverlayView {
			p := NewFileSearchPopup()
			p.SetQuery("f")
			p.SetMatches("f", []FileMatch{{Path: "a.go", Indices: []int{0}}})
			return p
		}(),
		NewListSelectionOverlay(SelectionViewParams{
			Title: "Pick", Subtitle: "sub", IsSearchable: true,
			Items:           []SelectionItem{{Name: "One", Description: "d", IsCurrent: true}, {Name: "Two", Toggle: &SelectionToggle{}}},
			FooterHint:      "enter to confirm",
			InitialSelected: 0,
		}, sender),
		NewApprovalOverlay(ApprovalRequest{Kind: ApprovalExec, ThreadID: "t", ID: "c", Command: []string{"ls", "-la"}, Reason: "list", ThreadLabel: "agent"}, sender),
		NewRequestUserInputOverlay(UserInputRequest{TurnID: "t", Questions: []UserInputQuestion{
			{ID: "q1", Header: "H", Question: "Pick", IsOther: true, Options: []UserInputOption{{Label: "A", Description: "a"}}},
			{ID: "q2", Question: "Notes?"},
		}}, sender),
		NewElicitationFormOverlay(ElicitationFormRequest{ThreadID: "t", ServerName: "srv", Message: "fill", Fields: []ElicitationField{
			{ID: "s", Label: "Choose", Kind: ElicitationSelect, Options: []ElicitationOption{{Label: "x"}, {Label: "y"}}, DefaultIdx: 0},
			{ID: "t", Label: "Text", Kind: ElicitationText, Secret: true},
		}}, sender),
	}
	for i, v := range views {
		out := v.View(theme, area)
		if out == "" {
			t.Fatalf("view %d rendered empty", i)
		}
		if h := v.DesiredHeight(area.Width); h <= 0 {
			t.Fatalf("view %d desired height should be positive, got %d", i, h)
		}
	}
}

func TestOverlayStackTerminalTitle(t *testing.T) {
	sender, _ := captureSender()
	s := NewOverlayStack()
	if s.TerminalTitleRequiresAction() {
		t.Fatalf("empty stack should not require action")
	}
	s = s.Push(NewApprovalOverlay(ApprovalRequest{Kind: ApprovalExec, ThreadID: "t", ID: "c"}, sender))
	if !s.TerminalTitleRequiresAction() {
		t.Fatalf("approval overlay should require action title")
	}
}
