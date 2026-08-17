package rollout

import (
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func sessionMetaItemForTest(id string, forkedFrom *string) RolloutItem {
	meta := SessionMeta{ID: protocol.NewThreadID(id)}
	if forkedFrom != nil {
		fid := protocol.NewThreadID(*forkedFrom)
		meta.ForkedFromID = &fid
	}
	return NewSessionMetaItem(SessionMetaLine{Meta: meta})
}

func TestInitialHistoryAccessors(t *testing.T) {
	base := BaseInstructions{Text: "be helpful"}
	threadSrc := ThreadSourceUser
	meta := SessionMeta{
		ID:               protocol.NewThreadID(testThreadID),
		Cwd:              "/work",
		Source:           NewCliSource(),
		ThreadSource:     &threadSrc,
		BaseInstructions: &base,
	}
	items := []RolloutItem{
		NewSessionMetaItem(SessionMetaLine{Meta: meta}),
		agentMessageItem("hi"),
	}
	history := InitialHistory{
		Kind:    InitialHistoryKindResumed,
		Resumed: &ResumedHistory{ConversationID: meta.ID, History: items},
	}

	if got := history.GetRolloutItems(); len(got) != 2 {
		t.Fatalf("expected 2 items, got %d", len(got))
	}
	if cwd := history.SessionCwd(); cwd == nil || *cwd != "/work" {
		t.Fatalf("session cwd = %v", cwd)
	}
	if bi := history.GetBaseInstructions(); bi == nil || bi.Text != "be helpful" {
		t.Fatalf("base instructions = %v", bi)
	}
	if ts := history.GetResumedThreadSource(); ts == nil || *ts != ThreadSourceUser {
		t.Fatalf("thread source = %v", ts)
	}
	events := history.GetEventMsgs()
	if len(events) != 1 || events[0].Type != protocol.EventMsgKindAgentMessage {
		t.Fatalf("expected 1 agent message event, got %v", events)
	}
	if sm := history.GetResumedSessionMeta(); sm == nil || sm.ID.String() != testThreadID {
		t.Fatalf("resumed session meta = %v", sm)
	}
}

func TestInitialHistoryForkedFromID(t *testing.T) {
	forkedFrom := "11111111-1111-1111-1111-111111111111"
	resumed := InitialHistory{
		Kind: InitialHistoryKindResumed,
		Resumed: &ResumedHistory{
			History: []RolloutItem{sessionMetaItemForTest(testThreadID, &forkedFrom)},
		},
	}
	if id := resumed.ForkedFromID(); id == nil || id.String() != forkedFrom {
		t.Fatalf("forked_from_id (resumed) = %v", id)
	}

	forked := InitialHistory{
		Kind:   InitialHistoryKindForked,
		Forked: []RolloutItem{sessionMetaItemForTest(testThreadID, nil)},
	}
	if id := forked.ForkedFromID(); id == nil || id.String() != testThreadID {
		t.Fatalf("forked_from_id (forked) = %v", id)
	}

	if NewInitialHistory().ForkedFromID() != nil {
		t.Fatalf("new history should have no forked_from_id")
	}
}

func TestInitialHistoryScanRolloutItems(t *testing.T) {
	history := InitialHistory{
		Kind:   InitialHistoryKindForked,
		Forked: []RolloutItem{agentMessageItem("a"), userMessageItem("b")},
	}
	found := history.ScanRolloutItems(func(item RolloutItem) bool {
		return item.Kind == RolloutItemKindEventMsg && item.EventMsg.Type == protocol.EventMsgKindUserMessage
	})
	if !found {
		t.Fatalf("expected to find a user message item")
	}
	if NewInitialHistory().ScanRolloutItems(func(RolloutItem) bool { return true }) {
		t.Fatalf("new history should scan no items")
	}
}

func TestInteractiveSessionSources(t *testing.T) {
	sources := InteractiveSessionSources()
	if len(sources) != 4 {
		t.Fatalf("expected 4 interactive sources, got %d", len(sources))
	}
	if sources[0].Kind != SessionSourceKindCli || sources[1].Kind != SessionSourceKindVSCode {
		t.Fatalf("unexpected ordering: %v", sources)
	}
	// A fresh slice each call so callers cannot mutate shared state.
	sources[0] = NewExecSource()
	if InteractiveSessionSources()[0].Kind != SessionSourceKindCli {
		t.Fatalf("InteractiveSessionSources must return a fresh slice")
	}
}
