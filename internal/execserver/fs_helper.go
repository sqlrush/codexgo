package execserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/filesystem"
)

// CodexFsHelperArg1 is the first argument that selects the fs helper mode.
//
// Rust: `CODEX_FS_HELPER_ARG1 = "--codex-run-as-fs-helper"`.
const CodexFsHelperArg1 = "--codex-run-as-fs-helper"

// fsHelperOperation enumerates the fs helper operations, identified by their
// `fs/*` method name on the wire.
type fsHelperOperation string

// FsHelperRequest is the adjacently tagged fs helper request envelope.
//
// Rust: `FsHelperRequest` with `#[serde(tag = "operation", content = "params")]`.
// The operation tag is the `fs/*` method name; exactly one params payload is set
// per request.
type FsHelperRequest struct {
	operation       fsHelperOperation
	readFile        *FsReadFileParams
	writeFile       *FsWriteFileParams
	createDirectory *FsCreateDirectoryParams
	getMetadata     *FsGetMetadataParams
	readDirectory   *FsReadDirectoryParams
	remove          *FsRemoveParams
	copy            *FsCopyParams
}

// NewReadFileRequest builds an fs/readFile request.
func NewReadFileRequest(params FsReadFileParams) FsHelperRequest {
	return FsHelperRequest{operation: FsReadFileMethod, readFile: &params}
}

// NewWriteFileRequest builds an fs/writeFile request.
func NewWriteFileRequest(params FsWriteFileParams) FsHelperRequest {
	return FsHelperRequest{operation: FsWriteFileMethod, writeFile: &params}
}

// NewCreateDirectoryRequest builds an fs/createDirectory request.
func NewCreateDirectoryRequest(params FsCreateDirectoryParams) FsHelperRequest {
	return FsHelperRequest{operation: FsCreateDirectoryMethod, createDirectory: &params}
}

// NewGetMetadataRequest builds an fs/getMetadata request.
func NewGetMetadataRequest(params FsGetMetadataParams) FsHelperRequest {
	return FsHelperRequest{operation: FsGetMetadataMethod, getMetadata: &params}
}

// NewReadDirectoryRequest builds an fs/readDirectory request.
func NewReadDirectoryRequest(params FsReadDirectoryParams) FsHelperRequest {
	return FsHelperRequest{operation: FsReadDirectoryMethod, readDirectory: &params}
}

// NewRemoveRequest builds an fs/remove request.
func NewRemoveRequest(params FsRemoveParams) FsHelperRequest {
	return FsHelperRequest{operation: FsRemoveMethod, remove: &params}
}

// NewCopyRequest builds an fs/copy request.
func NewCopyRequest(params FsCopyParams) FsHelperRequest {
	return FsHelperRequest{operation: FsCopyMethod, copy: &params}
}

// fsHelperEnvelope is the adjacently tagged wire shape used by both request and
// payload (tag "operation", content "params"/"response").
type fsHelperEnvelope struct {
	Operation fsHelperOperation `json:"operation"`
	Content   json.RawMessage   `json:"-"`
}

// MarshalJSON encodes the request as `{"operation": <method>, "params": <...>}`.
func (r FsHelperRequest) MarshalJSON() ([]byte, error) {
	content, err := r.contentJSON()
	if err != nil {
		return nil, err
	}
	return marshalAdjacent(string(r.operation), "params", content)
}

func (r FsHelperRequest) contentJSON() (any, error) {
	switch r.operation {
	case FsReadFileMethod:
		return r.readFile, nil
	case FsWriteFileMethod:
		return r.writeFile, nil
	case FsCreateDirectoryMethod:
		return r.createDirectory, nil
	case FsGetMetadataMethod:
		return r.getMetadata, nil
	case FsReadDirectoryMethod:
		return r.readDirectory, nil
	case FsRemoveMethod:
		return r.remove, nil
	case FsCopyMethod:
		return r.copy, nil
	default:
		return nil, fmt.Errorf("execserver: unknown fs helper operation %q", r.operation)
	}
}

// UnmarshalJSON decodes the adjacently tagged request.
func (r *FsHelperRequest) UnmarshalJSON(data []byte) error {
	op, content, err := decodeAdjacent(data, "params")
	if err != nil {
		return err
	}
	*r = FsHelperRequest{operation: fsHelperOperation(op)}
	switch r.operation {
	case FsReadFileMethod:
		return decodeInto(content, &r.readFile)
	case FsWriteFileMethod:
		return decodeInto(content, &r.writeFile)
	case FsCreateDirectoryMethod:
		return decodeInto(content, &r.createDirectory)
	case FsGetMetadataMethod:
		return decodeInto(content, &r.getMetadata)
	case FsReadDirectoryMethod:
		return decodeInto(content, &r.readDirectory)
	case FsRemoveMethod:
		return decodeInto(content, &r.remove)
	case FsCopyMethod:
		return decodeInto(content, &r.copy)
	default:
		return fmt.Errorf("execserver: unknown fs helper operation %q", op)
	}
}

// RunDirectRequest executes an fs helper request against the direct filesystem
// (no sandbox) and returns the payload or a JSON-RPC error.
//
// Rust: `run_direct_request`.
func RunDirectRequest(ctx context.Context, request FsHelperRequest) (FsHelperPayload, *appserverproto.JSONRPCErrorBody) {
	directFS := filesystem.NewDirectFileSystem()
	switch request.operation {
	case FsReadFileMethod:
		data, err := directFS.ReadFile(ctx, request.readFile.Path, nil)
		if err != nil {
			return FsHelperPayload{}, mapFsError(err)
		}
		return newReadFilePayload(FsReadFileResponse{DataBase64: base64.StdEncoding.EncodeToString(data)}), nil
	case FsWriteFileMethod:
		bytes, decErr := base64.StdEncoding.DecodeString(request.writeFile.DataBase64)
		if decErr != nil {
			return FsHelperPayload{}, invalidRequest(fmt.Sprintf("%s requires valid base64 dataBase64: %s", FsWriteFileMethod, decErr))
		}
		if err := directFS.WriteFile(ctx, request.writeFile.Path, bytes, nil); err != nil {
			return FsHelperPayload{}, mapFsError(err)
		}
		return newWriteFilePayload(FsWriteFileResponse{}), nil
	case FsCreateDirectoryMethod:
		opts := filesystem.CreateDirectoryOptions{Recursive: optBool(request.createDirectory.Recursive, true)}
		if err := directFS.CreateDirectory(ctx, request.createDirectory.Path, opts, nil); err != nil {
			return FsHelperPayload{}, mapFsError(err)
		}
		return newCreateDirectoryPayload(FsCreateDirectoryResponse{}), nil
	case FsGetMetadataMethod:
		md, err := directFS.GetMetadata(ctx, request.getMetadata.Path, nil)
		if err != nil {
			return FsHelperPayload{}, mapFsError(err)
		}
		return newGetMetadataPayload(FsGetMetadataResponse{
			IsDirectory:  md.IsDirectory,
			IsFile:       md.IsFile,
			IsSymlink:    md.IsSymlink,
			CreatedAtMs:  md.CreatedAtMs,
			ModifiedAtMs: md.ModifiedAtMs,
		}), nil
	case FsReadDirectoryMethod:
		entries, err := directFS.ReadDirectory(ctx, request.readDirectory.Path, nil)
		if err != nil {
			return FsHelperPayload{}, mapFsError(err)
		}
		converted := make([]FsReadDirectoryEntry, 0, len(entries))
		for _, e := range entries {
			converted = append(converted, FsReadDirectoryEntry{
				FileName:    e.FileName,
				IsDirectory: e.IsDirectory,
				IsFile:      e.IsFile,
			})
		}
		return newReadDirectoryPayload(FsReadDirectoryResponse{Entries: converted}), nil
	case FsRemoveMethod:
		opts := filesystem.RemoveOptions{
			Recursive: optBool(request.remove.Recursive, true),
			Force:     optBool(request.remove.Force, true),
		}
		if err := directFS.Remove(ctx, request.remove.Path, opts, nil); err != nil {
			return FsHelperPayload{}, mapFsError(err)
		}
		return newRemovePayload(FsRemoveResponse{}), nil
	case FsCopyMethod:
		opts := filesystem.CopyOptions{Recursive: request.copy.Recursive}
		if err := directFS.Copy(ctx, request.copy.SourcePath, request.copy.DestinationPath, opts, nil); err != nil {
			return FsHelperPayload{}, mapFsError(err)
		}
		return newCopyPayload(FsCopyResponse{}), nil
	default:
		return FsHelperPayload{}, internalError(fmt.Sprintf("unknown fs helper operation %q", request.operation))
	}
}

// mapFsError converts a filesystem io error into a JSON-RPC error, matching the
// Rust `map_fs_error`: NotFound -> not_found, InvalidInput/PermissionDenied ->
// invalid_request, everything else -> internal_error.
func mapFsError(err error) *appserverproto.JSONRPCErrorBody {
	if err == nil {
		return nil
	}
	var invalidInput *filesystem.InvalidInputError
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return notFound(err.Error())
	case errors.As(err, &invalidInput) || errors.Is(err, fs.ErrPermission):
		return invalidRequest(err.Error())
	default:
		return internalError(err.Error())
	}
}

// optBool returns the dereferenced bool, or fallback when the pointer is nil.
func optBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
