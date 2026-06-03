package tui

import "sort"

// SyntaxThemeSelectedEvent persists and applies a chosen syntax theme.
//
// Port of app_event.rs AppEvent::SyntaxThemeSelected. The app loop persists
// `[tui] theme = "<name>"` to config.toml and rebuilds the active theme.
type SyntaxThemeSelectedEvent struct {
	BaseAppEvent
	// Name is the canonical theme name to select.
	Name string
}

// SyntaxThemePreviewedEvent requests a repaint after a live theme preview swap.
//
// Port of app_event.rs AppEvent::SyntaxThemePreviewed.
type SyntaxThemePreviewedEvent struct {
	BaseAppEvent
}

// compile-time assertions for the theme-picker events.
var (
	_ AppEvent = SyntaxThemeSelectedEvent{}
	_ AppEvent = SyntaxThemePreviewedEvent{}
)

// ThemeEntry describes one selectable theme in the picker.
//
// Port of the highlight::ThemeEntry record used by theme_picker.rs.
type ThemeEntry struct {
	// Name is the canonical theme name (the value persisted to [tui].theme).
	Name string
	// IsCustom marks user-provided .tmTheme files (rendered with a "(custom)"
	// suffix). The Go port currently ships only built-in themes, so this is
	// always false; the field is retained for parity and future custom themes.
	IsCustom bool
}

// ListAvailableThemes returns the selectable themes (built-ins, sorted by name)
// plus any custom themes the caller supplies. customNames may be nil.
//
// Port of highlight::list_available_themes, reduced to the foundation's built-in
// palette set. Custom .tmTheme discovery under {CODEX_HOME}/themes is out of
// scope for the foundation and is injected by the caller when available.
func ListAvailableThemes(customNames []string) []ThemeEntry {
	builtins := make([]string, 0, len(builtinThemes))
	for name := range builtinThemes {
		builtins = append(builtins, name)
	}
	sort.Strings(builtins)

	entries := make([]ThemeEntry, 0, len(builtins)+len(customNames))
	for _, name := range builtins {
		entries = append(entries, ThemeEntry{Name: name})
	}
	for _, name := range customNames {
		entries = append(entries, ThemeEntry{Name: name, IsCustom: true})
	}
	return entries
}

// ConfiguredThemeName returns the default theme name used when no explicit,
// currently-available preference exists.
//
// Port of highlight::configured_theme_name (the adaptive default). The
// foundation's adaptive default is "default".
func ConfiguredThemeName() string { return "default" }

// ThemePickerSubtitle returns the subtitle shown under the theme picker title.
//
// Port of theme_picker::theme_picker_subtitle, simplified: the foundation has no
// custom-theme directory, so the picker always shows the live-preview hint.
func ThemePickerSubtitle() string {
	return "Move up/down to live preview themes"
}

// BuildThemePickerParams builds [SelectionViewParams] for the /theme picker.
//
// Port of theme_picker::build_theme_picker_params. It lists available themes
// with live preview on cursor movement and cancel-restore. currentName is the
// persisted [tui].theme preference; when it names an available theme the picker
// preselects it, otherwise it falls back to the configured/default selection.
//
// The wide side-by-side syntax preview and the narrow stacked fallback from the
// Rust picker are a rendering concern owned by the overlay/render area; this
// builder produces the behavioral picker (items, preview/cancel callbacks,
// selection) that drives them.
func BuildThemePickerParams(currentName string, customNames []string) SelectionViewParams {
	entries := ListAvailableThemes(customNames)

	// Resolve the effective theme name: honor an explicit preference only when it
	// is currently available; otherwise fall back to the configured/default name.
	effective := ConfiguredThemeName()
	if currentName != "" && themeEntriesContain(entries, currentName) {
		effective = currentName
	}

	initialIdx := -1
	items := make([]SelectionItem, 0, len(entries))
	previewNames := make([]string, 0, len(entries))
	for idx, entry := range entries {
		display := entry.Name
		if entry.IsCustom {
			display = entry.Name + " (custom)"
		}
		isCurrent := entry.Name == effective
		if isCurrent {
			initialIdx = idx
		}
		name := entry.Name // capture for the action closure
		items = append(items, SelectionItem{
			Name:            display,
			IsCurrent:       isCurrent,
			DismissOnSelect: true,
			SearchValue:     entry.Name,
			Actions: []SelectionAction{
				func(sender *AppEventSender) {
					sender.Send(SyntaxThemeSelectedEvent{Name: name})
				},
			},
		})
		previewNames = append(previewNames, entry.Name)
	}

	// Live preview on cursor movement: emit a previewed event so the app loop
	// applies the highlighted theme and repaints.
	onSelectionChanged := func(actualIdx int, sender *AppEventSender) {
		if actualIdx < 0 || actualIdx >= len(previewNames) {
			return
		}
		sender.Send(SyntaxThemePreviewedEvent{})
	}
	// On cancel, request a repaint so the original theme is restored by the app
	// loop (which snapshots the theme when the picker opens).
	onCancel := func(sender *AppEventSender) {
		sender.Send(SyntaxThemePreviewedEvent{})
	}

	return SelectionViewParams{
		ViewID:             "theme-picker",
		Title:              "Select Syntax Theme",
		Subtitle:           ThemePickerSubtitle(),
		Items:              items,
		IsSearchable:       true,
		SearchPlaceholder:  "Type to filter themes...",
		InitialSelected:    initialIdx,
		OnSelectionChanged: onSelectionChanged,
		OnCancel:           onCancel,
	}
}

func themeEntriesContain(entries []ThemeEntry, name string) bool {
	for _, e := range entries {
		if e.Name == name {
			return true
		}
	}
	return false
}
