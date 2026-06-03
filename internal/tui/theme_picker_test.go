package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestListAvailableThemesSorted(t *testing.T) {
	entries := ListAvailableThemes(nil)
	if len(entries) < 2 {
		t.Fatalf("expected at least the built-in themes, got %d", len(entries))
	}
	// Built-ins are sorted by name.
	for i := 1; i < len(entries); i++ {
		if entries[i-1].IsCustom {
			break
		}
		if entries[i].IsCustom {
			break
		}
		if entries[i-1].Name > entries[i].Name {
			t.Fatalf("themes not sorted: %q before %q", entries[i-1].Name, entries[i].Name)
		}
	}
}

func TestListAvailableThemesCustomAppended(t *testing.T) {
	entries := ListAvailableThemes([]string{"mytheme"})
	last := entries[len(entries)-1]
	if last.Name != "mytheme" || !last.IsCustom {
		t.Fatalf("custom theme = %+v", last)
	}
}

func TestBuildThemePickerParamsPreselectsCurrent(t *testing.T) {
	params := BuildThemePickerParams("light", nil)
	if params.Title != "Select Syntax Theme" {
		t.Fatalf("title = %q", params.Title)
	}
	if !params.IsSearchable {
		t.Fatal("theme picker should be searchable")
	}
	if params.InitialSelected < 0 {
		t.Fatal("expected a preselected current theme")
	}
	if params.Items[params.InitialSelected].SearchValue != "light" {
		t.Fatalf("preselected = %q, want light", params.Items[params.InitialSelected].SearchValue)
	}
}

func TestBuildThemePickerParamsUnknownFallsBack(t *testing.T) {
	params := BuildThemePickerParams("not-a-real-theme", nil)
	if params.InitialSelected < 0 {
		t.Fatal("expected fallback selection")
	}
	if params.Items[params.InitialSelected].SearchValue != ConfiguredThemeName() {
		t.Fatalf("fallback = %q, want %q",
			params.Items[params.InitialSelected].SearchValue, ConfiguredThemeName())
	}
}

func TestBuildThemePickerParamsItemsHaveSearchValues(t *testing.T) {
	params := BuildThemePickerParams("", nil)
	for _, item := range params.Items {
		if item.SearchValue == "" {
			t.Fatalf("item %q missing search value", item.Name)
		}
		if !item.DismissOnSelect {
			t.Fatalf("item %q should dismiss on select", item.Name)
		}
		if len(item.Actions) != 1 {
			t.Fatalf("item %q should have one action", item.Name)
		}
	}
}

func TestThemePickerActionEmitsSelectedEvent(t *testing.T) {
	params := BuildThemePickerParams("default", nil)
	var got AppEvent
	sender := NewAppEventSender()
	sender.attachFunc(func(msg tea.Msg) {
		if ev, ok := msg.(AppEvent); ok {
			got = ev
		}
	})
	params.Items[0].Actions[0](sender)
	sel, ok := got.(SyntaxThemeSelectedEvent)
	if !ok {
		t.Fatalf("expected SyntaxThemeSelectedEvent, got %T", got)
	}
	if sel.Name != params.Items[0].SearchValue {
		t.Fatalf("selected name = %q, want %q", sel.Name, params.Items[0].SearchValue)
	}
}
