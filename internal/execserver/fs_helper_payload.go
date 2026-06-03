package execserver

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// FsHelperPayload is the adjacently tagged successful fs helper payload.
//
// Rust: `FsHelperPayload` with `#[serde(tag = "operation", content = "response")]`.
type FsHelperPayload struct {
	operation       fsHelperOperation
	readFile        *FsReadFileResponse
	writeFile       *FsWriteFileResponse
	createDirectory *FsCreateDirectoryResponse
	getMetadata     *FsGetMetadataResponse
	readDirectory   *FsReadDirectoryResponse
	remove          *FsRemoveResponse
	copy            *FsCopyResponse
}

func newReadFilePayload(r FsReadFileResponse) FsHelperPayload {
	return FsHelperPayload{operation: FsReadFileMethod, readFile: &r}
}

func newWriteFilePayload(r FsWriteFileResponse) FsHelperPayload {
	return FsHelperPayload{operation: FsWriteFileMethod, writeFile: &r}
}

func newCreateDirectoryPayload(r FsCreateDirectoryResponse) FsHelperPayload {
	return FsHelperPayload{operation: FsCreateDirectoryMethod, createDirectory: &r}
}

func newGetMetadataPayload(r FsGetMetadataResponse) FsHelperPayload {
	return FsHelperPayload{operation: FsGetMetadataMethod, getMetadata: &r}
}

func newReadDirectoryPayload(r FsReadDirectoryResponse) FsHelperPayload {
	return FsHelperPayload{operation: FsReadDirectoryMethod, readDirectory: &r}
}

func newRemovePayload(r FsRemoveResponse) FsHelperPayload {
	return FsHelperPayload{operation: FsRemoveMethod, remove: &r}
}

func newCopyPayload(r FsCopyResponse) FsHelperPayload {
	return FsHelperPayload{operation: FsCopyMethod, copy: &r}
}

// Operation returns the fs/* method name of the payload.
func (p FsHelperPayload) Operation() string { return string(p.operation) }

// ReadFile returns the read-file response, or an error if the payload holds a
// different operation. Mirrors `FsHelperPayload::expect_read_file`.
func (p FsHelperPayload) ReadFile() (FsReadFileResponse, *appserverproto.JSONRPCErrorBody) {
	if p.operation == FsReadFileMethod {
		return *p.readFile, nil
	}
	return FsReadFileResponse{}, unexpectedResponse(FsReadFileMethod, string(p.operation))
}

// WriteFile returns the write-file response. Mirrors `expect_write_file`.
func (p FsHelperPayload) WriteFile() (FsWriteFileResponse, *appserverproto.JSONRPCErrorBody) {
	if p.operation == FsWriteFileMethod {
		return *p.writeFile, nil
	}
	return FsWriteFileResponse{}, unexpectedResponse(FsWriteFileMethod, string(p.operation))
}

// CreateDirectory returns the create-directory response. Mirrors
// `expect_create_directory`.
func (p FsHelperPayload) CreateDirectory() (FsCreateDirectoryResponse, *appserverproto.JSONRPCErrorBody) {
	if p.operation == FsCreateDirectoryMethod {
		return *p.createDirectory, nil
	}
	return FsCreateDirectoryResponse{}, unexpectedResponse(FsCreateDirectoryMethod, string(p.operation))
}

// GetMetadata returns the get-metadata response. Mirrors `expect_get_metadata`.
func (p FsHelperPayload) GetMetadata() (FsGetMetadataResponse, *appserverproto.JSONRPCErrorBody) {
	if p.operation == FsGetMetadataMethod {
		return *p.getMetadata, nil
	}
	return FsGetMetadataResponse{}, unexpectedResponse(FsGetMetadataMethod, string(p.operation))
}

// ReadDirectory returns the read-directory response. Mirrors
// `expect_read_directory`.
func (p FsHelperPayload) ReadDirectory() (FsReadDirectoryResponse, *appserverproto.JSONRPCErrorBody) {
	if p.operation == FsReadDirectoryMethod {
		return *p.readDirectory, nil
	}
	return FsReadDirectoryResponse{}, unexpectedResponse(FsReadDirectoryMethod, string(p.operation))
}

// Remove returns the remove response. Mirrors `expect_remove`.
func (p FsHelperPayload) Remove() (FsRemoveResponse, *appserverproto.JSONRPCErrorBody) {
	if p.operation == FsRemoveMethod {
		return *p.remove, nil
	}
	return FsRemoveResponse{}, unexpectedResponse(FsRemoveMethod, string(p.operation))
}

// Copy returns the copy response. Mirrors `expect_copy`.
func (p FsHelperPayload) Copy() (FsCopyResponse, *appserverproto.JSONRPCErrorBody) {
	if p.operation == FsCopyMethod {
		return *p.copy, nil
	}
	return FsCopyResponse{}, unexpectedResponse(FsCopyMethod, string(p.operation))
}

func unexpectedResponse(expected, actual string) *appserverproto.JSONRPCErrorBody {
	return internalError(fmt.Sprintf("unexpected fs sandbox helper response: expected %s, got %s", expected, actual))
}

// MarshalJSON encodes the payload as `{"operation": <method>, "response": <...>}`.
func (p FsHelperPayload) MarshalJSON() ([]byte, error) {
	var content any
	switch p.operation {
	case FsReadFileMethod:
		content = p.readFile
	case FsWriteFileMethod:
		content = p.writeFile
	case FsCreateDirectoryMethod:
		content = p.createDirectory
	case FsGetMetadataMethod:
		content = p.getMetadata
	case FsReadDirectoryMethod:
		content = p.readDirectory
	case FsRemoveMethod:
		content = p.remove
	case FsCopyMethod:
		content = p.copy
	default:
		return nil, fmt.Errorf("execserver: unknown fs helper operation %q", p.operation)
	}
	return marshalAdjacent(string(p.operation), "response", content)
}

// UnmarshalJSON decodes the adjacently tagged payload.
func (p *FsHelperPayload) UnmarshalJSON(data []byte) error {
	op, content, err := decodeAdjacent(data, "response")
	if err != nil {
		return err
	}
	*p = FsHelperPayload{operation: fsHelperOperation(op)}
	switch p.operation {
	case FsReadFileMethod:
		return decodeInto(content, &p.readFile)
	case FsWriteFileMethod:
		return decodeInto(content, &p.writeFile)
	case FsCreateDirectoryMethod:
		return decodeInto(content, &p.createDirectory)
	case FsGetMetadataMethod:
		return decodeInto(content, &p.getMetadata)
	case FsReadDirectoryMethod:
		return decodeInto(content, &p.readDirectory)
	case FsRemoveMethod:
		return decodeInto(content, &p.remove)
	case FsCopyMethod:
		return decodeInto(content, &p.copy)
	default:
		return fmt.Errorf("execserver: unknown fs helper operation %q", op)
	}
}

// FsHelperResponse is the adjacently tagged fs helper response: either a
// successful payload or a JSON-RPC error.
//
// Rust: `FsHelperResponse` with `#[serde(tag = "status", content = "payload",
// rename_all = "camelCase")]`. The status tag is "ok" or "error".
type FsHelperResponse struct {
	ok    *FsHelperPayload
	error *appserverproto.JSONRPCErrorBody
}

// NewOkResponse builds a successful fs helper response.
func NewOkResponse(payload FsHelperPayload) FsHelperResponse {
	return FsHelperResponse{ok: &payload}
}

// NewErrorResponse builds an error fs helper response.
func NewErrorResponse(err *appserverproto.JSONRPCErrorBody) FsHelperResponse {
	return FsHelperResponse{error: err}
}

// Ok returns the payload and true when the response is successful.
func (r FsHelperResponse) Ok() (FsHelperPayload, bool) {
	if r.ok != nil {
		return *r.ok, true
	}
	return FsHelperPayload{}, false
}

// Err returns the error and true when the response is an error.
func (r FsHelperResponse) Err() (*appserverproto.JSONRPCErrorBody, bool) {
	if r.error != nil {
		return r.error, true
	}
	return nil, false
}

// MarshalJSON encodes the response with the camelCase status tag.
func (r FsHelperResponse) MarshalJSON() ([]byte, error) {
	switch {
	case r.ok != nil:
		return marshalAdjacent("ok", "payload", r.ok)
	case r.error != nil:
		return marshalAdjacent("error", "payload", r.error)
	default:
		return nil, fmt.Errorf("execserver: empty fs helper response")
	}
}

// UnmarshalJSON decodes the response with the camelCase status tag.
func (r *FsHelperResponse) UnmarshalJSON(data []byte) error {
	status, content, err := decodeStatusAdjacent(data, "payload")
	if err != nil {
		return err
	}
	switch status {
	case "ok":
		var payload FsHelperPayload
		if err := json.Unmarshal(content, &payload); err != nil {
			return err
		}
		*r = FsHelperResponse{ok: &payload}
		return nil
	case "error":
		var body appserverproto.JSONRPCErrorBody
		if err := json.Unmarshal(content, &body); err != nil {
			return err
		}
		*r = FsHelperResponse{error: &body}
		return nil
	default:
		return fmt.Errorf("execserver: unknown fs helper response status %q", status)
	}
}

// marshalAdjacent encodes `{<tagKey>: tag, <contentKey>: content}` with the tag
// emitted first, matching serde's adjacently tagged field order. The tag key is
// "status" for responses (contentKey "payload") and "operation" otherwise.
func marshalAdjacent(tag, contentKey string, content any) ([]byte, error) {
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	tagKey := "operation"
	if contentKey == "payload" {
		tagKey = "status"
	}
	tagKeyJSON, err := json.Marshal(tagKey)
	if err != nil {
		return nil, err
	}
	tagJSON, err := json.Marshal(tag)
	if err != nil {
		return nil, err
	}
	contentKeyJSON, err := json.Marshal(contentKey)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	buf.Write(tagKeyJSON)
	buf.WriteByte(':')
	buf.Write(tagJSON)
	buf.WriteByte(',')
	buf.Write(contentKeyJSON)
	buf.WriteByte(':')
	buf.Write(contentJSON)
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// decodeAdjacent decodes an `{operation, <contentKey>}` envelope.
func decodeAdjacent(data []byte, contentKey string) (string, json.RawMessage, error) {
	return decodeTaggedAdjacent(data, "operation", contentKey)
}

// decodeStatusAdjacent decodes a `{status, <contentKey>}` envelope.
func decodeStatusAdjacent(data []byte, contentKey string) (string, json.RawMessage, error) {
	return decodeTaggedAdjacent(data, "status", contentKey)
}

func decodeTaggedAdjacent(data []byte, tagKey, contentKey string) (string, json.RawMessage, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", nil, err
	}
	tagRaw, ok := raw[tagKey]
	if !ok {
		return "", nil, fmt.Errorf("execserver: missing %q tag", tagKey)
	}
	var tag string
	if err := json.Unmarshal(tagRaw, &tag); err != nil {
		return "", nil, err
	}
	content := raw[contentKey]
	return tag, content, nil
}

// decodeInto unmarshals content into a pointer-to-pointer target, leaving it nil
// when content is absent.
func decodeInto[T any](content json.RawMessage, target **T) error {
	if len(content) == 0 {
		return nil
	}
	var value T
	if err := json.Unmarshal(content, &value); err != nil {
		return err
	}
	*target = &value
	return nil
}
