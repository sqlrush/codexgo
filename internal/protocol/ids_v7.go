package protocol

import (
	"fmt"

	"github.com/google/uuid"
)

// Identity generation mirrors upstream 0.147: `ThreadId::new()` and
// `SessionId::new()` mint UUIDv7 (`Uuid::now_v7()`), and Responses item ids are
// `<prefix>_<uuidv7>` (`ResponseItemId::new(prefix)`). Time-ordered ids let
// stores sort by identity and keep freshly created threads clustered.

// NewThreadIDV7 mints a fresh, time-ordered thread id. Mirrors Rust `ThreadId::new`.
func NewThreadIDV7() ThreadID { return ThreadID{uuid: mustV7()} }

// NewSessionIDV7 mints a fresh, time-ordered session id. Mirrors Rust `SessionId::new`.
func NewSessionIDV7() SessionID { return SessionID{uuid: mustV7()} }

// NewResponseItemID mints a prefixed Responses item id (`<prefix>_<uuidv7>`),
// mirroring Rust `ResponseItemId::new(prefix)`. Prefixes are the item family
// ("msg", "fc", "rs", ...); an empty prefix yields a bare UUIDv7 so callers that
// only need a unique id keep working.
func NewResponseItemID(prefix string) string {
	if prefix == "" {
		return mustV7()
	}
	return fmt.Sprintf("%s_%s", prefix, mustV7())
}

// IsPrefixedItemID reports whether id has the `<prefix>_<suffix>` shape with a
// non-empty prefix and suffix, mirroring Rust `ResponseItemId::is_prefixed`.
func IsPrefixedItemID(id string) bool {
	for i := 0; i < len(id); i++ {
		if id[i] == '_' {
			return i > 0 && i < len(id)-1
		}
	}
	return false
}

// mustV7 returns a UUIDv7 string; uuid.NewV7 only fails when the system clock
// or entropy source is unusable, which is unrecoverable for id minting.
func mustV7() string {
	id, err := uuid.NewV7()
	if err != nil {
		panic(fmt.Sprintf("protocol: uuid v7 generation failed: %v", err))
	}
	return id.String()
}
