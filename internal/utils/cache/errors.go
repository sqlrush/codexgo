package cache

import "errors"

// ErrZeroCapacity is returned by constructors when a capacity of zero is
// supplied. The Rust crate enforces this statically via NonZeroUsize; the Go
// port validates it at the boundary and reports this sentinel error.
var ErrZeroCapacity = errors.New("cache: capacity must be greater than zero")
