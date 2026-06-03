package extensionapi

import "errors"

// ErrInjectionUnavailable signals that a [ResponseItemInjector] could not inject
// items because no turn can currently accept same-turn model input. Callers
// recover the unchanged items from the injector's first return value, mirroring
// the Rust `Err(Vec<ResponseInputItem>)` payload.
var ErrInjectionUnavailable = errors.New("response item injection unavailable")
