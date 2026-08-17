package execserver

import (
	"github.com/sqlrush/codexgo/internal/filesystem"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// FsReadFileParams is the payload for the fs/readFile request.
//
// Rust: `FsReadFileParams` with camelCase fields; sandbox is an Option that is
// always present (null when absent).
type FsReadFileParams struct {
	Path    protocol.AbsolutePath                `json:"path"`
	Sandbox *filesystem.FileSystemSandboxContext `json:"sandbox"`
}

// FsReadFileResponse is the response to the fs/readFile request.
type FsReadFileResponse struct {
	DataBase64 string `json:"dataBase64"`
}

// FsWriteFileParams is the payload for the fs/writeFile request.
type FsWriteFileParams struct {
	Path       protocol.AbsolutePath                `json:"path"`
	DataBase64 string                               `json:"dataBase64"`
	Sandbox    *filesystem.FileSystemSandboxContext `json:"sandbox"`
}

// FsWriteFileResponse is the (empty) response to the fs/writeFile request.
type FsWriteFileResponse struct{}

// FsCreateDirectoryParams is the payload for the fs/createDirectory request.
type FsCreateDirectoryParams struct {
	Path      protocol.AbsolutePath                `json:"path"`
	Recursive *bool                                `json:"recursive"`
	Sandbox   *filesystem.FileSystemSandboxContext `json:"sandbox"`
}

// FsCreateDirectoryResponse is the (empty) response to fs/createDirectory.
type FsCreateDirectoryResponse struct{}

// FsGetMetadataParams is the payload for the fs/getMetadata request.
type FsGetMetadataParams struct {
	Path    protocol.AbsolutePath                `json:"path"`
	Sandbox *filesystem.FileSystemSandboxContext `json:"sandbox"`
}

// FsGetMetadataResponse is the response to the fs/getMetadata request.
type FsGetMetadataResponse struct {
	IsDirectory  bool  `json:"isDirectory"`
	IsFile       bool  `json:"isFile"`
	IsSymlink    bool  `json:"isSymlink"`
	CreatedAtMs  int64 `json:"createdAtMs"`
	ModifiedAtMs int64 `json:"modifiedAtMs"`
}

// FsReadDirectoryParams is the payload for the fs/readDirectory request.
type FsReadDirectoryParams struct {
	Path    protocol.AbsolutePath                `json:"path"`
	Sandbox *filesystem.FileSystemSandboxContext `json:"sandbox"`
}

// FsReadDirectoryEntry is a single directory entry in a read-directory response.
type FsReadDirectoryEntry struct {
	FileName    string `json:"fileName"`
	IsDirectory bool   `json:"isDirectory"`
	IsFile      bool   `json:"isFile"`
}

// FsReadDirectoryResponse is the response to the fs/readDirectory request.
type FsReadDirectoryResponse struct {
	Entries []FsReadDirectoryEntry `json:"entries"`
}

// FsRemoveParams is the payload for the fs/remove request.
type FsRemoveParams struct {
	Path      protocol.AbsolutePath                `json:"path"`
	Recursive *bool                                `json:"recursive"`
	Force     *bool                                `json:"force"`
	Sandbox   *filesystem.FileSystemSandboxContext `json:"sandbox"`
}

// FsRemoveResponse is the (empty) response to the fs/remove request.
type FsRemoveResponse struct{}

// FsCopyParams is the payload for the fs/copy request.
type FsCopyParams struct {
	SourcePath      protocol.AbsolutePath                `json:"sourcePath"`
	DestinationPath protocol.AbsolutePath                `json:"destinationPath"`
	Recursive       bool                                 `json:"recursive"`
	Sandbox         *filesystem.FileSystemSandboxContext `json:"sandbox"`
}

// FsCopyResponse is the (empty) response to the fs/copy request.
type FsCopyResponse struct{}

// HTTPHeader is one HTTP header in the executor protocol.
//
// Rust: `HttpHeader` with camelCase fields.
type HTTPHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HTTPRequestParams is the executor-side HTTP request envelope.
//
// Rust: `HttpRequestParams` with camelCase fields; headers and streamResponse
// default to their zero values, body is renamed to bodyBase64, and timeoutMs is
// omitted when nil (serde `skip_serializing_if = "Option::is_none"`).
type HTTPRequestParams struct {
	Method         string       `json:"method"`
	URL            string       `json:"url"`
	Headers        []HTTPHeader `json:"headers"`
	Body           *ByteChunk   `json:"bodyBase64"`
	TimeoutMs      *uint64      `json:"timeoutMs,omitempty"`
	RequestID      string       `json:"requestId"`
	StreamResponse bool         `json:"streamResponse"`
}

// HTTPRequestResponse is the HTTP response envelope from an http/request call.
type HTTPRequestResponse struct {
	Status  uint16       `json:"status"`
	Headers []HTTPHeader `json:"headers"`
	Body    ByteChunk    `json:"bodyBase64"`
}

// HTTPRequestBodyDeltaNotification is one ordered response-body frame for a
// streamResponse HTTP request.
//
// Rust: `HttpRequestBodyDeltaNotification`; delta is renamed to deltaBase64, and
// done/error default to their zero values.
type HTTPRequestBodyDeltaNotification struct {
	RequestID string    `json:"requestId"`
	Seq       uint64    `json:"seq"`
	Delta     ByteChunk `json:"deltaBase64"`
	Done      bool      `json:"done"`
	Error     *string   `json:"error"`
}
