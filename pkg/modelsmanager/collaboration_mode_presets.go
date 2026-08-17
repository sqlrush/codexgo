package modelsmanager

import (
	"fmt"
	"strings"

	_ "embed"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// collaborationModePlan is the bundled "plan" collaboration-mode template.
// Rust: codex_collaboration_mode_templates::PLAN.
//
//go:embed template_plan.md
var collaborationModePlan string

// collaborationModeDefault is the bundled "default" collaboration-mode template.
// Rust: codex_collaboration_mode_templates::DEFAULT.
//
//go:embed template_default.md
var collaborationModeDefault string

// knownModeNamesTemplateKey is the placeholder filled in the default template.
const knownModeNamesTemplateKey = "KNOWN_MODE_NAMES"

// tuiVisibleCollaborationModes mirrors Rust's
// TUI_VISIBLE_COLLABORATION_MODES = [Default, Plan].
var tuiVisibleCollaborationModes = []protocol.ModeKind{
	protocol.ModeKindDefault,
	protocol.ModeKindPlan,
}

// BuiltinCollaborationModePresets returns the static set of collaboration-mode
// presets. Rust: builtin_collaboration_mode_presets (returns [plan, default]).
func BuiltinCollaborationModePresets() []protocol.CollaborationModeMask {
	return []protocol.CollaborationModeMask{planPreset(), defaultPreset()}
}

// planPreset builds the "Plan" collaboration-mode mask. Rust: plan_preset.
func planPreset() protocol.CollaborationModeMask {
	mode := protocol.ModeKindPlan
	effort := protocol.ReasoningEffortMedium
	effortPtr := &effort
	instructions := collaborationModePlan
	instructionsPtr := &instructions
	return protocol.CollaborationModeMask{
		Name:                  modeDisplayName(protocol.ModeKindPlan),
		Mode:                  &mode,
		Model:                 nil,
		ReasoningEffort:       &effortPtr,
		DeveloperInstructions: &instructionsPtr,
	}
}

// defaultPreset builds the "Default" collaboration-mode mask. Rust:
// default_preset.
func defaultPreset() protocol.CollaborationModeMask {
	mode := protocol.ModeKindDefault
	instructions := defaultModeInstructions()
	instructionsPtr := &instructions
	return protocol.CollaborationModeMask{
		Name:                  modeDisplayName(protocol.ModeKindDefault),
		Mode:                  &mode,
		Model:                 nil,
		ReasoningEffort:       nil,
		DeveloperInstructions: &instructionsPtr,
	}
}

// defaultModeInstructions renders the default-mode template with the visible mode
// names substituted in. Rust: default_mode_instructions (panics on parse/render
// failure; the bundled template is trusted).
func defaultModeInstructions() string {
	knownModeNames := formatModeNames(tuiVisibleCollaborationModes)
	tmpl, err := parseTemplate(collaborationModeDefault)
	if err != nil {
		panic(fmt.Sprintf("collaboration mode default template must parse: %v", err))
	}
	rendered, err := tmpl.render(map[string]string{knownModeNamesTemplateKey: knownModeNames})
	if err != nil {
		panic(fmt.Sprintf("collaboration mode default template must render: %v", err))
	}
	return rendered
}

// formatModeNames joins mode display names into a human-readable list. Rust:
// format_mode_names.
func formatModeNames(modes []protocol.ModeKind) string {
	names := make([]string, 0, len(modes))
	for _, mode := range modes {
		names = append(names, modeDisplayName(mode))
	}
	switch len(names) {
	case 0:
		return "none"
	case 1:
		return names[0]
	case 2:
		return fmt.Sprintf("%s and %s", names[0], names[1])
	default:
		return strings.Join(names, ", ")
	}
}

// modeDisplayName returns the human-readable label for a ModeKind. Rust:
// ModeKind::display_name.
func modeDisplayName(mode protocol.ModeKind) string {
	switch mode {
	case protocol.ModeKindPlan:
		return "Plan"
	case protocol.ModeKindDefault:
		return "Default"
	default:
		return string(mode)
	}
}
