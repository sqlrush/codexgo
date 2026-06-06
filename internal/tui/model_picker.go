package tui

// /model picker: a ListSelectionOverlay over the available models.
//
// The entry list is supplied by the host (cli) and combines the bundled
// OpenAI catalog's picker-visible presets with the custom-provider models
// declared in config.toml ([model_providers.X].models — the codexgo routing
// extension). Selecting an entry emits [ModelSelectedEvent]; the app loop
// persists the choice and starts a fresh session with the model override so
// the model→provider routing takes effect immediately.

import "fmt"

// ModelPickerEntry is one selectable model in the /model picker.
type ModelPickerEntry struct {
	// Slug is the model identifier sent on the wire (e.g. "gpt-5.5").
	Slug string
	// DisplayName is the friendly name shown next to the slug (may be empty).
	DisplayName string
	// Description is the one-line description (may be empty).
	Description string
}

// ModelSelectedEvent reports that the user picked a model in the /model picker.
type ModelSelectedEvent struct {
	BaseAppEvent
	// Slug is the selected model slug.
	Slug string
}

// compile-time assertion that ModelSelectedEvent satisfies AppEvent.
var _ AppEvent = ModelSelectedEvent{}

// BuildModelPickerParams assembles the SelectionViewParams for the /model
// picker. currentSlug marks the active model with the "(current)" tag.
func BuildModelPickerParams(entries []ModelPickerEntry, currentSlug string) SelectionViewParams {
	items := make([]SelectionItem, 0, len(entries))
	initial := -1
	for i, entry := range entries {
		slug := entry.Slug
		description := entry.Description
		if entry.DisplayName != "" && entry.DisplayName != slug {
			if description != "" {
				description = fmt.Sprintf("%s — %s", entry.DisplayName, description)
			} else {
				description = entry.DisplayName
			}
		}
		isCurrent := slug == currentSlug
		if isCurrent {
			initial = i
		}
		items = append(items, SelectionItem{
			Name:            slug,
			Description:     description,
			IsCurrent:       isCurrent,
			SearchValue:     slug + " " + entry.DisplayName,
			DismissOnSelect: true,
			Actions: []SelectionAction{
				func(sender *AppEventSender) {
					sender.Send(ModelSelectedEvent{Slug: slug})
				},
			},
		})
	}
	return SelectionViewParams{
		ViewID:            "model-picker",
		Title:             "Select model",
		Subtitle:          "Switch the model for this and future sessions",
		FooterHint:        "press enter to confirm or esc to go back",
		Items:             items,
		IsSearchable:      true,
		SearchPlaceholder: "Type to filter models",
		InitialSelected:   initial,
	}
}
