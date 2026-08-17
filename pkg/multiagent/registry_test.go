package multiagent

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func tid(suffix int) protocol.ThreadID {
	return protocol.NewThreadID(fmt.Sprintf("00000000-0000-0000-0000-%012d", suffix))
}

func mustPath(t *testing.T, s string) protocol.AgentPath {
	t.Helper()
	p, err := protocol.NewAgentPath(s)
	if err != nil {
		t.Fatalf("NewAgentPath(%q): %v", s, err)
	}
	return p
}

// firstPicker is a deterministic NicknamePicker that always returns the first
// candidate, so nickname reservation is reproducible in tests.
func firstPicker(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func TestReserveSpawnSlotEnforcesLimit(t *testing.T) {
	r := NewRegistry(firstPicker)
	max := uint64(2)

	res1, err := r.ReserveSpawnSlot(&max)
	if err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	res2, err := r.ReserveSpawnSlot(&max)
	if err != nil {
		t.Fatalf("second reserve: %v", err)
	}

	_, err = r.ReserveSpawnSlot(&max)
	var limitErr *AgentLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("third reserve error = %v, want AgentLimitError", err)
	}
	if limitErr.MaxThreads != 2 {
		t.Fatalf("limit max = %d, want 2", limitErr.MaxThreads)
	}

	// Releasing one slot frees capacity for another reservation.
	res1.Release()
	res3, err := r.ReserveSpawnSlot(&max)
	if err != nil {
		t.Fatalf("reserve after release: %v", err)
	}

	res2.Release()
	res3.Release()
}

func TestReserveSpawnSlotUnboundedWhenNil(t *testing.T) {
	r := NewRegistry(firstPicker)
	for i := 0; i < 100; i++ {
		res, err := r.ReserveSpawnSlot(nil)
		if err != nil {
			t.Fatalf("reserve %d: %v", i, err)
		}
		res.Release()
	}
}

func TestReserveAgentPathRejectsDuplicate(t *testing.T) {
	r := NewRegistry(firstPicker)
	res, err := r.ReserveSpawnSlot(nil)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	path := mustPath(t, "/root/researcher")
	if err := res.ReserveAgentPath(path); err != nil {
		t.Fatalf("first reserve path: %v", err)
	}

	res2, _ := r.ReserveSpawnSlot(nil)
	err = res2.ReserveAgentPath(path)
	if !errors.Is(err, ErrUnsupportedOperation) {
		t.Fatalf("duplicate path error = %v, want ErrUnsupportedOperation", err)
	}
	res.Release()
	res2.Release()

	// After releasing the (uncommitted) reservation, the path is free again.
	res3, _ := r.ReserveSpawnSlot(nil)
	if err := res3.ReserveAgentPath(path); err != nil {
		t.Fatalf("reserve path after release: %v", err)
	}
	res3.Release()
}

func TestReserveAgentNicknamePrefersPreferred(t *testing.T) {
	r := NewRegistry(firstPicker)
	res, _ := r.ReserveSpawnSlot(nil)
	got, err := res.ReserveAgentNicknameWithPreference([]string{"Euclid", "Newton"}, "Custom")
	if err != nil {
		t.Fatalf("reserve nickname: %v", err)
	}
	if got != "Custom" {
		t.Fatalf("nickname = %q, want Custom", got)
	}
	res.Release()
}

func TestReserveAgentNicknameUniqueAndResets(t *testing.T) {
	r := NewRegistry(firstPicker)
	names := []string{"Euclid", "Newton"}

	// firstPicker always returns the first available candidate.
	res1, _ := r.ReserveSpawnSlot(nil)
	n1, _ := res1.ReserveAgentNicknameWithPreference(names, "")
	if n1 != "Euclid" {
		t.Fatalf("n1 = %q, want Euclid", n1)
	}
	res2, _ := r.ReserveSpawnSlot(nil)
	n2, _ := res2.ReserveAgentNicknameWithPreference(names, "")
	if n2 != "Newton" {
		t.Fatalf("n2 = %q, want Newton", n2)
	}

	// Pool exhausted: the next reservation resets and applies an ordinal suffix.
	res3, _ := r.ReserveSpawnSlot(nil)
	n3, err := res3.ReserveAgentNicknameWithPreference(names, "")
	if err != nil {
		t.Fatalf("reserve after exhaustion: %v", err)
	}
	if n3 != "Euclid the 2nd" {
		t.Fatalf("n3 = %q, want \"Euclid the 2nd\"", n3)
	}
	res1.Release()
	res2.Release()
	res3.Release()
}

func TestReserveAgentNicknameEmptyPoolFails(t *testing.T) {
	r := NewRegistry(firstPicker)
	res, _ := r.ReserveSpawnSlot(nil)
	_, err := res.ReserveAgentNicknameWithPreference(nil, "")
	if !errors.Is(err, ErrUnsupportedOperation) {
		t.Fatalf("empty pool error = %v, want ErrUnsupportedOperation", err)
	}
	res.Release()
}

func TestCommitRegistersAndReleaseTracksCount(t *testing.T) {
	r := NewRegistry(firstPicker)
	max := uint64(1)

	res, err := r.ReserveSpawnSlot(&max)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	path := mustPath(t, "/root/worker")
	if err := res.ReserveAgentPath(path); err != nil {
		t.Fatalf("reserve path: %v", err)
	}
	id := tid(1)
	res.Commit(AgentMetadata{AgentID: &id, AgentPath: &path, AgentNickname: strptr("Euclid")})

	if got := r.AgentIDForPath(path); got == nil || *got != id {
		t.Fatalf("AgentIDForPath = %v, want %v", got, id)
	}
	if md := r.AgentMetadataForThread(id); md == nil || md.AgentNickname == nil || *md.AgentNickname != "Euclid" {
		t.Fatalf("AgentMetadataForThread = %v, want nickname Euclid", md)
	}

	// The committed agent occupies the only slot.
	if _, err := r.ReserveSpawnSlot(&max); err == nil {
		t.Fatalf("expected limit error while committed agent holds the slot")
	}

	// Releasing the spawned thread frees the slot again.
	r.ReleaseSpawnedThread(id)
	if md := r.AgentMetadataForThread(id); md != nil {
		t.Fatalf("metadata should be gone after release, got %v", md)
	}
	res2, err := r.ReserveSpawnSlot(&max)
	if err != nil {
		t.Fatalf("reserve after release: %v", err)
	}
	res2.Release()
}

func TestRegisterRootThreadIsIdempotentAndExcludedFromLive(t *testing.T) {
	r := NewRegistry(firstPicker)
	rootID := tid(1)
	r.RegisterRootThread(rootID)
	r.RegisterRootThread(tid(2)) // second registration is ignored

	root := protocol.AgentPathRootValue()
	if got := r.AgentIDForPath(root); got == nil || *got != rootID {
		t.Fatalf("root id = %v, want %v", got, rootID)
	}
	if live := r.LiveAgents(); len(live) != 0 {
		t.Fatalf("root should be excluded from LiveAgents, got %d", len(live))
	}

	// Releasing the root does not decrement the spawn count (it was never counted).
	r.ReleaseSpawnedThread(rootID)
}

func TestUpdateLastTaskMessage(t *testing.T) {
	r := NewRegistry(firstPicker)
	res, _ := r.ReserveSpawnSlot(nil)
	id := tid(5)
	path := mustPath(t, "/root/talker")
	res.Commit(AgentMetadata{AgentID: &id, AgentPath: &path})

	r.UpdateLastTaskMessage(id, "hello world")
	md := r.AgentMetadataForThread(id)
	if md == nil || md.LastTaskMessage == nil || *md.LastTaskMessage != "hello world" {
		t.Fatalf("last task message = %v, want \"hello world\"", md)
	}

	// Update on an unknown thread is a no-op (does not panic).
	r.UpdateLastTaskMessage(tid(99), "ignored")
}

func TestMetadataForThreadReturnsCopy(t *testing.T) {
	r := NewRegistry(firstPicker)
	res, _ := r.ReserveSpawnSlot(nil)
	id := tid(7)
	path := mustPath(t, "/root/copy")
	res.Commit(AgentMetadata{AgentID: &id, AgentPath: &path, AgentNickname: strptr("Newton")})

	md := r.AgentMetadataForThread(id)
	*md.AgentNickname = "mutated"
	again := r.AgentMetadataForThread(id)
	if again.AgentNickname == nil || *again.AgentNickname != "Newton" {
		t.Fatalf("registry copy was mutated: %v", again.AgentNickname)
	}
}

func TestRegistryConcurrentReservations(t *testing.T) {
	r := NewRegistry(firstPicker)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			res, err := r.ReserveSpawnSlot(nil)
			if err != nil {
				t.Errorf("reserve: %v", err)
				return
			}
			id := tid(1000 + n)
			res.Commit(AgentMetadata{AgentID: &id})
			r.UpdateLastTaskMessage(id, "x")
			_ = r.AgentMetadataForThread(id)
			r.ReleaseSpawnedThread(id)
		}(i)
	}
	wg.Wait()
}
