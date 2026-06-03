// Package protocol contains the shared wire-format types for the Codex
// protocol. These types are a faithful, drop-in-compatible Go reimplementation
// of the Rust `codex-protocol` crate; their JSON encoding must match the Rust
// serde output byte-for-byte (after key-order canonicalization).
package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SessionID identifies a single Codex session. On the wire it is a UUID string.
//
// Mirrors Rust `SessionId`, which serializes/deserializes as a bare string.
type SessionID struct {
	uuid string
}

// NewSessionID constructs a SessionID from a UUID string. The caller is
// responsible for supplying a valid UUID; no validation is performed beyond
// rejecting the empty string during JSON decoding.
func NewSessionID(uuid string) SessionID {
	return SessionID{uuid: uuid}
}

// String returns the UUID string representation.
func (s SessionID) String() string { return s.uuid }

// MarshalJSON encodes the SessionID as a JSON string.
func (s SessionID) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.uuid)
}

// UnmarshalJSON decodes a SessionID from a JSON string.
func (s *SessionID) UnmarshalJSON(data []byte) error {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*s = SessionID{uuid: v}
	return nil
}

// ToThreadID converts a SessionID into the equivalent ThreadID. They share the
// same underlying UUID, mirroring the Rust `From<SessionId> for ThreadId`.
func (s SessionID) ToThreadID() ThreadID {
	return ThreadID{uuid: s.uuid}
}

// ThreadID identifies a Codex thread. On the wire it is a UUID string.
//
// Mirrors Rust `ThreadId`, which serializes/deserializes as a bare string.
type ThreadID struct {
	uuid string
}

// NewThreadID constructs a ThreadID from a UUID string.
func NewThreadID(uuid string) ThreadID {
	return ThreadID{uuid: uuid}
}

// String returns the UUID string representation.
func (t ThreadID) String() string { return t.uuid }

// MarshalJSON encodes the ThreadID as a JSON string.
func (t ThreadID) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.uuid)
}

// UnmarshalJSON decodes a ThreadID from a JSON string.
func (t *ThreadID) UnmarshalJSON(data []byte) error {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*t = ThreadID{uuid: v}
	return nil
}

// ToSessionID converts a ThreadID into the equivalent SessionID. They share the
// same underlying UUID, mirroring the Rust `From<ThreadId> for SessionId`.
func (t ThreadID) ToSessionID() SessionID {
	return SessionID{uuid: t.uuid}
}

// Reserved AgentPath constants, mirroring the Rust associated constants.
const (
	AgentPathRoot     = "/root"
	AgentPathMorpheus = "/morpheus"
	agentPathRootSeg  = "root"
)

// AgentPath is a validated, absolute agent path such as "/root" or
// "/root/researcher". On the wire it is a bare string (Rust uses
// `#[serde(try_from = "String", into = "String")]`).
//
// Mirrors Rust `AgentPath(String)`.
type AgentPath struct {
	path string
}

// AgentPathRootValue returns the canonical root agent path.
func AgentPathRootValue() AgentPath { return AgentPath{path: AgentPathRoot} }

// AgentPathMorpheusValue returns the canonical morpheus agent path.
func AgentPathMorpheusValue() AgentPath { return AgentPath{path: AgentPathMorpheus} }

// NewAgentPath validates and constructs an AgentPath from an absolute path
// string. It returns an error when the path is not a valid absolute agent path.
func NewAgentPath(path string) (AgentPath, error) {
	if err := validateAbsoluteAgentPath(path); err != nil {
		return AgentPath{}, err
	}
	return AgentPath{path: path}, nil
}

// String returns the underlying path string.
func (a AgentPath) String() string { return a.path }

// IsRoot reports whether this path is the canonical root path.
func (a AgentPath) IsRoot() bool { return a.path == AgentPathRoot }

// Name returns the final path segment, or "root" for the root path.
func (a AgentPath) Name() string {
	if a.IsRoot() {
		return agentPathRootSeg
	}
	idx := strings.LastIndex(a.path, "/")
	if idx >= 0 {
		seg := a.path[idx+1:]
		if seg != "" {
			return seg
		}
	}
	return agentPathRootSeg
}

// Join validates agentName and returns a new child AgentPath.
func (a AgentPath) Join(agentName string) (AgentPath, error) {
	if err := validateAgentName(agentName); err != nil {
		return AgentPath{}, err
	}
	return NewAgentPath(a.path + "/" + agentName)
}

// MarshalJSON encodes the AgentPath as a JSON string.
func (a AgentPath) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.path)
}

// UnmarshalJSON decodes and validates an AgentPath from a JSON string.
func (a *AgentPath) UnmarshalJSON(data []byte) error {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	p, err := NewAgentPath(v)
	if err != nil {
		return err
	}
	*a = p
	return nil
}

func validateAgentName(agentName string) error {
	switch {
	case agentName == "":
		return fmt.Errorf("agent_name must not be empty")
	case agentName == agentPathRootSeg:
		return fmt.Errorf("agent_name `root` is reserved")
	case agentName == "." || agentName == "..":
		return fmt.Errorf("agent_name `%s` is reserved", agentName)
	case strings.Contains(agentName, "/"):
		return fmt.Errorf("agent_name must not contain `/`")
	}
	for _, ch := range agentName {
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_') {
			return fmt.Errorf("agent_name must use only lowercase letters, digits, and underscores")
		}
	}
	return nil
}

func validateAbsoluteAgentPath(path string) error {
	if path == AgentPathMorpheus {
		return nil
	}
	stripped, ok := strings.CutPrefix(path, "/")
	if !ok {
		return fmt.Errorf("absolute agent paths must start with `/root` or be `/morpheus`")
	}
	segments := strings.Split(stripped, "/")
	if len(segments) == 0 || segments[0] != agentPathRootSeg {
		return fmt.Errorf("absolute agent paths must start with `/root` or be `/morpheus`")
	}
	if strings.HasSuffix(stripped, "/") {
		return fmt.Errorf("absolute agent path must not end with `/`")
	}
	for _, seg := range segments[1:] {
		if err := validateAgentName(seg); err != nil {
			return err
		}
	}
	return nil
}

// ToolName identifies a callable tool, preserving the namespace split when the
// model provides one.
//
// Mirrors Rust `ToolName`. Field names are emitted verbatim ("name",
// "namespace"); `namespace` uses Go's `omitempty` to match serde's
// `Option<String>` (absent fields decode to nil, but note: Rust here does NOT
// use skip_serializing_if, so a None namespace serializes as `null`). To match
// Rust exactly we therefore always emit the namespace key.
type ToolName struct {
	Name      string  `json:"name"`
	Namespace *string `json:"namespace"`
}

// PlainToolName constructs a ToolName with no namespace.
func PlainToolName(name string) ToolName {
	return ToolName{Name: name, Namespace: nil}
}

// NamespacedToolName constructs a ToolName with the given namespace.
func NamespacedToolName(namespace, name string) ToolName {
	ns := namespace
	return ToolName{Name: name, Namespace: &ns}
}

// String renders the tool name, prefixing the namespace when present. This
// mirrors the Rust Display impl which writes "{namespace}{name}".
func (t ToolName) String() string {
	if t.Namespace != nil {
		return *t.Namespace + t.Name
	}
	return t.Name
}
