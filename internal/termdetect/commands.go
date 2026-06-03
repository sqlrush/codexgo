package termdetect

import (
	"os/exec"
	"strings"
)

// queryTmuxClientInfo collects tmux client terminal identification via
// `tmux display-message`. Missing or broken tmux binaries yield empty fields
// rather than errors, so terminal detection degrades gracefully.
func queryTmuxClientInfo() tmuxClientInfo {
	return tmuxClientInfo{
		termtype: tmuxDisplayMessage("#{client_termtype}"),
		termname: tmuxDisplayMessage("#{client_termname}"),
	}
}

// tmuxDisplayMessage runs `tmux display-message -p <format>` and returns the
// trimmed, non-whitespace output, or "" when the command fails or produces no
// meaningful value.
func tmuxDisplayMessage(format string) string {
	out, err := exec.Command("tmux", "display-message", "-p", format).Output()
	if err != nil {
		return ""
	}
	return noneIfWhitespace(strings.TrimSpace(string(out)))
}

// zellijVersionFromCommand is a best-effort fallback that parses
// `zellij --version`. Missing or broken zellij binaries should not affect
// terminal detection, so failures return "".
func zellijVersionFromCommand() string {
	out, err := exec.Command("zellij", "--version").Output()
	if err != nil {
		return ""
	}
	return parseZellijVersion(strings.TrimSpace(string(out)))
}

// parseZellijVersion extracts a version string from `zellij --version` output.
//
// Output of the form "zellij 0.44.1" yields "0.44.1"; a bare "0.44.1" is
// returned unchanged. An all-whitespace value yields "".
func parseZellijVersion(value string) string {
	value = noneIfWhitespace(value)
	if value == "" {
		return ""
	}
	parts := strings.Fields(value)
	if len(parts) >= 2 && strings.EqualFold(parts[0], "zellij") {
		return parts[1]
	}
	return value
}
