package execserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// DispatchProcessMethod routes a process/* JSON-RPC request to the backend and
// returns the marshaled result or a JSON-RPC error.
//
// This mirrors the request-routing the server processor performs for the
// process methods (process/start, process/read, process/write,
// process/terminate). Unknown methods return a method-not-found error, matching
// the Rust processor's fallback. Params decoding failures return invalid-params.
//
// It is the minimal request surface for using [LocalProcess] as a standalone
// process-execution service without the full websocket/HTTP transport.
func DispatchProcessMethod(ctx context.Context, backend *LocalProcess, method string, params json.RawMessage) (json.RawMessage, *appserverproto.JSONRPCErrorBody) {
	switch method {
	case ExecMethod:
		var p ExecParams
		if err := decodeParams(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
		resp, rpcErr := backend.Exec(ctx, p)
		return marshalResult(resp, rpcErr)
	case ExecReadMethod:
		var p ReadParams
		if err := decodeParams(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
		resp, rpcErr := backend.ExecRead(ctx, p)
		return marshalResult(resp, rpcErr)
	case ExecWriteMethod:
		var p WriteParams
		if err := decodeParams(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
		resp, rpcErr := backend.ExecWrite(ctx, p)
		return marshalResult(resp, rpcErr)
	case ExecTerminateMethod:
		var p TerminateParams
		if err := decodeParams(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
		resp, rpcErr := backend.Terminate(p)
		return marshalResult(resp, rpcErr)
	default:
		return nil, methodNotFound(fmt.Sprintf("unknown method: %s", method))
	}
}

// DispatchFsMethod routes an fs/* JSON-RPC request to the direct filesystem and
// returns the marshaled result or a JSON-RPC error. Unknown methods return a
// method-not-found error.
func DispatchFsMethod(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, *appserverproto.JSONRPCErrorBody) {
	var request FsHelperRequest
	switch method {
	case FsReadFileMethod:
		var p FsReadFileParams
		if err := decodeParams(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
		request = NewReadFileRequest(p)
	case FsWriteFileMethod:
		var p FsWriteFileParams
		if err := decodeParams(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
		request = NewWriteFileRequest(p)
	case FsCreateDirectoryMethod:
		var p FsCreateDirectoryParams
		if err := decodeParams(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
		request = NewCreateDirectoryRequest(p)
	case FsGetMetadataMethod:
		var p FsGetMetadataParams
		if err := decodeParams(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
		request = NewGetMetadataRequest(p)
	case FsReadDirectoryMethod:
		var p FsReadDirectoryParams
		if err := decodeParams(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
		request = NewReadDirectoryRequest(p)
	case FsRemoveMethod:
		var p FsRemoveParams
		if err := decodeParams(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
		request = NewRemoveRequest(p)
	case FsCopyMethod:
		var p FsCopyParams
		if err := decodeParams(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
		request = NewCopyRequest(p)
	default:
		return nil, methodNotFound(fmt.Sprintf("unknown method: %s", method))
	}

	payload, rpcErr := RunDirectRequest(ctx, request)
	if rpcErr != nil {
		return nil, rpcErr
	}
	return marshalResult(payload, nil)
}

// decodeParams decodes JSON-RPC params into target, treating a missing/null
// params object the same way serde does for the executor methods.
//
// Rust: `decode_params` in rpc.rs, which substitutes Null for absent params and
// retries an empty object as Null so unit-shaped params decode cleanly.
func decodeParams[T any](params json.RawMessage, target *T) error {
	if len(params) == 0 || string(params) == "null" {
		params = json.RawMessage("null")
	}
	if err := json.Unmarshal(params, target); err != nil {
		if string(params) == "{}" {
			// Retry an empty object as null for unit-shaped params.
			if retryErr := json.Unmarshal(json.RawMessage("null"), target); retryErr == nil {
				return nil
			}
		}
		return err
	}
	return nil
}

// marshalResult marshals a successful result or returns the JSON-RPC error.
func marshalResult(result any, rpcErr *appserverproto.JSONRPCErrorBody) (json.RawMessage, *appserverproto.JSONRPCErrorBody) {
	if rpcErr != nil {
		return nil, rpcErr
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, internalError(err.Error())
	}
	return data, nil
}
