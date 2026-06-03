package websearch

import (
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func message(role, text string) protocol.ResponseItem {
	var content protocol.ContentItem
	if role == assistantRole {
		content = protocol.ContentItem{Type: protocol.ContentItemKindOutputText, Text: text}
	} else {
		content = protocol.ContentItem{Type: protocol.ContentItemKindInputText, Text: text}
	}
	return protocol.ResponseItem{
		Type:    protocol.ResponseItemKindMessage,
		Role:    role,
		Content: []protocol.ContentItem{content},
	}
}

func wantItems(items ...protocol.ResponseItem) *SearchInput {
	in := ItemsSearchInput(items)
	return &in
}

func TestRecentInputKeepsCurrentAndPreviousVisibleTurn(t *testing.T) {
	items := []protocol.ResponseItem{
		message("system", "system"),
		message(userRole, "old user"),
		message(assistantRole, "old assistant"),
		message(userRole, "previous user"),
		{
			Type:      protocol.ResponseItemKindFunctionCall,
			Name:      "tool",
			Arguments: "{}",
			CallID:    "call-1",
		},
		message(assistantRole, "previous assistant"),
		message("developer", "developer"),
		message(userRole, "current user"),
		message(assistantRole, "current commentary"),
	}

	got := RecentInput(items)
	want := wantItems(
		message(userRole, "previous user"),
		message(assistantRole, "previous assistant"),
		message(userRole, "current user"),
	)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestRecentInputKeepsOnlyTextFromRecentUserMessages(t *testing.T) {
	previousUser := protocol.ResponseItem{
		Type: protocol.ResponseItemKindMessage,
		Role: userRole,
		Content: []protocol.ContentItem{
			{Type: protocol.ContentItemKindInputText, Text: "previous user"},
			{Type: protocol.ContentItemKindInputImage, ImageURL: "data:image/png;base64,image"},
		},
	}
	items := []protocol.ResponseItem{
		previousUser,
		message(assistantRole, "previous assistant"),
		message(userRole, "current user"),
	}

	got := RecentInput(items)
	want := wantItems(
		message(userRole, "previous user"),
		message(assistantRole, "previous assistant"),
		message(userRole, "current user"),
	)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestRecentInputIgnoresContextualUserMessages(t *testing.T) {
	items := []protocol.ResponseItem{
		message(userRole, "previous user"),
		message(assistantRole, "previous assistant"),
		message(userRole, "<environment_context>\n<cwd>/tmp</cwd>\n</environment_context>"),
		message(userRole, "current user"),
	}

	got := RecentInput(items)
	want := wantItems(
		message(userRole, "previous user"),
		message(assistantRole, "previous assistant"),
		message(userRole, "current user"),
	)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestRecentInputEmptyHistoryReturnsNil(t *testing.T) {
	if got := RecentInput(nil); got != nil {
		t.Errorf("got %#v, want nil", got)
	}
	if got := RecentInput([]protocol.ResponseItem{message("system", "s")}); got != nil {
		t.Errorf("got %#v, want nil", got)
	}
}

func TestRetainTailFromLastNUserMessages(t *testing.T) {
	items := []protocol.ResponseItem{
		message("system", "system"),
		message(userRole, "old user"),
		message(assistantRole, "old assistant"),
		message(userRole, "previous user"),
		message(assistantRole, "previous assistant"),
		message(userRole, "current user"),
		message(assistantRole, "later assistant"),
	}
	retainTailFromLastNUserMessages(&items, 2)
	want := []protocol.ResponseItem{
		message(userRole, "previous user"),
		message(assistantRole, "previous assistant"),
		message(userRole, "current user"),
	}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("got %#v, want %#v", items, want)
	}
}

func TestTruncateAssistantOutputTextDropsAfterBudget(t *testing.T) {
	items := []protocol.ResponseItem{
		message(userRole, "previous user"),
		message(assistantRole, "short"),
		message(assistantRole, "after budget"),
		message(userRole, "current user"),
	}
	// Budget large enough for "short" but the second assistant item's text is
	// dropped once the budget is exhausted, removing the now-empty message.
	truncateAssistantOutputTextToTokenBudget(&items, 2)

	// "short" fits within the 2-token budget; "after budget" then sees a zero
	// budget and is removed entirely.
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3: %#v", len(items), items)
	}
	if items[1].Content[0].Text != "short" {
		t.Errorf("kept assistant text = %q, want short", items[1].Content[0].Text)
	}
}
