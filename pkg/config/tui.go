package config

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// SessionPickerViewMode is the layout for the resume/fork session picker. Rust
// serde rename_all = "kebab-case"; default Dense.
type SessionPickerViewMode string

const (
	SessionPickerViewComfortable SessionPickerViewMode = "comfortable"
	SessionPickerViewDense       SessionPickerViewMode = "dense"
)

// NotificationMethod selects the terminal notification mechanism. lowercase;
// default Auto.
type NotificationMethod string

const (
	NotificationMethodAuto NotificationMethod = "auto"
	NotificationMethodOsc9 NotificationMethod = "osc9"
	NotificationMethodBel  NotificationMethod = "bel"
)

// NotificationCondition controls when TUI notifications are delivered. lowercase;
// default Unfocused.
type NotificationCondition string

const (
	NotificationConditionUnfocused NotificationCondition = "unfocused"
	NotificationConditionAlways    NotificationCondition = "always"
)

// TuiPetAnchor controls vertical anchoring of the terminal pet. kebab-case;
// default Composer.
type TuiPetAnchor string

const (
	TuiPetAnchorComposer     TuiPetAnchor = "composer"
	TuiPetAnchorScreenBottom TuiPetAnchor = "screen-bottom"
)

// Notifications is an untagged enum: either a bool (enable/disable) or a custom
// list of strings. Default is Enabled(true).
type Notifications struct {
	// Enabled is set when the value was a boolean.
	Enabled *bool
	// Custom is set when the value was a list of strings.
	Custom *[]string
}

// DefaultNotifications returns the default Enabled(true) form.
func DefaultNotifications() Notifications {
	t := true
	return Notifications{Enabled: &t}
}

// MarshalJSON emits the bool or array form, matching the untagged Rust enum.
func (n Notifications) MarshalJSON() ([]byte, error) {
	if n.Custom != nil {
		return json.Marshal(*n.Custom)
	}
	if n.Enabled != nil {
		return json.Marshal(*n.Enabled)
	}
	return json.Marshal(true)
}

// UnmarshalJSON decodes either a bool or a string array.
func (n *Notifications) UnmarshalJSON(data []byte) error {
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		n.Enabled = &b
		n.Custom = nil
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		n.Custom = &arr
		n.Enabled = nil
		return nil
	}
	return fmt.Errorf("notifications must be a bool or a list of strings, got %s", string(data))
}

// TuiNotificationSettings are flattened into Tui. Field renames mirror serde.
type TuiNotificationSettings struct {
	Notifications Notifications         `json:"notifications" toml:"notifications"`
	Method        NotificationMethod    `json:"notification_method" toml:"notification_method"`
	Condition     NotificationCondition `json:"notification_condition" toml:"notification_condition"`
}

// DefaultTuiNotificationSettings returns the defaults.
func DefaultTuiNotificationSettings() TuiNotificationSettings {
	return TuiNotificationSettings{
		Notifications: DefaultNotifications(),
		Method:        NotificationMethodAuto,
		Condition:     NotificationConditionUnfocused,
	}
}

// ModelAvailabilityNuxConfig tracks per-model NUX shown counts (flattened map).
type ModelAvailabilityNuxConfig struct {
	ShownCount map[string]uint32 `json:"-" toml:"-"`
}

// Tui collects TUI-specific settings. deny_unknown_fields with flattened
// notification settings and model_availability_nux. Several fields default to
// true. The keymap is represented opaquely as a TOML value tree to preserve
// arbitrary keybinding structure faithfully.
type Tui struct {
	NotificationSettings TuiNotificationSettings    `json:"-" toml:"-"`
	Animations           bool                       `json:"animations" toml:"animations"`
	ShowTooltips         bool                       `json:"show_tooltips" toml:"show_tooltips"`
	VimModeDefault       bool                       `json:"vim_mode_default" toml:"vim_mode_default"`
	RawOutputMode        bool                       `json:"raw_output_mode" toml:"raw_output_mode"`
	AlternateScreen      protocol.AltScreenMode     `json:"alternate_screen" toml:"alternate_screen"`
	StatusLine           *[]string                  `json:"status_line" toml:"status_line"`
	StatusLineUseColors  bool                       `json:"status_line_use_colors" toml:"status_line_use_colors"`
	TerminalTitle        *[]string                  `json:"terminal_title" toml:"terminal_title"`
	Theme                *string                    `json:"theme" toml:"theme"`
	Pet                  *string                    `json:"pet" toml:"pet"`
	PetAnchor            TuiPetAnchor               `json:"pet_anchor" toml:"pet_anchor"`
	SessionPickerView    *SessionPickerViewMode     `json:"session_picker_view" toml:"session_picker_view"`
	Keymap               TomlValue                  `json:"keymap" toml:"keymap"`
	ModelAvailabilityNux ModelAvailabilityNuxConfig `json:"-" toml:"-"`
	TerminalReflowRows   *uint64                    `json:"terminal_resize_reflow_max_rows" toml:"terminal_resize_reflow_max_rows"`
}

// DefaultTui returns the Tui with serde defaults applied.
func DefaultTui() Tui {
	return Tui{
		NotificationSettings: DefaultTuiNotificationSettings(),
		Animations:           true,
		ShowTooltips:         true,
		AlternateScreen:      protocol.AltScreenModeAuto,
		StatusLineUseColors:  true,
		PetAnchor:            TuiPetAnchorComposer,
		ModelAvailabilityNux: ModelAvailabilityNuxConfig{ShownCount: map[string]uint32{}},
	}
}
