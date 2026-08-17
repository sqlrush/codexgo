package multiagent

import (
	"strings"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// resolveAgentPath resolves a reference (absolute or relative) against a base
// agent path, mirroring the Rust `AgentPath::resolve`. The Go
// [protocol.AgentPath] does not expose this method, so the logic is ported here
// without modifying the protocol package.
//
// Rules (mirroring the Rust impl):
//   - an empty reference is an error;
//   - the literal "/root" resolves to the root path;
//   - a reference starting with "/" is parsed as an absolute path;
//   - otherwise the reference is validated as a relative reference and joined
//     onto base segment-by-segment.
func resolveAgentPath(base protocol.AgentPath, reference string) (protocol.AgentPath, error) {
	if reference == "" {
		return protocol.AgentPath{}, unsupportedOperationf("agent path must not be empty")
	}
	if reference == protocol.AgentPathRoot {
		return protocol.AgentPathRootValue(), nil
	}
	if strings.HasPrefix(reference, "/") {
		path, err := protocol.NewAgentPath(reference)
		if err != nil {
			return protocol.AgentPath{}, unsupportedOperationf("%s", err.Error())
		}
		return path, nil
	}
	if err := validateRelativeReference(reference); err != nil {
		return protocol.AgentPath{}, err
	}
	resolved := base
	for _, segment := range strings.Split(reference, "/") {
		next, err := resolved.Join(segment)
		if err != nil {
			return protocol.AgentPath{}, unsupportedOperationf("%s", err.Error())
		}
		resolved = next
	}
	return resolved, nil
}

// validateRelativeReference mirrors the Rust `validate_relative_reference`: a
// relative reference must not end with "/" and each segment must be a valid
// agent name (validated by [protocol.AgentPath.Join] during resolution).
func validateRelativeReference(reference string) error {
	if strings.HasSuffix(reference, "/") {
		return unsupportedOperationf("relative agent path must not end with `/`")
	}
	return nil
}

// agentMatchesPrefix reports whether an agent path is at or under prefix,
// mirroring the Rust `agent_matches_prefix`. A root prefix matches everything.
func agentMatchesPrefix(agentPath *protocol.AgentPath, prefix protocol.AgentPath) bool {
	if prefix.IsRoot() {
		return true
	}
	if agentPath == nil {
		return false
	}
	if agentPath.String() == prefix.String() {
		return true
	}
	suffix, ok := strings.CutPrefix(agentPath.String(), prefix.String())
	return ok && strings.HasPrefix(suffix, "/")
}
