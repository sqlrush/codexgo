package cliutil

import "fmt"

// ProfileV2Name is a validated plain profile-v2 name used to select the
// $CODEXGO_HOME/<name>.config.toml layer.
//
// It mirrors codex_protocol::config_types::ProfileV2Name. A valid name is
// non-empty and contains only ASCII alphanumerics, '_', or '-'. The value is
// stored unexported so the only way to obtain a ProfileV2Name is through
// [ParseProfileV2Name], guaranteeing the invariant holds for any value of this
// type.
type ProfileV2Name struct {
	value string
}

// ParseProfileV2Name validates a raw --profile value and returns the
// corresponding [ProfileV2Name]. It mirrors the upstream FromStr implementation,
// returning an error whose message matches the upstream ProfileV2NameParseError
// Display output for any invalid input.
func ParseProfileV2Name(value string) (ProfileV2Name, error) {
	if value == "" || !isValidProfileName(value) {
		return ProfileV2Name{}, fmt.Errorf(
			"invalid --profile value `%s`; pass a plain name such as `work`",
			value,
		)
	}
	return ProfileV2Name{value: value}, nil
}

// String returns the underlying name, mirroring the upstream Display
// implementation. The zero ProfileV2Name renders as the empty string.
func (p ProfileV2Name) String() string {
	return p.value
}

// AsStr returns the underlying name, mirroring the upstream as_str accessor.
func (p ProfileV2Name) AsStr() string {
	return p.value
}

// IsZero reports whether the value is the zero ProfileV2Name (no name set).
func (p ProfileV2Name) IsZero() bool {
	return p.value == ""
}

// isValidProfileName reports whether every byte of value is an ASCII
// alphanumeric, '_', or '-'.
func isValidProfileName(value string) bool {
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '_' || c == '-':
		default:
			return false
		}
	}
	return true
}
