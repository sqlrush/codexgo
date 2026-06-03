//go:build unix

package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeEchoScript writes a minimal line-delimited JSON-RPC stdio "server" as a
// shell script: for every input line it emits one canned result frame, echoing
// back the request id. This exercises the real child-process stdio transport.
func writeEchoScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "echo_server.sh")
	const script = `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  if [ -z "$id" ]; then
    continue
  fi
  printf '{"jsonrpc":"2.0","id":%s,"result":{"ok":true}}\n' "$id"
done
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

func TestLaunchStdioRoundTrip(t *testing.T) {
	t.Parallel()
	script := writeEchoScript(t)

	tr, err := LaunchStdio(context.Background(), StdioCommand{
		Program: "/bin/sh",
		Args:    []string{script},
		Env:     map[string]string{"PATH": os.Getenv("PATH")},
	}, t.TempDir())
	if err != nil {
		t.Fatalf("LaunchStdio: %v", err)
	}
	defer tr.Close()

	ctx := context.Background()
	if err := tr.Send(ctx, []byte(`{"jsonrpc":"2.0","id":7,"method":"ping"}`)); err != nil {
		t.Fatalf("Send: %v", err)
	}

	recvCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	frame, err := tr.Receive(recvCtx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	var resp Response
	if err := json.Unmarshal(frame, &resp); err != nil {
		t.Fatalf("decode: %v (%s)", err, frame)
	}
	id, ok := decodeIntID(resp.ID)
	if !ok || id != 7 {
		t.Fatalf("response id=%v ok=%v want 7", id, ok)
	}
}

func TestLaunchStdioRequiresProgram(t *testing.T) {
	t.Parallel()
	_, err := LaunchStdio(context.Background(), StdioCommand{}, "")
	if err == nil {
		t.Fatal("expected error for empty program")
	}
}

func TestStdioClientHandshake(t *testing.T) {
	t.Parallel()
	// A shell server that answers initialize and tools/list, exercising the full
	// client handshake over a real stdio transport.
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp_server.sh")
	const script = `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"sh-server","version":"0.1"}}}\n' "$id"
      ;;
    *'"method":"tools/list"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"t","inputSchema":{"type":"object"}}]}}\n' "$id"
      ;;
    *'"method":"notifications/initialized"'*)
      ;;
    *)
      [ -n "$id" ] && printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
  esac
done
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	tr, err := LaunchStdio(context.Background(), StdioCommand{
		Program: "/bin/sh",
		Args:    []string{path},
		Env:     map[string]string{"PATH": os.Getenv("PATH")},
	}, t.TempDir())
	if err != nil {
		t.Fatalf("LaunchStdio: %v", err)
	}
	client := NewClient(tr)
	defer client.Close()

	res, err := client.Initialize(context.Background(), InitializeParams{}, 3*time.Second)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if res.ServerInfo.Name != "sh-server" {
		t.Fatalf("serverInfo=%+v", res.ServerInfo)
	}

	toolsList, err := client.ListAllTools(context.Background(), 3*time.Second)
	if err != nil {
		t.Fatalf("ListAllTools: %v", err)
	}
	if len(toolsList) != 1 || toolsList[0].Name != "t" {
		t.Fatalf("tools=%+v", toolsList)
	}
}
