// Package keyring provides a system-keyring abstraction for storing,
// retrieving, and deleting secrets keyed by a service name and account.
//
// It is a faithful Go port of codex's keyring-store crate. The central
// abstraction is [Store], an interface modeled on the crate's KeyringStore
// trait. The production implementation backed by the operating system
// keyring (via github.com/zalando/go-keyring) lives in the subpackage
// keyring/system so that packages which only need the abstraction (mcp,
// secrets, login) stay free of the OS keyring dependency; [MemoryStore] is an
// in-memory implementation suitable for tests, mirroring the crate's
// MockKeyringStore, and [Unavailable] reports ErrNotAvailable for every
// operation (the store hosts without a system keyring inject).
//
// Error mapping follows the source crate: a missing entry is not an error.
// Load returns (nil, nil) and Delete returns (false, nil) when no secret
// exists. Any other backend failure is wrapped in a [StoreError]. When the
// platform has no usable keyring backend, operations report [ErrNotAvailable]
// (still wrapped in a [StoreError]) so callers can detect the not-available
// condition and fall back to an alternative credential source.
package keyring

import (
	"errors"
	"fmt"
)

// ErrNotAvailable signals that no usable system keyring backend is available
// on the current platform. Callers may test for it with errors.Is to decide
// whether to fall back to an alternative credential store. It is always
// returned wrapped inside a [StoreError].
var ErrNotAvailable = errors.New("keyring: no system keyring backend available")

// Store is the shared credential-store abstraction for keyring-backed
// implementations. It mirrors codex's KeyringStore trait.
//
// All methods identify a secret by (service, account). Implementations must be
// safe for concurrent use by multiple goroutines.
type Store interface {
	// Load returns the secret stored for (service, account). When no secret
	// exists it returns (nil, nil) rather than an error. Any backend failure
	// is returned as a non-nil error wrapping [StoreError].
	Load(service, account string) (*string, error)

	// Save stores value as the secret for (service, account), overwriting any
	// existing secret. A backend failure is wrapped in a [StoreError].
	Save(service, account, value string) error

	// Delete removes the secret for (service, account). It reports whether a
	// secret was actually removed: true if one existed and was deleted, false
	// if none existed. A backend failure is wrapped in a [StoreError].
	Delete(service, account string) (bool, error)
}

// StoreError wraps an underlying keyring backend error. It corresponds to the
// crate's CredentialStoreError and preserves the wrapped error for inspection
// via errors.Is / errors.As and the Unwrap method.
type StoreError struct {
	// Err is the underlying backend error that caused this failure.
	Err error
}

// NewStoreError wraps err in a *StoreError. It returns nil when err is nil so
// it can be used directly in return statements. Backend implementations in
// other packages (keyring/system) use it to report failures in the package's
// error model.
func NewStoreError(err error) error {
	if err == nil {
		return nil
	}
	return &StoreError{Err: err}
}

// newStoreError is the package-internal spelling of [NewStoreError].
func newStoreError(err error) error { return NewStoreError(err) }

// Error implements the error interface, rendering the underlying error message,
// matching the crate's Display implementation which forwards to the inner error.
func (e *StoreError) Error() string {
	if e == nil || e.Err == nil {
		return "keyring: unknown error"
	}
	return fmt.Sprintf("keyring: %v", e.Err)
}

// Message returns the underlying error's message. It mirrors the crate's
// CredentialStoreError::message helper.
func (e *StoreError) Message() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

// Unwrap exposes the wrapped backend error so errors.Is and errors.As can
// inspect it (for example to detect [ErrNotAvailable]).
func (e *StoreError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Unavailable is a [Store] with no backend: every operation fails with
// [ErrNotAvailable] (wrapped in a [StoreError]) and Load/Delete report no entry
// only after that failure is surfaced. Hosts without a system keyring (hosted
// runtimes, tests) inject it so callers take their documented fallback path
// (e.g. the MCP OAuth store's file fallback under the Auto mode).
type Unavailable struct{}

// Load implements [Store]; it always reports [ErrNotAvailable].
func (Unavailable) Load(string, string) (*string, error) {
	return nil, NewStoreError(ErrNotAvailable)
}

// Save implements [Store]; it always reports [ErrNotAvailable].
func (Unavailable) Save(string, string, string) error { return NewStoreError(ErrNotAvailable) }

// Delete implements [Store]; it always reports [ErrNotAvailable].
func (Unavailable) Delete(string, string) (bool, error) {
	return false, NewStoreError(ErrNotAvailable)
}
