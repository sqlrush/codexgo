package protocol

import (
	"encoding/json"
	"fmt"
)

// This file ports `parse_command.rs`: the `ParsedCommand` enum describing a
// best-effort interpretation of a shell command.

// ParsedCommandKind is the discriminator for ParsedCommand. Rust:
// `#[serde(tag = "type", rename_all = "snake_case")]`. There is no
// `#[serde(other)]` fallback, so unknown types are rejected on decode.
type ParsedCommandKind string

const (
	// ParsedCommandRead is a command that reads a single file.
	ParsedCommandRead ParsedCommandKind = "read"
	// ParsedCommandListFiles is a command that lists files.
	ParsedCommandListFiles ParsedCommandKind = "list_files"
	// ParsedCommandSearch is a command that searches files.
	ParsedCommandSearch ParsedCommandKind = "search"
	// ParsedCommandUnknown is a command that could not be classified.
	ParsedCommandUnknown ParsedCommandKind = "unknown"
)

// ParsedCommand is the structured (best-effort) interpretation of a shell
// command. It is an internally-tagged enum (discriminator "type").
//
// Optional fields (ListFiles.Path, Search.Query, Search.Path) have no
// `skip_serializing_if` in Rust, so they are always emitted and encode as JSON
// null when absent.
type ParsedCommand struct {
	Type ParsedCommandKind

	// Cmd is the original command string (all variants).
	Cmd string

	// Name is the file name for the Read variant.
	Name string
	// Path is the file path. For Read it is a required string (Rust PathBuf);
	// for ListFiles and Search it is an optional string (Rust Option<String>),
	// represented by ReadPath / OptPath respectively below.
	//
	// ReadPath holds the required path of the Read variant.
	ReadPath string
	// OptPath holds the optional path of the ListFiles and Search variants.
	OptPath *string
	// Query holds the optional query of the Search variant.
	Query *string
}

// NewReadParsedCommand builds a Read ParsedCommand.
func NewReadParsedCommand(cmd, name, path string) ParsedCommand {
	return ParsedCommand{Type: ParsedCommandRead, Cmd: cmd, Name: name, ReadPath: path}
}

// NewListFilesParsedCommand builds a ListFiles ParsedCommand.
func NewListFilesParsedCommand(cmd string, path *string) ParsedCommand {
	return ParsedCommand{Type: ParsedCommandListFiles, Cmd: cmd, OptPath: path}
}

// NewSearchParsedCommand builds a Search ParsedCommand.
func NewSearchParsedCommand(cmd string, query, path *string) ParsedCommand {
	return ParsedCommand{Type: ParsedCommandSearch, Cmd: cmd, Query: query, OptPath: path}
}

// NewUnknownParsedCommand builds an Unknown ParsedCommand.
func NewUnknownParsedCommand(cmd string) ParsedCommand {
	return ParsedCommand{Type: ParsedCommandUnknown, Cmd: cmd}
}

// MarshalJSON emits the internally-tagged ParsedCommand. Optional path/query
// fields are emitted even when nil (encoded as null), matching the Rust struct
// variants which lack `skip_serializing_if`.
func (p ParsedCommand) MarshalJSON() ([]byte, error) {
	m := map[string]any{"type": string(p.Type), "cmd": p.Cmd}
	switch p.Type {
	case ParsedCommandRead:
		m["name"] = p.Name
		m["path"] = p.ReadPath
	case ParsedCommandListFiles:
		m["path"] = optStringValue(p.OptPath)
	case ParsedCommandSearch:
		m["query"] = optStringValue(p.Query)
		m["path"] = optStringValue(p.OptPath)
	case ParsedCommandUnknown:
		// Only the cmd field is present.
	default:
		return nil, fmt.Errorf("unknown ParsedCommand type: %q", p.Type)
	}
	return json.Marshal(m)
}

// optStringValue returns a value suitable for JSON encoding of an Option<String>
// without skip_serializing_if: the string when present, or nil (JSON null).
func optStringValue(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// UnmarshalJSON decodes the internally-tagged ParsedCommand. Unknown "type"
// values are rejected (no serde `other` fallback in Rust).
func (p *ParsedCommand) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type  string  `json:"type"`
		Cmd   string  `json:"cmd"`
		Name  string  `json:"name"`
		Path  *string `json:"path"`
		Query *string `json:"query"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := ParsedCommand{Cmd: raw.Cmd}
	switch ParsedCommandKind(raw.Type) {
	case ParsedCommandRead:
		out.Type = ParsedCommandRead
		out.Name = raw.Name
		if raw.Path != nil {
			out.ReadPath = *raw.Path
		}
	case ParsedCommandListFiles:
		out.Type = ParsedCommandListFiles
		out.OptPath = raw.Path
	case ParsedCommandSearch:
		out.Type = ParsedCommandSearch
		out.Query = raw.Query
		out.OptPath = raw.Path
	case ParsedCommandUnknown:
		out.Type = ParsedCommandUnknown
	default:
		return fmt.Errorf("unknown ParsedCommand type: %q", raw.Type)
	}
	*p = out
	return nil
}
