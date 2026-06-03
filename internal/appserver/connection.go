package appserver

import (
	"sync"
)

// Conn holds the per-connection negotiated state established by the initialize
// handshake. It is a reduced port of the Rust
// ConnectionSessionState/InitializedConnectionSessionState: enough to enforce
// the "must initialize first" contract and to carry the client identity into
// spawns.
//
// One Conn is created per logical client connection. It is safe for concurrent
// use. External drivers (the in-process client) create a Conn with [NewConn] and
// pass it to [Processor.HandleRequest].
type Conn struct {
	mu          sync.Mutex
	initialized bool

	experimentalAPI bool
	clientName      string
	clientVersion   string
}

// NewConn returns a fresh, uninitialized connection state.
func NewConn() *Conn { return &Conn{} }

// newConnectionState is the internal alias retained for transport call sites.
func newConnectionState() *Conn { return NewConn() }

// markInitialized records the negotiated capabilities. It returns false if the
// connection was already initialized, mirroring the Rust OnceLock.set contract.
func (s *Conn) markInitialized(name, version string, experimentalAPI bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initialized {
		return false
	}
	s.initialized = true
	s.clientName = name
	s.clientVersion = version
	s.experimentalAPI = experimentalAPI
	return true
}

// isInitialized reports whether the initialize handshake has completed.
func (s *Conn) isInitialized() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.initialized
}

// clientInfo returns the negotiated client name and version (nil before
// initialize).
func (s *Conn) clientInfo() (name *string, version *string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.initialized {
		return nil, nil
	}
	n := s.clientName
	v := s.clientVersion
	return &n, &v
}
