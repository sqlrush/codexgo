package modelproviderinfo

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// Default values for command-backed provider auth, mirroring the Rust
// DEFAULT_PROVIDER_AUTH_* constants on codex_protocol::config_types.
const (
	defaultProviderAuthTimeoutMS         = uint64(5_000)
	defaultProviderAuthRefreshIntervalMS = uint64(300_000)
	defaultProviderAuthCwd               = protocol.AbsolutePath(".")
)

// ModelProviderAuthInfo configures obtaining a provider bearer token from a
// command.
//
// This mirrors codex_protocol::config_types::ModelProviderAuthInfo. (That type
// is not yet available in the Go internal/protocol package, so it is defined
// here against the existing protocol.AbsolutePath alias; it can move to
// internal/protocol once that crate is ported.)
//
// Rust serde: deny_unknown_fields; `args` defaults to empty; `timeout_ms` is a
// NonZeroU64 defaulting to 5000; `refresh_interval_ms` defaults to 300000 and
// may be 0 (which disables proactive refresh); `cwd` defaults to "." and is
// skipped on serialize when it equals that default.
type ModelProviderAuthInfo struct {
	// Command to execute. Bare names are resolved via PATH; paths are resolved
	// against Cwd.
	Command string
	// Args are the command arguments.
	Args []string
	// TimeoutMS is the maximum time (milliseconds) to wait for the token command
	// to exit successfully. Must be non-zero.
	TimeoutMS uint64
	// RefreshIntervalMS is the maximum age (milliseconds) for the cached token
	// before rerunning the command. Zero disables proactive refresh.
	RefreshIntervalMS uint64
	// Cwd is the working directory used when running the token command.
	Cwd protocol.AbsolutePath
}

// DefaultModelProviderAuthInfo returns an auth info populated with the serde
// defaults (empty command and args, the default timeout/refresh interval, and
// the "." default cwd).
func DefaultModelProviderAuthInfo() ModelProviderAuthInfo {
	return ModelProviderAuthInfo{
		Command:           "",
		Args:              nil,
		TimeoutMS:         defaultProviderAuthTimeoutMS,
		RefreshIntervalMS: defaultProviderAuthRefreshIntervalMS,
		Cwd:               defaultProviderAuthCwd,
	}
}

// Timeout returns the token command timeout as a Duration.
func (a ModelProviderAuthInfo) Timeout() time.Duration {
	return time.Duration(a.TimeoutMS) * time.Millisecond
}

// RefreshInterval returns the proactive refresh interval, or (0, false) when
// refresh is disabled (RefreshIntervalMS == 0), mirroring the Rust
// Option<Duration> return.
func (a ModelProviderAuthInfo) RefreshInterval() (time.Duration, bool) {
	if a.RefreshIntervalMS == 0 {
		return 0, false
	}
	return time.Duration(a.RefreshIntervalMS) * time.Millisecond, true
}

type authInfoJSON struct {
	Command           string                 `json:"command"`
	Args              []string               `json:"args"`
	TimeoutMS         uint64                 `json:"timeout_ms"`
	RefreshIntervalMS uint64                 `json:"refresh_interval_ms"`
	Cwd               *protocol.AbsolutePath `json:"cwd,omitempty"`
}

// MarshalJSON encodes the auth info, applying the serde rules: args is always
// present (empty array when nil), timeout_ms/refresh_interval_ms are always
// present, and cwd is omitted when it equals the default ".".
func (a ModelProviderAuthInfo) MarshalJSON() ([]byte, error) {
	args := a.Args
	if args == nil {
		args = []string{}
	}
	out := authInfoJSON{
		Command:           a.Command,
		Args:              args,
		TimeoutMS:         a.TimeoutMS,
		RefreshIntervalMS: a.RefreshIntervalMS,
	}
	if a.Cwd != defaultProviderAuthCwd {
		cwd := a.Cwd
		out.Cwd = &cwd
	}
	return json.Marshal(out)
}

// UnmarshalJSON decodes the auth info, applying serde defaults for any absent
// fields (args, timeout_ms, refresh_interval_ms, cwd) and rejecting a zero
// timeout_ms (the Rust field is NonZeroU64).
func (a *ModelProviderAuthInfo) UnmarshalJSON(data []byte) error {
	type rawAuthInfo struct {
		Command           string                 `json:"command"`
		Args              []string               `json:"args"`
		TimeoutMS         *uint64                `json:"timeout_ms"`
		RefreshIntervalMS *uint64                `json:"refresh_interval_ms"`
		Cwd               *protocol.AbsolutePath `json:"cwd"`
	}
	var raw rawAuthInfo
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode auth info: %w", err)
	}

	out := DefaultModelProviderAuthInfo()
	out.Command = raw.Command
	out.Args = raw.Args
	if raw.TimeoutMS != nil {
		if *raw.TimeoutMS == 0 {
			return fmt.Errorf("auth.timeout_ms must be non-zero")
		}
		out.TimeoutMS = *raw.TimeoutMS
	}
	if raw.RefreshIntervalMS != nil {
		out.RefreshIntervalMS = *raw.RefreshIntervalMS
	}
	if raw.Cwd != nil {
		out.Cwd = *raw.Cwd
	}
	*a = out
	return nil
}

// Equal reports whether two auth infos are identical, treating a nil and empty
// Args slice as equal (matching Rust PartialEq semantics where both are empty
// vectors).
func (a ModelProviderAuthInfo) Equal(other ModelProviderAuthInfo) bool {
	if a.Command != other.Command ||
		a.TimeoutMS != other.TimeoutMS ||
		a.RefreshIntervalMS != other.RefreshIntervalMS ||
		a.Cwd != other.Cwd {
		return false
	}
	if len(a.Args) != len(other.Args) {
		return false
	}
	for i := range a.Args {
		if a.Args[i] != other.Args[i] {
			return false
		}
	}
	return true
}

// ModelProviderAwsAuthInfo configures AWS SigV4 auth for a model provider.
//
// Rust serde: deny_unknown_fields; both fields are Option<String> without
// skip_serializing_if, so they always serialize (null when absent).
type ModelProviderAwsAuthInfo struct {
	// Profile is the AWS profile name to use. When nil, the AWS SDK default
	// chain decides.
	Profile *string `json:"profile"`
	// Region is the AWS region to use for provider-specific endpoints.
	Region *string `json:"region"`
}

// Equal reports whether two AWS auth infos are equal by value.
func (a ModelProviderAwsAuthInfo) Equal(other ModelProviderAwsAuthInfo) bool {
	return optStringEqual(a.Profile, other.Profile) &&
		optStringEqual(a.Region, other.Region)
}

func optStringEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
