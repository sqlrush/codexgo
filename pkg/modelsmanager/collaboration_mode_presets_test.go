package modelsmanager

import (
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func TestPresetNamesUseModeDisplayNames(t *testing.T) {
	plan := planPreset()
	def := defaultPreset()

	if plan.Name != modeDisplayName(protocol.ModeKindPlan) {
		t.Fatalf("plan preset name = %q, want %q", plan.Name, modeDisplayName(protocol.ModeKindPlan))
	}
	if def.Name != modeDisplayName(protocol.ModeKindDefault) {
		t.Fatalf("default preset name = %q, want %q", def.Name, modeDisplayName(protocol.ModeKindDefault))
	}
	if plan.Model != nil {
		t.Fatalf("plan preset model should be nil")
	}
	// reasoning_effort == Some(Some(Medium)) -> outer non-nil, inner non-nil.
	if plan.ReasoningEffort == nil || *plan.ReasoningEffort == nil || **plan.ReasoningEffort != protocol.ReasoningEffortMedium {
		t.Fatalf("plan preset reasoning effort should be Some(Some(Medium))")
	}
	if def.Model != nil {
		t.Fatalf("default preset model should be nil")
	}
	if def.ReasoningEffort != nil {
		t.Fatalf("default preset reasoning effort should be None")
	}
}

func TestDefaultModeInstructionsReplaceModeNamesPlaceholder(t *testing.T) {
	def := defaultPreset()
	if def.DeveloperInstructions == nil || *def.DeveloperInstructions == nil {
		t.Fatalf("default preset should include instructions")
	}
	instructions := **def.DeveloperInstructions

	if strings.Contains(instructions, "{{KNOWN_MODE_NAMES}}") {
		t.Fatalf("placeholder should be substituted")
	}

	knownModeNames := formatModeNames(tuiVisibleCollaborationModes)
	expectedSnippet := "Known mode names are " + knownModeNames + "."
	if !strings.Contains(instructions, expectedSnippet) {
		t.Fatalf("instructions missing snippet %q", expectedSnippet)
	}
	if !strings.Contains(instructions, "Use the `request_user_input` tool only when it is listed in the available tools") {
		t.Fatalf("instructions missing request_user_input guidance")
	}
	if !strings.Contains(instructions, "ask the user directly with a concise plain-text question") {
		t.Fatalf("instructions missing plain-text question guidance")
	}
}

func TestBuiltinCollaborationModePresetsOrder(t *testing.T) {
	presets := BuiltinCollaborationModePresets()
	if len(presets) != 2 {
		t.Fatalf("expected 2 presets, got %d", len(presets))
	}
	if presets[0].Name != "Plan" {
		t.Fatalf("first preset should be Plan, got %q", presets[0].Name)
	}
	if presets[1].Name != "Default" {
		t.Fatalf("second preset should be Default, got %q", presets[1].Name)
	}
}

func TestFormatModeNames(t *testing.T) {
	tests := []struct {
		name  string
		modes []protocol.ModeKind
		want  string
	}{
		{"empty", nil, "none"},
		{"one", []protocol.ModeKind{protocol.ModeKindPlan}, "Plan"},
		{"two", []protocol.ModeKind{protocol.ModeKindDefault, protocol.ModeKindPlan}, "Default and Plan"},
		{"three", []protocol.ModeKind{protocol.ModeKindDefault, protocol.ModeKindPlan, protocol.ModeKindDefault}, "Default, Plan, Default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatModeNames(tt.modes); got != tt.want {
				t.Fatalf("formatModeNames = %q, want %q", got, tt.want)
			}
		})
	}
}
