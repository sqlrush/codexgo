package extensionapi

import (
	"context"
	"sync"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

type counterValue struct{ n int }

type otherValue struct{ s string }

func TestExtensionDataInsertGetRemove(t *testing.T) {
	d := NewExtensionData("level-1")
	if d.LevelID() != "level-1" {
		t.Fatalf("LevelID = %q, want level-1", d.LevelID())
	}

	if _, ok := ExtensionDataGet[counterValue](d); ok {
		t.Fatal("expected no value before insert")
	}

	prev, had := ExtensionDataInsert(d, counterValue{n: 1})
	if had {
		t.Fatalf("first insert reported a previous value: %+v", prev)
	}
	got, ok := ExtensionDataGet[counterValue](d)
	if !ok || got.n != 1 {
		t.Fatalf("get after insert = %+v ok=%v", got, ok)
	}

	prev, had = ExtensionDataInsert(d, counterValue{n: 2})
	if !had || prev.n != 1 {
		t.Fatalf("second insert previous = %+v had=%v, want n=1", prev, had)
	}

	// A distinct Go type occupies a distinct slot.
	if _, ok := ExtensionDataGet[otherValue](d); ok {
		t.Fatal("otherValue should not exist")
	}
	ExtensionDataInsert(d, otherValue{s: "x"})
	if got, ok := ExtensionDataGet[otherValue](d); !ok || got.s != "x" {
		t.Fatalf("otherValue get = %+v ok=%v", got, ok)
	}
	// counterValue is untouched by the otherValue insert.
	if got, ok := ExtensionDataGet[counterValue](d); !ok || got.n != 2 {
		t.Fatalf("counterValue after otherValue insert = %+v ok=%v", got, ok)
	}

	removed, ok := ExtensionDataRemove[counterValue](d)
	if !ok || removed.n != 2 {
		t.Fatalf("remove = %+v ok=%v", removed, ok)
	}
	if _, ok := ExtensionDataGet[counterValue](d); ok {
		t.Fatal("value still present after remove")
	}
	if _, ok := ExtensionDataRemove[counterValue](d); ok {
		t.Fatal("remove of absent value reported ok")
	}
}

func TestExtensionDataGetOrInit(t *testing.T) {
	d := NewExtensionData("level")
	calls := 0
	first := ExtensionDataGetOrInit(d, func() counterValue {
		calls++
		return counterValue{n: 7}
	})
	if first.n != 7 || calls != 1 {
		t.Fatalf("first get_or_init = %+v calls=%d", first, calls)
	}
	second := ExtensionDataGetOrInit(d, func() counterValue {
		calls++
		return counterValue{n: 99}
	})
	if second.n != 7 || calls != 1 {
		t.Fatalf("second get_or_init = %+v calls=%d (init ran again)", second, calls)
	}
}

func TestExtensionDataConcurrentAccess(t *testing.T) {
	d := NewExtensionData("level")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ExtensionDataGetOrInit(d, func() counterValue { return counterValue{n: 1} })
			_, _ = ExtensionDataGet[counterValue](d)
		}()
	}
	wg.Wait()
	if got, ok := ExtensionDataGet[counterValue](d); !ok || got.n != 1 {
		t.Fatalf("after concurrent access = %+v ok=%v", got, ok)
	}
}

func TestPromptFragmentConstructors(t *testing.T) {
	tests := []struct {
		name string
		frag PromptFragment
		slot PromptSlot
		text string
	}{
		{"policy", DeveloperPolicyFragment("p"), PromptSlotDeveloperPolicy, "p"},
		{"capability", DeveloperCapabilityFragment("c"), PromptSlotDeveloperCapabilities, "c"},
		{"separate", SeparateDeveloperFragment("s"), PromptSlotSeparateDeveloper, "s"},
		{"contextual", NewPromptFragment(PromptSlotContextualUser, "u"), PromptSlotContextualUser, "u"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.frag.Slot() != tc.slot {
				t.Errorf("Slot = %v, want %v", tc.frag.Slot(), tc.slot)
			}
			if tc.frag.Text() != tc.text {
				t.Errorf("Text = %q, want %q", tc.frag.Text(), tc.text)
			}
		})
	}
}

func TestNoopCapabilities(t *testing.T) {
	NoopExtensionEventSink{}.Emit(protocol.Event{ID: "x"})

	items := []tools.ResponseInputItem{}
	out, err := NoopResponseItemInjector{}.InjectResponseItems(context.Background(), items)
	if err != ErrInjectionUnavailable {
		t.Fatalf("err = %v, want ErrInjectionUnavailable", err)
	}
	if len(out) != 0 {
		t.Fatalf("out len = %d, want 0 (unchanged)", len(out))
	}
}

// stubReviewContributor records its prompt and returns a configured result.
type stubReviewContributor struct {
	decision protocol.ReviewDecision
	claim    bool
	seen     *string
}

func (s stubReviewContributor) Contribute(_ context.Context, _, _ *ExtensionData, prompt string) (protocol.ReviewDecision, bool) {
	if s.seen != nil {
		*s.seen = prompt
	}
	return s.decision, s.claim
}

func TestRegistryApprovalReviewFirstClaimWins(t *testing.T) {
	var firstSeen, secondSeen string
	first := stubReviewContributor{claim: false, seen: &firstSeen}
	second := stubReviewContributor{
		decision: protocol.NewReviewDecisionApproved(),
		claim:    true,
		seen:     &secondSeen,
	}
	third := stubReviewContributor{
		decision: protocol.NewReviewDecisionDenied(),
		claim:    true,
	}

	b := NewExtensionRegistryBuilder[struct{}]()
	b.AddApprovalReviewContributor(first)
	b.AddApprovalReviewContributor(second)
	b.AddApprovalReviewContributor(third)
	reg := b.Build()

	decision, ok := reg.ApprovalReview(context.Background(), NewExtensionData("s"), NewExtensionData("t"), "prompt-text")
	if !ok {
		t.Fatal("expected a decision")
	}
	if decision.Kind != protocol.ReviewDecisionApproved {
		t.Fatalf("decision = %v, want approved (second contributor)", decision.Kind)
	}
	if firstSeen != "prompt-text" || secondSeen != "prompt-text" {
		t.Fatalf("contributors did not observe prompt: first=%q second=%q", firstSeen, secondSeen)
	}
}

func TestRegistryApprovalReviewNoClaim(t *testing.T) {
	b := NewExtensionRegistryBuilder[struct{}]()
	b.AddApprovalReviewContributor(stubReviewContributor{claim: false})
	reg := b.Build()
	if _, ok := reg.ApprovalReview(context.Background(), NewExtensionData("s"), NewExtensionData("t"), "p"); ok {
		t.Fatal("expected no decision when nobody claims")
	}
}

func TestEmptyRegistry(t *testing.T) {
	reg := EmptyExtensionRegistry[struct{}]()
	if reg.EventSink() == nil {
		t.Fatal("event sink should default to noop, not nil")
	}
	if reg.ThreadLifecycleContributors() != nil {
		t.Fatal("expected nil thread contributors")
	}
	if _, ok := reg.ApprovalReview(context.Background(), NewExtensionData("s"), NewExtensionData("t"), "p"); ok {
		t.Fatal("empty registry should not claim approvals")
	}
}

type recordingSink struct {
	mu     sync.Mutex
	events []protocol.Event
}

func (r *recordingSink) Emit(e protocol.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func TestRegistryBuilderEventSink(t *testing.T) {
	sink := &recordingSink{}
	b := NewExtensionRegistryBuilderWithEventSink[struct{}](sink)
	if b.EventSink() != sink {
		t.Fatal("builder did not retain provided sink")
	}
	reg := b.Build()
	reg.EventSink().Emit(protocol.Event{ID: "evt"})
	if len(sink.events) != 1 || sink.events[0].ID != "evt" {
		t.Fatalf("sink events = %+v", sink.events)
	}

	// nil sink falls back to noop.
	b2 := NewExtensionRegistryBuilderWithEventSink[struct{}](nil)
	if b2.EventSink() == nil {
		t.Fatal("nil sink should fall back to noop")
	}
}
