package mcp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
)

// StdioCommand describes the local stdio MCP server process to launch. It is a
// focused port of codex's StdioServerCommand for the local launcher path.
type StdioCommand struct {
	// Program is the executable to run. On Unix the OS resolves it via PATH and
	// shebangs; the program is passed through unchanged.
	Program string
	// Args are the command-line arguments passed after the program name.
	Args []string
	// Env is the complete environment for the child process. The caller builds
	// it (see [BuildStdioEnv]) so the child does not inherit the orchestrator's
	// full environment.
	Env map[string]string
	// Cwd is the working directory for the child process. When empty the
	// fallback working directory supplied to the launcher is used.
	Cwd string
}

// stdioTransport carries line-delimited JSON-RPC over a child process's stdin
// and stdout. Each JSON-RPC message occupies exactly one newline-terminated
// line, matching the MCP stdio transport contract.
type stdioTransport struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner

	sendMu sync.Mutex

	closeOnce sync.Once
	closeErr  error
}

// LaunchStdio starts the configured command as a local child process and returns
// a [Transport] speaking line-delimited JSON-RPC over its stdin/stdout. The
// child's stderr is streamed to the standard logger, prefixed with the program
// name, matching the reference launcher's diagnostics behavior.
//
// fallbackCwd is used when command.Cwd is empty so relative programs resolve
// from the caller's runtime working directory.
func LaunchStdio(ctx context.Context, command StdioCommand, fallbackCwd string) (Transport, error) {
	if command.Program == "" {
		return nil, errors.New("mcp: stdio command requires a program")
	}

	cmd := exec.CommandContext(ctx, command.Program, command.Args...)
	cmd.Env = flattenEnv(command.Env)
	if command.Cwd != "" {
		cmd.Dir = command.Cwd
	} else {
		cmd.Dir = fallbackCwd
	}
	configureProcessGroup(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: open stdin for %q: %w", command.Program, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: open stdout for %q: %w", command.Program, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: open stderr for %q: %w", command.Program, err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp: start %q: %w", command.Program, err)
	}

	program := command.Program
	go logChildStderr(program, stderr)

	scanner := bufio.NewScanner(stdout)
	// MCP servers can emit large tool schemas on a single line; raise the
	// scanner buffer well above the default 64 KiB to avoid truncation.
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	return &stdioTransport{
		cmd:     cmd,
		stdin:   stdin,
		scanner: scanner,
	}, nil
}

// Send writes one JSON-RPC message followed by a newline to the child's stdin.
func (t *stdioTransport) Send(ctx context.Context, message []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.sendMu.Lock()
	defer t.sendMu.Unlock()

	if _, err := t.stdin.Write(message); err != nil {
		return fmt.Errorf("mcp: write to stdio server: %w", err)
	}
	if _, err := t.stdin.Write([]byte("\n")); err != nil {
		return fmt.Errorf("mcp: write newline to stdio server: %w", err)
	}
	return nil
}

// Receive reads the next newline-delimited JSON-RPC message from the child's
// stdout. Reads run on a background goroutine so the call honors context
// cancellation.
func (t *stdioTransport) Receive(ctx context.Context) ([]byte, error) {
	type readResult struct {
		line []byte
		err  error
	}
	resultCh := make(chan readResult, 1)
	go func() {
		if t.scanner.Scan() {
			line := t.scanner.Bytes()
			out := make([]byte, len(line))
			copy(out, line)
			resultCh <- readResult{line: out}
			return
		}
		if err := t.scanner.Err(); err != nil {
			resultCh <- readResult{err: fmt.Errorf("mcp: read from stdio server: %w", err)}
			return
		}
		resultCh <- readResult{err: ErrTransportClosed}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resultCh:
		return res.line, res.err
	}
}

// Close terminates the child process and closes the stdin pipe. It is
// idempotent.
func (t *stdioTransport) Close() error {
	t.closeOnce.Do(func() {
		_ = t.stdin.Close()
		t.closeErr = terminateProcess(t.cmd)
	})
	return t.closeErr
}

// logChildStderr streams a child process's stderr to the standard logger,
// matching the reference launcher which logs each line.
func logChildStderr(program string, stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		log.Printf("MCP server stderr (%s): %s", program, scanner.Text())
	}
}

// flattenEnv converts an env map to the "KEY=VALUE" slice form expected by
// exec.Cmd. A nil map yields a nil slice (the child inherits nothing).
func flattenEnv(env map[string]string) []string {
	if env == nil {
		return nil
	}
	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	return out
}
