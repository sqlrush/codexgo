package core

// InstallActiveTurnForTesting installs a fresh [ActiveTurn] on the session so
// approval waiters (RequestCommandApproval / NotifyApproval and friends) have a
// home without a running turn, and returns it. Production turns install their
// own active turn (turn_spawn.go); this entry point exists only for tests of
// packages layered on core (core/localexec, host assemblies) that exercise an
// executor's approval path against a spawned session. It must not be called
// while a turn is running.
func (s *Session) InstallActiveTurnForTesting() *ActiveTurn {
	at := NewActiveTurn()
	s.setActiveTurn(at)
	return at
}
