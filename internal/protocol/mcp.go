package protocol

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// This file ports `mcp.rs`: TS/JSON-schema friendly Model Context Protocol
// types embedded inside the Codex protocol. All structs use camelCase wire keys
// (`#[serde(rename_all = "camelCase")]`) and the `_meta` field renames to
// "_meta".

// RequestIdKind discriminates the untagged RequestId representation.
type RequestIdKind int

const (
	// RequestIdString is a string request id.
	RequestIdString RequestIdKind = iota
	// RequestIdInteger is an integer request id.
	RequestIdInteger
)

// RequestId is the id of a request, which can be either a string or an integer.
//
// Rust: `#[serde(untagged)]` enum over `String` and `i64`. On the wire it is a
// bare string or a bare JSON number, so marshaling/unmarshaling is custom.
type RequestId struct {
	Kind RequestIdKind
	// Str holds the value when Kind == RequestIdString.
	Str string
	// Int holds the value when Kind == RequestIdInteger.
	Int int64
}

// StringRequestId constructs a string-valued RequestId.
func StringRequestId(s string) RequestId {
	return RequestId{Kind: RequestIdString, Str: s}
}

// IntegerRequestId constructs an integer-valued RequestId.
func IntegerRequestId(i int64) RequestId {
	return RequestId{Kind: RequestIdInteger, Int: i}
}

// String renders the request id, mirroring the Rust Display impl.
func (r RequestId) String() string {
	if r.Kind == RequestIdInteger {
		return strconv.FormatInt(r.Int, 10)
	}
	return r.Str
}

// MarshalJSON emits a bare string or a bare number, matching the untagged enum.
func (r RequestId) MarshalJSON() ([]byte, error) {
	switch r.Kind {
	case RequestIdString:
		return json.Marshal(r.Str)
	case RequestIdInteger:
		return json.Marshal(r.Int)
	default:
		return nil, fmt.Errorf("unknown RequestId kind: %d", r.Kind)
	}
}

// UnmarshalJSON reads either a bare string or a bare integer. To match serde's
// untagged behavior, the string form is tried first, then the integer form.
func (r *RequestId) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*r = RequestId{Kind: RequestIdString, Str: s}
		return nil
	}
	var i int64
	if err := json.Unmarshal(data, &i); err != nil {
		return fmt.Errorf("RequestId must be a string or integer: %w", err)
	}
	*r = RequestId{Kind: RequestIdInteger, Int: i}
	return nil
}

// McpServerInfo is presentation metadata advertised by an initialized MCP
// server. Rust: camelCase; every field is a (possibly null) Option and is always
// emitted (no skip_serializing_if), so the optional keys remain present as null.
type McpServerInfo struct {
	Name        string            `json:"name"`
	Title       *string           `json:"title"`
	Version     string            `json:"version"`
	Description *string           `json:"description"`
	Icons       []json.RawMessage `json:"icons"`
	WebsiteURL  *string           `json:"websiteUrl"`
}

// Tool is a definition for a tool the client can call. Rust: camelCase. Optional
// fields use `skip_serializing_if = "Option::is_none"`; `_meta` renames to
// "_meta".
type Tool struct {
	Name         string            `json:"name"`
	Title        *string           `json:"title,omitempty"`
	Description  *string           `json:"description,omitempty"`
	InputSchema  json.RawMessage   `json:"inputSchema"`
	OutputSchema *json.RawMessage  `json:"outputSchema,omitempty"`
	Annotations  *json.RawMessage  `json:"annotations,omitempty"`
	Icons        []json.RawMessage `json:"icons,omitempty"`
	Meta         *json.RawMessage  `json:"_meta,omitempty"`
}

// Resource is a known resource that the server is capable of reading. Rust:
// camelCase; most fields are optional (skip_serializing_if).
type Resource struct {
	Annotations *json.RawMessage  `json:"annotations,omitempty"`
	Description *string           `json:"description,omitempty"`
	MimeType    *string           `json:"mimeType,omitempty"`
	Name        string            `json:"name"`
	Size        *int64            `json:"size,omitempty"`
	Title       *string           `json:"title,omitempty"`
	URI         string            `json:"uri"`
	Icons       []json.RawMessage `json:"icons,omitempty"`
	Meta        *json.RawMessage  `json:"_meta,omitempty"`
}

// ResourceContentKind discriminates the untagged ResourceContent.
type ResourceContentKind int

const (
	// ResourceContentText is the text-content variant.
	ResourceContentText ResourceContentKind = iota
	// ResourceContentBlob is the blob-content variant.
	ResourceContentBlob
)

// ResourceContent is the content returned when reading a resource from an MCP
// server. Rust: `#[serde(untagged)]` over a Text and a Blob variant; the variant
// is distinguished structurally (Text carries "text", Blob carries "blob").
// Inner fields are camelCase; `_meta` renames to "_meta".
type ResourceContent struct {
	Kind ResourceContentKind

	// URI is the URI of this resource (both variants).
	URI string
	// MimeType is optional (both variants).
	MimeType *string
	// Meta is the optional "_meta" field (both variants).
	Meta *json.RawMessage

	// Text holds the text payload when Kind == ResourceContentText.
	Text string
	// Blob holds the blob payload when Kind == ResourceContentBlob.
	Blob string
}

// MarshalJSON emits the untagged ResourceContent form for the active variant.
func (r ResourceContent) MarshalJSON() ([]byte, error) {
	m := map[string]any{"uri": r.URI}
	if r.MimeType != nil {
		m["mimeType"] = *r.MimeType
	}
	switch r.Kind {
	case ResourceContentText:
		m["text"] = r.Text
	case ResourceContentBlob:
		m["blob"] = r.Blob
	default:
		return nil, fmt.Errorf("unknown ResourceContent kind: %d", r.Kind)
	}
	if r.Meta != nil {
		m["_meta"] = r.Meta
	}
	return json.Marshal(m)
}

// UnmarshalJSON reads the untagged ResourceContent. Matching serde's untagged
// behavior, the Text variant is attempted first (presence of "text"), then Blob.
func (r *ResourceContent) UnmarshalJSON(data []byte) error {
	var probe struct {
		URI      string           `json:"uri"`
		MimeType *string          `json:"mimeType"`
		Text     *string          `json:"text"`
		Blob     *string          `json:"blob"`
		Meta     *json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	out := ResourceContent{URI: probe.URI, MimeType: probe.MimeType, Meta: probe.Meta}
	switch {
	case probe.Text != nil:
		out.Kind = ResourceContentText
		out.Text = *probe.Text
	case probe.Blob != nil:
		out.Kind = ResourceContentBlob
		out.Blob = *probe.Blob
	default:
		return fmt.Errorf("ResourceContent must contain a text or blob field")
	}
	*r = out
	return nil
}

// ResourceTemplate is a template description for resources available on the
// server. Rust: camelCase; `uriTemplate` and `name` are required.
type ResourceTemplate struct {
	Annotations *json.RawMessage `json:"annotations,omitempty"`
	URITemplate string           `json:"uriTemplate"`
	Name        string           `json:"name"`
	Title       *string          `json:"title,omitempty"`
	Description *string          `json:"description,omitempty"`
	MimeType    *string          `json:"mimeType,omitempty"`
}

// CallToolResult is the server's response to a tool call. Rust: camelCase. This
// is the structured form of the value kept as raw JSON in McpToolCallItem.Result.
type CallToolResult struct {
	Content           []json.RawMessage `json:"content"`
	StructuredContent *json.RawMessage  `json:"structuredContent,omitempty"`
	IsError           *bool             `json:"isError,omitempty"`
	Meta              *json.RawMessage  `json:"_meta,omitempty"`
}
