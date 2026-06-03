package keyring

import "sync"

// MemoryStore is an in-memory [Store] implementation intended for tests. It is
// the Go analogue of the crate's MockKeyringStore.
//
// Like the source mock, MemoryStore keys credentials by account only and
// ignores the service argument, so it is unsuitable for production use. It is
// safe for concurrent use by multiple goroutines.
//
// A per-account error may be injected with [MemoryStore.SetError]; once set,
// operations on that account return the configured error (wrapped in a
// [StoreError]) instead of touching stored values.
type MemoryStore struct {
	mu     sync.Mutex
	values map[string]string
	errs   map[string]error
}

// NewMemoryStore returns an empty [MemoryStore] ready for use.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		values: make(map[string]string),
		errs:   make(map[string]error),
	}
}

// ensure lazily initializes the internal maps so the zero value of MemoryStore
// is usable. Callers must hold s.mu.
func (s *MemoryStore) ensure() {
	if s.values == nil {
		s.values = make(map[string]string)
	}
	if s.errs == nil {
		s.errs = make(map[string]error)
	}
}

// SetError configures account so that subsequent operations return err (wrapped
// in a [StoreError]). Passing a nil err clears any previously configured error.
// This mirrors MockKeyringStore::set_error.
func (s *MemoryStore) SetError(account string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensure()
	if err == nil {
		delete(s.errs, account)
		return
	}
	s.errs[account] = err
}

// Contains reports whether a secret is currently stored for account. It mirrors
// MockKeyringStore::contains.
func (s *MemoryStore) Contains(account string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.values[account]
	return ok
}

// SavedValue returns the stored secret for account, if any. It mirrors
// MockKeyringStore::saved_value and ignores any injected error.
func (s *MemoryStore) SavedValue(account string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[account]
	return v, ok
}

// Load implements [Store]. It returns the stored secret, (nil, nil) when none
// exists, or the injected error for the account wrapped in a [StoreError].
func (s *MemoryStore) Load(_ /*service*/, account string) (*string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.errs[account]; err != nil {
		return nil, newStoreError(err)
	}
	v, ok := s.values[account]
	if !ok {
		return nil, nil
	}
	value := v
	return &value, nil
}

// Save implements [Store]. It stores value for account, overwriting any existing
// value, unless an error is injected for the account.
func (s *MemoryStore) Save(_ /*service*/, account, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensure()
	if err := s.errs[account]; err != nil {
		return newStoreError(err)
	}
	s.values[account] = value
	return nil
}

// Delete implements [Store]. It reports whether a secret was removed: true if
// one existed and was deleted, false otherwise. An injected error for the
// account is returned wrapped in a [StoreError].
func (s *MemoryStore) Delete(_ /*service*/, account string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.errs[account]; err != nil {
		return false, newStoreError(err)
	}
	if _, ok := s.values[account]; !ok {
		return false, nil
	}
	delete(s.values, account)
	return true, nil
}
