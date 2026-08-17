package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/pkg/config"
)

// pagingResourceServer answers initialize, then two pages of resources and a
// single page of resource templates. The first resources page advertises a
// nextCursor so the client must page.
func pagingResourceServer() responder {
	return func(req Response) []json.RawMessage {
		switch req.Method {
		case MethodInitialize:
			return []json.RawMessage{resultFrame(reqID(req), `{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"s","version":"1"}}`)}
		case MethodInitializedNotify:
			return nil
		case MethodToolsList:
			return []json.RawMessage{resultFrame(reqID(req), `{"tools":[]}`)}
		case MethodResourcesList:
			var p struct {
				Params struct {
					Cursor *string `json:"cursor"`
				} `json:"params"`
			}
			_ = json.Unmarshal(rawOf(req), &p)
			if p.Params.Cursor == nil {
				return []json.RawMessage{resultFrame(reqID(req), `{"resources":[{"uri":"file:///a","name":"a"}],"nextCursor":"c2"}`)}
			}
			return []json.RawMessage{resultFrame(reqID(req), `{"resources":[{"uri":"file:///b","name":"b"}]}`)}
		case MethodResourceTemplatesList:
			return []json.RawMessage{resultFrame(reqID(req), `{"resourceTemplates":[{"uriTemplate":"file:///{x}","name":"tmpl"}]}`)}
		default:
			return []json.RawMessage{errorFrame(reqID(req), -32601, "method not found")}
		}
	}
}

// rawOf re-serializes a decoded request so the params object can be re-read.
func rawOf(req Response) []byte {
	out := map[string]json.RawMessage{}
	if len(req.Params) > 0 {
		out["params"] = req.Params
	}
	b, _ := json.Marshal(out)
	return b
}

func TestClientListAllResourcesPaging(t *testing.T) {
	t.Parallel()
	tr := newFakeTransport(pagingResourceServer())
	client := NewClient(tr)
	defer client.Close()
	mustInit(t, client)

	resources, err := client.ListAllResources(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("ListAllResources: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("expected 2 resources across pages, got %d", len(resources))
	}
	if resources[0].URI != "file:///a" || resources[1].URI != "file:///b" {
		t.Fatalf("resources=%+v", resources)
	}
}

func TestClientListAllResourceTemplates(t *testing.T) {
	t.Parallel()
	tr := newFakeTransport(pagingResourceServer())
	client := NewClient(tr)
	defer client.Close()
	mustInit(t, client)

	templates, err := client.ListAllResourceTemplates(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("ListAllResourceTemplates: %v", err)
	}
	if len(templates) != 1 {
		t.Fatalf("expected 1 template, got %d", len(templates))
	}
}

func TestClientListToolsDuplicateCursorGuard(t *testing.T) {
	t.Parallel()
	// A misbehaving server that always returns the same cursor must not loop
	// forever; ListAllTools detects the repeat and errors.
	tr := newFakeTransport(func(req Response) []json.RawMessage {
		switch req.Method {
		case MethodInitialize:
			return []json.RawMessage{resultFrame(reqID(req), `{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"s","version":"1"}}`)}
		case MethodInitializedNotify:
			return nil
		case MethodToolsList:
			return []json.RawMessage{resultFrame(reqID(req), `{"tools":[{"name":"t","inputSchema":{}}],"nextCursor":"same"}`)}
		default:
			return []json.RawMessage{errorFrame(reqID(req), -32601, "nope")}
		}
	})
	client := NewClient(tr)
	defer client.Close()
	mustInit(t, client)

	_, err := client.ListAllTools(context.Background(), time.Second)
	if err == nil {
		t.Fatal("expected duplicate-cursor error")
	}
}

func TestManagerListResourcesAndTemplates(t *testing.T) {
	t.Parallel()
	factory := &stubFactory{servers: map[string]responder{"alpha": pagingResourceServer()}}
	mgr, _, err := NewManager(context.Background(), map[string]config.McpServerConfig{"alpha": stdioServerConfig()}, ManagerOptions{Factory: factory})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()

	resources, err := mgr.ListResources(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
	}

	templates, err := mgr.ListResourceTemplates(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("ListResourceTemplates: %v", err)
	}
	if len(templates) != 1 {
		t.Fatalf("expected 1 template, got %d", len(templates))
	}

	all := mgr.ListAllResources(context.Background())
	if len(all["alpha"]) != 2 {
		t.Fatalf("ListAllResources[alpha]=%v", all["alpha"])
	}
}
