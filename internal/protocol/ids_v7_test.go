package protocol

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewThreadIDV7IsTimeOrderedV7(t *testing.T) {
	first := NewThreadIDV7()
	time.Sleep(2 * time.Millisecond)
	second := NewThreadIDV7()
	for _, id := range []ThreadID{first, second} {
		parsed, err := uuid.Parse(id.String())
		if err != nil {
			t.Fatalf("thread id %q is not a UUID: %v", id, err)
		}
		if parsed.Version() != 7 {
			t.Fatalf("thread id %q version = %d, want 7", id, parsed.Version())
		}
	}
	if !(first.String() < second.String()) {
		t.Fatalf("v7 ids should sort by creation time: %s !< %s", first, second)
	}
	if NewSessionIDV7().String() == "" {
		t.Fatal("session id empty")
	}
}

func TestNewResponseItemID(t *testing.T) {
	id := NewResponseItemID("msg")
	if !IsPrefixedItemID(id) || id[:4] != "msg_" {
		t.Fatalf("item id %q should be msg_<uuid>", id)
	}
	if _, err := uuid.Parse(id[4:]); err != nil {
		t.Fatalf("suffix of %q is not a UUID: %v", id, err)
	}
	if bare := NewResponseItemID(""); IsPrefixedItemID(bare) {
		t.Fatalf("empty prefix should yield a bare uuid, got %q", bare)
	}
	for in, want := range map[string]bool{"a_b": true, "_b": false, "a_": false, "ab": false, "": false} {
		if got := IsPrefixedItemID(in); got != want {
			t.Errorf("IsPrefixedItemID(%q) = %v, want %v", in, got, want)
		}
	}
}
