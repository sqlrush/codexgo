package keyring

import (
	"errors"

	gokeyring "github.com/zalando/go-keyring"
)

// backend is the minimal set of operations [DefaultStore] needs from a keyring
// implementation. The github.com/zalando/go-keyring package exposes only
// package-level functions backed by a global provider; depending on this small
// interface instead lets us drive DefaultStore against the real keyring in
// production while injecting a fake in tests, without mutating that global
// state.
//
// Accepting this interface (rather than the concrete package) keeps DefaultStore
// testable and follows the "accept interfaces, return structs" guideline.
type backend interface {
	// Get returns the secret for (service, user) or an error. A missing secret
	// is reported as gokeyring.ErrNotFound.
	Get(service, user string) (string, error)
	// Set stores password for (service, user), overwriting any existing value.
	Set(service, user, password string) error
	// Delete removes the secret for (service, user). A missing secret is
	// reported as gokeyring.ErrNotFound.
	Delete(service, user string) error
}

// systemBackend is the production backend that forwards to the package-level
// functions of github.com/zalando/go-keyring, which select an OS-appropriate
// provider (macOS Keychain, Windows Credential Manager, the Secret Service on
// Linux/BSD, and so on).
type systemBackend struct{}

// Get implements backend by delegating to gokeyring.Get.
func (systemBackend) Get(service, user string) (string, error) {
	return gokeyring.Get(service, user)
}

// Set implements backend by delegating to gokeyring.Set.
func (systemBackend) Set(service, user, password string) error {
	return gokeyring.Set(service, user, password)
}

// Delete implements backend by delegating to gokeyring.Delete.
func (systemBackend) Delete(service, user string) error {
	return gokeyring.Delete(service, user)
}

// isNotFound reports whether err indicates that no entry exists for the
// requested (service, account). This is the Go analogue of matching against
// keyring::Error::NoEntry in the source crate.
func isNotFound(err error) bool {
	return errors.Is(err, gokeyring.ErrNotFound)
}

// isNotAvailable reports whether err indicates that the platform has no usable
// keyring backend. The zalando library returns ErrUnsupportedPlatform from its
// fallback provider in that case.
func isNotAvailable(err error) bool {
	return errors.Is(err, gokeyring.ErrUnsupportedPlatform)
}

// normalize maps a raw backend error to the package's error model: it converts
// the "platform unsupported" signal into [ErrNotAvailable] (wrapped) and wraps
// any other error in a [StoreError]. Callers must have already handled the
// "not found" case, which is not an error in this package. normalize returns
// nil for a nil input.
func normalize(err error) error {
	if err == nil {
		return nil
	}
	if isNotAvailable(err) {
		return newStoreError(ErrNotAvailable)
	}
	return newStoreError(err)
}
