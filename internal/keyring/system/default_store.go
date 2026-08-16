// Package system holds the production [keyring.Store] backed by the operating
// system keyring (github.com/zalando/go-keyring: macOS Keychain, Windows
// Credential Manager, Secret Service on Linux/BSD). It is split from package
// keyring so that consumers of the abstraction do not link the OS keyring
// dependency; hosts that want the real keyring construct [NewDefaultStore] and
// inject it (CLI mcp/secrets/login wiring).
package system

import "github.com/sqlrush/codexgo/internal/keyring"

// DefaultStore is the production [keyring.Store] implementation backed by the
// operating system keyring. It corresponds to the crate's DefaultKeyringStore.
//
// The zero value is ready to use and talks to the real system keyring. A
// DefaultStore is safe for concurrent use; it holds no mutable state and the
// underlying backend serializes access to the OS keyring as needed.
type DefaultStore struct {
	// backend performs the actual keyring operations. When nil, the system
	// backend is used, so the zero value works out of the box.
	backend backend
}

// NewDefaultStore returns a [DefaultStore] backed by the system keyring.
func NewDefaultStore() *DefaultStore {
	return &DefaultStore{backend: systemBackend{}}
}

var _ keyring.Store = (*DefaultStore)(nil)

// store returns the configured backend, defaulting to the system backend so the
// zero value of DefaultStore is usable.
func (s *DefaultStore) store() backend {
	if s.backend == nil {
		return systemBackend{}
	}
	return s.backend
}

// Load implements [Store]. It returns the stored secret, (nil, nil) when no
// entry exists, or an error wrapping [StoreError] on any other failure.
func (s *DefaultStore) Load(service, account string) (*string, error) {
	password, err := s.store().Get(service, account)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, normalize(err)
	}
	return &password, nil
}

// Save implements [Store]. It stores value as the secret for (service, account),
// overwriting any existing value, and wraps any failure in a [StoreError].
func (s *DefaultStore) Save(service, account, value string) error {
	if err := s.store().Set(service, account, value); err != nil {
		return normalize(err)
	}
	return nil
}

// Delete implements [Store]. It reports whether a secret was removed: true if
// one existed and was deleted, false if none existed. Other failures are wrapped
// in a [StoreError].
func (s *DefaultStore) Delete(service, account string) (bool, error) {
	if err := s.store().Delete(service, account); err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, normalize(err)
	}
	return true, nil
}
