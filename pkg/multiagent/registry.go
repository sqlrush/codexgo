package multiagent

import (
	_ "embed"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

//go:embed agent_names.txt
var agentNamesFile string

// NicknamePicker selects one candidate from a non-empty list of available
// nicknames. It is the testability seam for the Rust `rand::rng().choose(..)`
// call; production code uses [DefaultNicknamePicker].
type NicknamePicker func(candidates []string) string

// DefaultNicknamePicker chooses a uniformly random candidate, mirroring the Rust
// `IndexedRandom::choose`.
func DefaultNicknamePicker(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	return candidates[rand.IntN(len(candidates))]
}

// defaultAgentNicknameList returns the built-in nickname pool, mirroring the Rust
// `default_agent_nickname_list` that reads the embedded agent_names.txt.
func defaultAgentNicknameList() []string {
	var out []string
	for _, line := range strings.Split(agentNamesFile, "\n") {
		name := strings.TrimSpace(line)
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// Registry adds limits and bookkeeping to a session's multi-agent capabilities.
// It is a faithful port of the Rust `agent::registry::AgentRegistry`: it bounds
// the total number of spawned sub-agents per session, tracks per-agent metadata
// keyed by agent path, and reserves unique nicknames.
//
// A single Registry is shared by all agents in the same root session (because a
// single [Control] is). It is safe for concurrent use.
type Registry struct {
	picker NicknamePicker

	mu                 sync.Mutex
	agentTree          map[string]AgentMetadata
	usedNicknames      map[string]struct{}
	nicknameResetCount int
	totalCount         uint64
}

// NewRegistry constructs an empty [Registry]. When picker is nil the
// [DefaultNicknamePicker] is used.
func NewRegistry(picker NicknamePicker) *Registry {
	if picker == nil {
		picker = DefaultNicknamePicker
	}
	return &Registry{
		picker:        picker,
		agentTree:     make(map[string]AgentMetadata),
		usedNicknames: make(map[string]struct{}),
	}
}

// ReserveSpawnSlot reserves a slot for a new sub-agent, enforcing maxThreads when
// non-nil. It is a faithful port of the Rust `reserve_spawn_slot`: the returned
// [SpawnReservation] must be either committed via [SpawnReservation.Commit] or
// released via [SpawnReservation.Release]; releasing an uncommitted reservation
// frees the slot and any reserved nickname/path.
func (r *Registry) ReserveSpawnSlot(maxThreads *uint64) (*SpawnReservation, error) {
	if maxThreads != nil {
		if !r.tryIncrementSpawned(*maxThreads) {
			return nil, &AgentLimitError{MaxThreads: *maxThreads}
		}
	} else {
		r.mu.Lock()
		r.totalCount++
		r.mu.Unlock()
	}
	return &SpawnReservation{registry: r, active: true}, nil
}

// ReleaseSpawnedThread removes the agent with the given thread id from the tree
// and, when it was a counted (non-root) agent, decrements the spawn count. It is
// a faithful port of the Rust `release_spawned_thread`.
func (r *Registry) ReleaseSpawnedThread(threadID protocol.ThreadID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var removedKey string
	found := false
	for key, metadata := range r.agentTree {
		if metadata.AgentID != nil && *metadata.AgentID == threadID {
			removedKey = key
			found = true
			break
		}
	}
	if !found {
		return
	}
	metadata := r.agentTree[removedKey]
	delete(r.agentTree, removedKey)
	if !isRootPath(metadata.AgentPath) && r.totalCount > 0 {
		r.totalCount--
	}
}

// RegisterRootThread records the root thread under the canonical root agent path
// if it is not already present. It is a faithful port of the Rust
// `register_root_thread`.
func (r *Registry) RegisterRootThread(threadID protocol.ThreadID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.agentTree[protocol.AgentPathRoot]; ok {
		return
	}
	root := protocol.AgentPathRootValue()
	id := threadID
	r.agentTree[protocol.AgentPathRoot] = AgentMetadata{AgentID: &id, AgentPath: &root}
}

// AgentIDForPath returns the live thread id registered for an agent path, if any.
// It is a faithful port of the Rust `agent_id_for_path`.
func (r *Registry) AgentIDForPath(path protocol.AgentPath) *protocol.ThreadID {
	r.mu.Lock()
	defer r.mu.Unlock()
	metadata, ok := r.agentTree[path.String()]
	if !ok || metadata.AgentID == nil {
		return nil
	}
	id := *metadata.AgentID
	return &id
}

// AgentMetadataForThread returns a copy of the metadata for the given thread id,
// or nil when not tracked. It is a faithful port of the Rust
// `agent_metadata_for_thread`.
func (r *Registry) AgentMetadataForThread(threadID protocol.ThreadID) *AgentMetadata {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, metadata := range r.agentTree {
		if metadata.AgentID != nil && *metadata.AgentID == threadID {
			m := metadata.clone()
			return &m
		}
	}
	return nil
}

// LiveAgents returns copies of every tracked agent that has a live thread id and
// is not the root agent, mirroring the Rust `live_agents`.
func (r *Registry) LiveAgents() []AgentMetadata {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []AgentMetadata
	for _, metadata := range r.agentTree {
		if metadata.AgentID != nil && !isRootPath(metadata.AgentPath) {
			out = append(out, metadata.clone())
		}
	}
	return out
}

// UpdateLastTaskMessage sets the last task message preview for the agent with the
// given thread id, mirroring the Rust `update_last_task_message`.
func (r *Registry) UpdateLastTaskMessage(threadID protocol.ThreadID, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, metadata := range r.agentTree {
		if metadata.AgentID != nil && *metadata.AgentID == threadID {
			next := metadata.clone()
			msg := message
			next.LastTaskMessage = &msg
			r.agentTree[key] = next
			return
		}
	}
}

// registerSpawnedThread inserts committed metadata into the tree under its agent
// path (or a thread-id-derived key when no path), mirroring the Rust
// `register_spawned_thread`.
func (r *Registry) registerSpawnedThread(metadata AgentMetadata) {
	if metadata.AgentID == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := metadata.pathString()
	if key == "" {
		key = fmt.Sprintf("thread:%s", metadata.AgentID.String())
	}
	if metadata.AgentNickname != nil {
		r.usedNicknames[*metadata.AgentNickname] = struct{}{}
	}
	r.agentTree[key] = metadata.clone()
}

// reserveAgentNickname reserves a nickname, preferring preferred when non-empty,
// otherwise choosing an unused candidate (resetting the pool when exhausted). It
// is a faithful port of the Rust `reserve_agent_nickname`. It returns "" when no
// nickname could be chosen.
func (r *Registry) reserveAgentNickname(names []string, preferred string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var nickname string
	if preferred != "" {
		nickname = preferred
	} else {
		if len(names) == 0 {
			return ""
		}
		var available []string
		for _, name := range names {
			candidate := formatAgentNickname(name, r.nicknameResetCount)
			if _, used := r.usedNicknames[candidate]; !used {
				available = append(available, candidate)
			}
		}
		if len(available) > 0 {
			nickname = r.picker(available)
		} else {
			// Pool exhausted: reset and retry with a higher reset count so the new
			// candidates carry an ordinal suffix, mirroring the Rust reset branch.
			r.usedNicknames = make(map[string]struct{})
			r.nicknameResetCount++
			if len(names) == 0 {
				return ""
			}
			base := r.picker(names)
			if base == "" {
				return ""
			}
			nickname = formatAgentNickname(base, r.nicknameResetCount)
		}
	}
	r.usedNicknames[nickname] = struct{}{}
	return nickname
}

// reserveAgentPath inserts a placeholder metadata entry for an agent path,
// failing when the path is already taken. It is a faithful port of the Rust
// `reserve_agent_path`.
func (r *Registry) reserveAgentPath(path protocol.AgentPath) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.agentTree[path.String()]; ok {
		return unsupportedOperationf("agent path `%s` already exists", path.String())
	}
	p := path
	r.agentTree[path.String()] = AgentMetadata{AgentPath: &p}
	return nil
}

// releaseReservedAgentPath removes a path-only reservation (no live thread id),
// mirroring the Rust `release_reserved_agent_path`.
func (r *Registry) releaseReservedAgentPath(path protocol.AgentPath) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if metadata, ok := r.agentTree[path.String()]; ok && metadata.AgentID == nil {
		delete(r.agentTree, path.String())
	}
}

// tryIncrementSpawned atomically bumps the spawn count when below maxThreads,
// mirroring the Rust compare-exchange loop.
func (r *Registry) tryIncrementSpawned(maxThreads uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.totalCount >= maxThreads {
		return false
	}
	r.totalCount++
	return true
}

// decrementTotalCount frees a reserved slot on a dropped (uncommitted)
// reservation, mirroring the Rust `Drop for SpawnReservation`.
func (r *Registry) decrementTotalCount() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.totalCount > 0 {
		r.totalCount--
	}
}

// SpawnReservation is the RAII-style handle returned by
// [Registry.ReserveSpawnSlot]. It tracks the slot, nickname, and path reserved
// for an in-progress spawn so they can be released on failure. It is a faithful
// port of the Rust `agent::registry::SpawnReservation`.
//
// A reservation must be finalized exactly once with either [SpawnReservation.Commit]
// (success) or [SpawnReservation.Release] (failure). Release is idempotent.
type SpawnReservation struct {
	registry *Registry
	active   bool

	reservedNickname *string
	reservedPath     *protocol.AgentPath
}

// ReserveAgentNicknameWithPreference reserves a nickname for the spawn, honoring
// the preferred name when supplied. It is a faithful port of the Rust
// `reserve_agent_nickname_with_preference`.
func (s *SpawnReservation) ReserveAgentNicknameWithPreference(names []string, preferred string) (string, error) {
	nickname := s.registry.reserveAgentNickname(names, preferred)
	if nickname == "" {
		return "", unsupportedOperationf("no available agent nicknames")
	}
	s.reservedNickname = &nickname
	return nickname, nil
}

// ReserveAgentPath reserves an agent path for the spawn, failing on a collision.
// It is a faithful port of the Rust `SpawnReservation::reserve_agent_path`.
func (s *SpawnReservation) ReserveAgentPath(path protocol.AgentPath) error {
	if err := s.registry.reserveAgentPath(path); err != nil {
		return err
	}
	p := path
	s.reservedPath = &p
	return nil
}

// Commit finalizes the reservation by registering metadata for the spawned
// thread. It is a faithful port of the Rust `SpawnReservation::commit`.
func (s *SpawnReservation) Commit(metadata AgentMetadata) {
	if !s.active {
		return
	}
	s.reservedNickname = nil
	s.reservedPath = nil
	s.active = false
	s.registry.registerSpawnedThread(metadata)
}

// Release abandons an uncommitted reservation, freeing the spawn slot and any
// reserved path. It is a faithful port of the Rust `Drop for SpawnReservation`
// and is safe to call multiple times.
func (s *SpawnReservation) Release() {
	if !s.active {
		return
	}
	s.active = false
	if s.reservedPath != nil {
		s.registry.releaseReservedAgentPath(*s.reservedPath)
		s.reservedPath = nil
	}
	s.registry.decrementTotalCount()
}

// formatAgentNickname appends an ordinal suffix once the nickname pool has been
// reset, mirroring the Rust `format_agent_nickname`.
func formatAgentNickname(name string, nicknameResetCount int) string {
	if nicknameResetCount == 0 {
		return name
	}
	value := nicknameResetCount + 1
	suffix := ordinalSuffix(value)
	return fmt.Sprintf("%s the %d%s", name, value, suffix)
}

// ordinalSuffix returns the English ordinal suffix for value, mirroring the Rust
// suffix match (st/nd/rd/th with the 11-13 teens exception).
func ordinalSuffix(value int) string {
	switch value % 100 {
	case 11, 12, 13:
		return "th"
	}
	switch value % 10 {
	case 1:
		return "st"
	case 2:
		return "nd"
	case 3:
		return "rd"
	default:
		return "th"
	}
}

// isRootPath reports whether path is the canonical root agent path.
func isRootPath(path *protocol.AgentPath) bool {
	return path != nil && path.IsRoot()
}
