package appserver

import (
	"context"
	"encoding/base64"
	"errors"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/filesystem"
)

// fsHandler implements the fs/* requests against the local filesystem. It is the
// reduced port of the Rust FsRequestProcessor, which delegates to the
// environment manager's local ExecutorFileSystem. Here it uses the
// DirectFileSystem directly (no sandbox), matching the local-environment path.
type fsHandler struct {
	fs filesystem.ExecutorFileSystem
}

// newFsHandler builds an fsHandler backed by the direct filesystem.
func newFsHandler() *fsHandler {
	return &fsHandler{fs: filesystem.NewDirectFileSystem()}
}

// readFile reads a file and returns its base64-encoded contents.
func (h *fsHandler) readFile(ctx context.Context, params *appserverproto.FsReadFileParams) (any, *RPCError) {
	bytes, err := h.fs.ReadFile(ctx, params.Path, nil)
	if err != nil {
		return nil, mapFsError(err)
	}
	return appserverproto.FsReadFileResponse{
		DataBase64: base64.StdEncoding.EncodeToString(bytes),
	}, nil
}

// writeFile decodes base64 contents and writes them to a file.
func (h *fsHandler) writeFile(ctx context.Context, params *appserverproto.FsWriteFileParams) (any, *RPCError) {
	bytes, err := base64.StdEncoding.DecodeString(params.DataBase64)
	if err != nil {
		return nil, invalidRequest("fs/writeFile requires valid base64 dataBase64: %v", err)
	}
	if err := h.fs.WriteFile(ctx, params.Path, bytes, nil); err != nil {
		return nil, mapFsError(err)
	}
	return appserverproto.FsWriteFileResponse{}, nil
}

// createDirectory creates a directory (recursive by default).
func (h *fsHandler) createDirectory(ctx context.Context, params *appserverproto.FsCreateDirectoryParams) (any, *RPCError) {
	recursive := true
	if params.Recursive != nil {
		recursive = *params.Recursive
	}
	if err := h.fs.CreateDirectory(ctx, params.Path, filesystem.CreateDirectoryOptions{Recursive: recursive}, nil); err != nil {
		return nil, mapFsError(err)
	}
	return appserverproto.FsCreateDirectoryResponse{}, nil
}

// getMetadata returns filesystem metadata for a path.
func (h *fsHandler) getMetadata(ctx context.Context, params *appserverproto.FsGetMetadataParams) (any, *RPCError) {
	meta, err := h.fs.GetMetadata(ctx, params.Path, nil)
	if err != nil {
		return nil, mapFsError(err)
	}
	return appserverproto.FsGetMetadataResponse{
		IsDirectory:  meta.IsDirectory,
		IsFile:       meta.IsFile,
		IsSymlink:    meta.IsSymlink,
		CreatedAtMs:  meta.CreatedAtMs,
		ModifiedAtMs: meta.ModifiedAtMs,
	}, nil
}

// readDirectory lists the direct children of a directory.
func (h *fsHandler) readDirectory(ctx context.Context, params *appserverproto.FsReadDirectoryParams) (any, *RPCError) {
	entries, err := h.fs.ReadDirectory(ctx, params.Path, nil)
	if err != nil {
		return nil, mapFsError(err)
	}
	out := make([]appserverproto.FsReadDirectoryEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, appserverproto.FsReadDirectoryEntry{
			FileName:    e.FileName,
			IsDirectory: e.IsDirectory,
			IsFile:      e.IsFile,
		})
	}
	return appserverproto.FsReadDirectoryResponse{Entries: out}, nil
}

// remove deletes a file or directory tree.
func (h *fsHandler) remove(ctx context.Context, params *appserverproto.FsRemoveParams) (any, *RPCError) {
	recursive := true
	if params.Recursive != nil {
		recursive = *params.Recursive
	}
	force := true
	if params.Force != nil {
		force = *params.Force
	}
	if err := h.fs.Remove(ctx, params.Path, filesystem.RemoveOptions{Recursive: recursive, Force: force}, nil); err != nil {
		return nil, mapFsError(err)
	}
	return appserverproto.FsRemoveResponse{}, nil
}

// copy copies a file or directory tree.
func (h *fsHandler) copy(ctx context.Context, params *appserverproto.FsCopyParams) (any, *RPCError) {
	if err := h.fs.Copy(ctx, params.SourcePath, params.DestinationPath, filesystem.CopyOptions{Recursive: params.Recursive}, nil); err != nil {
		return nil, mapFsError(err)
	}
	return appserverproto.FsCopyResponse{}, nil
}

// mapFsError translates a filesystem error into a JSON-RPC error, mirroring the
// Rust map_fs_error: InvalidInput-class errors become invalid-request, all
// others become internal-error.
func mapFsError(err error) *RPCError {
	var invalid *filesystem.InvalidInputError
	if errors.As(err, &invalid) {
		return invalidRequest("%s", err.Error())
	}
	return internalError("%s", err.Error())
}
