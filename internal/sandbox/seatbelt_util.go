package sandbox

import (
	"errors"
	"path"
	"strconv"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// errInvalidPort is returned when a *_PROXY_URL port is not a valid u16.
var errInvalidPort = errors.New("sandbox: invalid proxy port")

// dirParam is a Seatbelt policy parameter binding (a -D<key>=<path> arg). The
// generated policy text references the key via (param "<key>"); the value is the
// concrete path passed to sandbox-exec. Mirrors the (String, PathBuf) pairs in
// seatbelt.rs.
type dirParam struct {
	key  string
	path string
}

// itoa formats an int in base 10.
func itoa(v int) string {
	return strconv.Itoa(v)
}

// uitoa formats a uint64 in base 10.
func uitoa(v uint64) string {
	return strconv.FormatUint(v, 10)
}

// absolutePathsToStrings converts a slice of protocol.AbsolutePath to plain
// strings, returning nil for an empty input.
func absolutePathsToStrings(paths []protocol.AbsolutePath) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, string(p))
	}
	return out
}

// containsString reports whether values holds target.
func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

// joinPath joins a root and a single component using forward-slash semantics,
// matching the Path::join used in seatbelt.rs.
func joinPath(root, name string) string {
	return path.Join(root, name)
}
