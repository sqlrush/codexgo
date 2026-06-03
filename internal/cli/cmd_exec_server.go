package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/execserver"
)

// runExecServerSubcommand handles `codex exec-server`: run the standalone
// exec-server service. The reference codex supports a `ws://` listener (default)
// and remote-environment registration; this port wires the `stdio` transport,
// which serves the process-execution and filesystem JSON-RPC surface over
// stdin/stdout. Unsupported transports (ws:// listeners, remote registration)
// emit a clear notice and exit non-zero rather than failing silently.
func runExecServerSubcommand(ctx context.Context, parsed ParsedCommandLine, streams Streams) int {
	args, err := parseExecServerArgs(parsed.SubcommandArgs)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "error: %v\n", err)
		return 2
	}
	if args.help {
		printExecServerHelp(streams.Stdout)
		return 0
	}
	if args.remote != "" {
		fmt.Fprintln(streams.Stderr,
			"remote exec-server registration (--remote) requires ChatGPT/API-key auth and the remote-environment client, which is not wired in this build")
		return 1
	}
	if !isStdioListen(args.listen) {
		fmt.Fprintf(streams.Stderr,
			"exec-server transport %q is not supported in this build; only the stdio transport is wired (use --listen stdio)\n",
			args.listen)
		return 1
	}

	if err := serveExecServerStdio(ctx, streams.Stdin, streams.Stdout); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
			return 0
		}
		fmt.Fprintf(streams.Stderr, "codex exec-server: %v\n", err)
		return 1
	}
	return 0
}

// execServerArgs holds the parsed `codex exec-server` flags.
type execServerArgs struct {
	help   bool
	listen string
	remote string
}

// parseExecServerArgs parses the exec-server flags this port models. Unknown
// flags are rejected to surface misuse, matching the strict subcommand parsers.
func parseExecServerArgs(args []string) (execServerArgs, error) {
	var out execServerArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			out.help = true
		case arg == "--strict-config":
			// Accepted for compatibility; config validation is owned by the loader.
		case arg == "--listen":
			v, ni, err := takeValue(args, i, arg)
			if err != nil {
				return execServerArgs{}, err
			}
			out.listen, i = v, ni-1
		case strings.HasPrefix(arg, "--listen="):
			out.listen = strings.TrimPrefix(arg, "--listen=")
		case arg == "--remote":
			v, ni, err := takeValue(args, i, arg)
			if err != nil {
				return execServerArgs{}, err
			}
			out.remote, i = v, ni-1
		case strings.HasPrefix(arg, "--remote="):
			out.remote = strings.TrimPrefix(arg, "--remote=")
		case arg == "--environment-id" || arg == "--name":
			// Only meaningful with --remote; consume the attached value.
			if _, ni, err := takeValue(args, i, arg); err == nil {
				i = ni - 1
			}
		case strings.HasPrefix(arg, "--environment-id=") || strings.HasPrefix(arg, "--name="):
			// Accepted; only meaningful with --remote.
		case arg == "--use-agent-identity-auth":
			// Only meaningful with --remote.
		default:
			return execServerArgs{}, fmt.Errorf("unexpected argument: %s", arg)
		}
	}
	return out, nil
}

// isStdioListen reports whether the listen URL selects the stdio transport. An
// empty value defaults to stdio in this build (the reference default is ws://,
// which is not wired here).
func isStdioListen(listen string) bool {
	switch listen {
	case "", "stdio", "stdio://":
		return true
	default:
		return false
	}
}

// execServerStdioSender writes process notifications to the stdout stream as
// JSON-RPC notification envelopes. It is safe for concurrent use.
type execServerStdioSender struct {
	mu  *sync.Mutex
	out io.Writer
}

// Notify implements execserver.NotificationSender by emitting a line-delimited
// JSON-RPC notification.
func (s execServerStdioSender) Notify(_ context.Context, method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshaling exec-server notification params: %w", err)
	}
	msg := appserverproto.NewNotificationMessage(appserverproto.JSONRPCNotification{
		Method: method,
		Params: raw,
	})
	return writeExecServerMessage(s.mu, s.out, msg)
}

// serveExecServerStdio runs the line-delimited JSON-RPC loop over stdin/stdout,
// routing process/* requests to a LocalProcess backend and fs/* requests to the
// direct filesystem dispatch. It returns nil on clean EOF.
func serveExecServerStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	var writeMu sync.Mutex
	sender := execServerStdioSender{mu: &writeMu, out: out}
	backend := execserver.NewLocalProcess(sender, nil)

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg appserverproto.JSONRPCMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			// Malformed input: report a parse error without an id and continue.
			_ = writeExecServerError(&writeMu, out, appserverproto.NewStringRequestId(""),
				-32700, fmt.Sprintf("parse error: %v", err))
			continue
		}
		if msg.Kind != appserverproto.MessageKindRequest || msg.Request == nil {
			// Notifications/responses are not part of the client->server request
			// surface; ignore them silently as the Rust processor does.
			continue
		}
		handleExecServerRequest(ctx, backend, &writeMu, out, *msg.Request)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading exec-server input: %w", err)
	}
	return nil
}

// handleExecServerRequest routes a single request and writes its response.
func handleExecServerRequest(ctx context.Context, backend *execserver.LocalProcess, mu *sync.Mutex, out io.Writer, req appserverproto.JSONRPCRequest) {
	if req.Method == execserver.InitializeMethod {
		resp, _ := json.Marshal(execserver.InitializeResponse{SessionID: "stdio"})
		_ = writeExecServerResult(mu, out, req.ID, resp)
		return
	}

	var (
		result json.RawMessage
		rpcErr *appserverproto.JSONRPCErrorBody
	)
	switch {
	case strings.HasPrefix(req.Method, "process/"):
		result, rpcErr = execserver.DispatchProcessMethod(ctx, backend, req.Method, req.Params)
	case strings.HasPrefix(req.Method, "fs/"):
		result, rpcErr = execserver.DispatchFsMethod(ctx, req.Method, req.Params)
	default:
		_ = writeExecServerError(mu, out, req.ID, -32601, "unknown method: "+req.Method)
		return
	}

	if rpcErr != nil {
		_ = writeExecServerError(mu, out, req.ID, rpcErr.Code, rpcErr.Message)
		return
	}
	_ = writeExecServerResult(mu, out, req.ID, result)
}

// writeExecServerResult writes a success response envelope.
func writeExecServerResult(mu *sync.Mutex, out io.Writer, id appserverproto.RequestId, result json.RawMessage) error {
	msg := appserverproto.NewResponseMessage(appserverproto.JSONRPCResponse{ID: id, Result: result})
	return writeExecServerMessage(mu, out, msg)
}

// writeExecServerError writes an error response envelope.
func writeExecServerError(mu *sync.Mutex, out io.Writer, id appserverproto.RequestId, code int64, message string) error {
	msg := appserverproto.NewErrorMessage(appserverproto.JSONRPCError{
		ID:    id,
		Error: appserverproto.JSONRPCErrorBody{Code: code, Message: message},
	})
	return writeExecServerMessage(mu, out, msg)
}

// writeExecServerMessage serializes and writes one line-delimited JSON-RPC
// message, serializing concurrent writers with mu.
func writeExecServerMessage(mu *sync.Mutex, out io.Writer, msg appserverproto.JSONRPCMessage) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling exec-server message: %w", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if _, err := out.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("writing exec-server message: %w", err)
	}
	return nil
}

func printExecServerHelp(w io.Writer) {
	fmt.Fprintln(w, "[EXPERIMENTAL] Run the standalone exec-server service")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex exec-server [--listen <URL>]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "      --listen <URL>   Transport endpoint. Supported in this build: stdio, stdio://")
	fmt.Fprintln(w, "  -h, --help           Print help")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Note: the ws:// listener and remote-environment registration (--remote) are not wired in this build.")
}
