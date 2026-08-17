//go:build !unix

package msghistory

import "os"

// ensureOwnerOnlyPermissions is a no-op on non-Unix platforms, mirroring the
// Windows arm of the Rust `ensure_owner_only_permissions`.
func ensureOwnerOnlyPermissions(*os.File) error { return nil }
