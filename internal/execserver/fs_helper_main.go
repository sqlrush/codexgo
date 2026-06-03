package execserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// RunFsHelperMain reads a single fs helper request from in, executes it against
// the direct filesystem, and writes the JSON-encoded response (followed by a
// newline) to out. It returns the process exit code: 0 on success, 1 on an I/O
// or decode failure (with a diagnostic written to errOut).
//
// Rust: `fs_helper_main::main` / `run_main`. The Go port takes the streams as
// parameters so it is testable; a binary entrypoint wires os.Stdin/os.Stdout/
// os.Stderr and calls os.Exit with the returned code.
func RunFsHelperMain(ctx context.Context, in io.Reader, out, errOut io.Writer) int {
	input, err := io.ReadAll(in)
	if err != nil {
		fmt.Fprintf(errOut, "fs sandbox helper failed: %v\n", err)
		return 1
	}

	var request FsHelperRequest
	if err := json.Unmarshal(input, &request); err != nil {
		fmt.Fprintf(errOut, "fs sandbox helper failed: %v\n", err)
		return 1
	}

	var response FsHelperResponse
	if payload, rpcErr := RunDirectRequest(ctx, request); rpcErr != nil {
		response = NewErrorResponse(rpcErr)
	} else {
		response = NewOkResponse(payload)
	}

	encoded, err := json.Marshal(response)
	if err != nil {
		fmt.Fprintf(errOut, "fs sandbox helper failed: %v\n", err)
		return 1
	}
	if _, err := out.Write(encoded); err != nil {
		fmt.Fprintf(errOut, "fs sandbox helper failed: %v\n", err)
		return 1
	}
	if _, err := out.Write([]byte("\n")); err != nil {
		fmt.Fprintf(errOut, "fs sandbox helper failed: %v\n", err)
		return 1
	}
	return 0
}
