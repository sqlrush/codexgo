package tui

import "testing"

func TestSlashCommandCanonicalNamesAndAliases(t *testing.T) {
	if SlashStop.Command() != "stop" {
		t.Fatalf("stop command = %q", SlashStop.Command())
	}
	if cmd, ok := ParseSlashCommandName("clean"); !ok || cmd != SlashStop {
		t.Fatalf("clean alias = %v ok=%v", cmd, ok)
	}
	if SlashPets.Command() != "pets" {
		t.Fatalf("pets command = %q", SlashPets.Command())
	}
	if cmd, ok := ParseSlashCommandName("pet"); !ok || cmd != SlashPets {
		t.Fatalf("pet alias = %v ok=%v", cmd, ok)
	}
	if cmd, ok := ParseSlashCommandName("approve"); !ok || cmd != SlashAutoReview {
		t.Fatalf("approve = %v ok=%v", cmd, ok)
	}
}

func TestSlashCommandAvailability(t *testing.T) {
	if !SlashGoal.AvailableDuringTask() {
		t.Fatal("goal should be available during task")
	}
	if SlashModel.AvailableDuringTask() {
		t.Fatal("model should not be available during task")
	}
	if !SlashRaw.AvailableInSideConversation() {
		t.Fatal("raw should be available in side conversation")
	}
	if !SlashRaw.SupportsInlineArgs() {
		t.Fatal("raw should support inline args")
	}
}

func TestResolveSlashOutcomeEvents(t *testing.T) {
	if out := ResolveSlashOutcome(SlashNew, "", SlashSourceLive); out.Kind != OutcomeEmitEvent {
		t.Fatalf("/new kind = %v", out.Kind)
	} else if _, ok := out.Event.(NewSessionEvent); !ok {
		t.Fatalf("/new event = %T", out.Event)
	}

	if out := ResolveSlashOutcome(SlashClear, "", SlashSourceLive); out.Kind != OutcomeEmitEvent {
		t.Fatalf("/clear kind = %v", out.Kind)
	} else if _, ok := out.Event.(ClearUIEvent); !ok {
		t.Fatalf("/clear event = %T", out.Event)
	}

	out := ResolveSlashOutcome(SlashQuit, "", SlashSourceLive)
	ev, ok := out.Event.(ExitEvent)
	if !ok || ev.Mode != ExitShutdownFirst {
		t.Fatalf("/quit event = %+v ok=%v", out.Event, ok)
	}
}

func TestResolveSlashOutcomeCompact(t *testing.T) {
	out := ResolveSlashOutcome(SlashCompact, "", SlashSourceLive)
	co, ok := out.Event.(CodexOpEvent)
	if !ok || co.Command.Kind != AppCommandCompact {
		t.Fatalf("/compact = %+v ok=%v", out.Event, ok)
	}
}

func TestResolveSlashOutcomePickers(t *testing.T) {
	cases := map[SlashCommand]PickerID{
		SlashModel:       PickerModel,
		SlashTheme:       PickerTheme,
		SlashPermissions: PickerPermissions,
		SlashKeymap:      PickerKeymap,
		SlashAgent:       PickerAgent,
		SlashMultiAgents: PickerAgent,
	}
	for cmd, want := range cases {
		out := ResolveSlashOutcome(cmd, "", SlashSourceLive)
		if out.Kind != OutcomeOpenPicker || out.Picker != want {
			t.Fatalf("%s -> kind=%v picker=%v, want %v", cmd.Command(), out.Kind, out.Picker, want)
		}
	}
}

func TestResolveSlashOutcomeToggles(t *testing.T) {
	if out := ResolveSlashOutcome(SlashVim, "", SlashSourceLive); out.Kind != OutcomeToggleState || out.Toggle != ToggleVim {
		t.Fatalf("/vim = %+v", out)
	}
	if out := ResolveSlashOutcome(SlashRaw, "", SlashSourceLive); out.Kind != OutcomeToggleState || out.Toggle != ToggleRawOutput {
		t.Fatalf("/raw = %+v", out)
	}
}

func TestResolveSlashOutcomeInlineArgsCarried(t *testing.T) {
	out := ResolveSlashOutcome(SlashReview, "fix the bug", SlashSourceLive)
	if out.Kind != OutcomeOpenPicker || out.Picker != PickerReview || out.Text != "fix the bug" {
		t.Fatalf("/review args = %+v", out)
	}
}

func TestResolveSlashOutcomeMention(t *testing.T) {
	out := ResolveSlashOutcome(SlashMention, "", SlashSourceLive)
	if out.Kind != OutcomeInsertText || out.Text != "@" {
		t.Fatalf("/mention = %+v", out)
	}
}

func TestResolveSlashOutcomeDiff(t *testing.T) {
	out := ResolveSlashOutcome(SlashDiff, "", SlashSourceLive)
	if out.Kind != OutcomeRunCommand || out.RunID != "diff" {
		t.Fatalf("/diff = %+v", out)
	}
}

func TestResolveSlashOutcomeInit(t *testing.T) {
	out := ResolveSlashOutcome(SlashInit, "", SlashSourceLive)
	if out.Kind != OutcomeSubmitTurn || out.Text == "" {
		t.Fatalf("/init = %+v", out)
	}
}

func TestEmitOutcomeCmd(t *testing.T) {
	// Event outcomes are emittable by the foundation.
	if _, ok := EmitOutcomeCmd(ResolveSlashOutcome(SlashNew, "", SlashSourceLive)); !ok {
		t.Fatal("/new outcome should be emittable")
	}
	if _, ok := EmitOutcomeCmd(ResolveSlashOutcome(SlashInit, "", SlashSourceLive)); !ok {
		t.Fatal("/init outcome should be emittable")
	}
	// Picker outcomes are area-specific.
	if _, ok := EmitOutcomeCmd(ResolveSlashOutcome(SlashModel, "", SlashSourceLive)); ok {
		t.Fatal("/model outcome is area-specific and should not be emittable here")
	}
}
