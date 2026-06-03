package termdetect

import (
	"os"
	"strings"
)

// tmuxClientInfo holds tmux client terminal identification captured via
// `tmux display-message`.
//
// termtype corresponds to `#{client_termtype}` and typically reflects the
// underlying terminal program (for example, "ghostty" or "wezterm") with an
// optional version suffix. termname comes from `#{client_termname}` and
// preserves the TERM capability string exposed by the client (for example,
// "xterm-256color").
//
// This information is only available when running under tmux and lets us
// attribute the session to the underlying terminal rather than to tmux itself.
// Absent values are represented by the empty string.
type tmuxClientInfo struct {
	termtype string
	termname string
}

// environment abstracts environment-variable and external-command access used
// by terminal detection. It exists to allow faking the environment in tests.
//
// The interface is deliberately small (accept interfaces): callers only need
// variable lookup plus the two side-effecting probes (tmux client info and the
// zellij version fallback).
type environment interface {
	// lookup returns an environment variable and whether it was set. A set
	// variable with an empty value reports (",", true).
	lookup(name string) (string, bool)
	// tmuxClientInfo returns tmux client details when available.
	tmuxClientInfo() tmuxClientInfo
	// zellijVersion returns Zellij version details when available, or "".
	zellijVersion() string
}

// has reports whether an environment variable is set (even if empty).
func has(env environment, name string) bool {
	_, ok := env.lookup(name)
	return ok
}

// varValue returns the value of an environment variable, or "" when unset.
//
// This mirrors Rust's Option<String>::var when the caller only needs the value
// and treats unset as empty.
func varValue(env environment, name string) string {
	v, _ := env.lookup(name)
	return v
}

// varNonEmpty returns a non-whitespace environment variable value, or "" when
// the variable is unset or contains only whitespace.
func varNonEmpty(env environment, name string) string {
	v, ok := env.lookup(name)
	if !ok {
		return ""
	}
	return noneIfWhitespace(v)
}

// hasNonEmpty reports whether an environment variable is set and non-empty.
func hasNonEmpty(env environment, name string) bool {
	return varNonEmpty(env, name) != ""
}

// processEnvironment reads environment variables from the running process and
// shells out for tmux/zellij probes.
type processEnvironment struct{}

// lookup implements environment using os.LookupEnv.
func (processEnvironment) lookup(name string) (string, bool) {
	return os.LookupEnv(name)
}

// tmuxClientInfo implements environment by querying tmux.
func (processEnvironment) tmuxClientInfo() tmuxClientInfo {
	return queryTmuxClientInfo()
}

// zellijVersion implements environment by reading ZELLIJ_VERSION first and
// falling back to the `zellij --version` command.
func (p processEnvironment) zellijVersion() string {
	if v := varNonEmpty(p, "ZELLIJ_VERSION"); v != "" {
		return v
	}
	return zellijVersionFromCommand()
}

// noneIfWhitespace returns value unchanged when it contains a non-whitespace
// character, or "" otherwise. This mirrors the Rust crate's none_if_whitespace,
// which preserves the original (possibly internally padded) string while
// rejecting all-whitespace values.
func noneIfWhitespace(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return value
}
