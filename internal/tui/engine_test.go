package tui

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/internal/appserverclient"
	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// fakeEngineClient is a scripted engineClient for testing the engine spine.
type fakeEngineClient struct {
	mu        sync.Mutex
	requests  []fakeRequest
	responses map[string]json.RawMessage
	events    []appserverclient.ServerEvent
	cursor    int
}

type fakeRequest struct {
	method string
	params any
}

func (f *fakeEngineClient) RequestTyped(_ context.Context, method string, params any, out any) error {
	f.mu.Lock()
	f.requests = append(f.requests, fakeRequest{method: method, params: params})
	resp, ok := f.responses[method]
	f.mu.Unlock()
	if ok && out != nil {
		return json.Unmarshal(resp, out)
	}
	return nil
}

func (f *fakeEngineClient) NextEvent(ctx context.Context) (appserverclient.ServerEvent, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cursor >= len(f.events) {
		return appserverclient.ServerEvent{}, false
	}
	ev := f.events[f.cursor]
	f.cursor++
	return ev, true
}

func (f *fakeEngineClient) Shutdown(context.Context) {}

func codexEvent(t *testing.T, id string) appserverclient.ServerEvent {
	t.Helper()
	params, err := json.Marshal(map[string]any{
		"id":  id,
		"msg": map[string]any{"type": "agent_message", "message": "hi"},
	})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return appserverclient.ServerEvent{
		Notification: &appserverproto.JSONRPCNotification{
			Method: appserver.CodexEventMethod,
			Params: params,
		},
	}
}

func TestEngineStart(t *testing.T) {
	fake := &fakeEngineClient{
		responses: map[string]json.RawMessage{
			"thread/start": json.RawMessage(`{"thread":{"id":"thread-123"}}`),
		},
	}
	e := NewEngine(EngineConfig{Client: fake, Sender: NewAppEventSender()})
	id, err := e.Start(context.Background(), "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if id != "thread-123" {
		t.Fatalf("thread id = %q, want thread-123", id)
	}
	if e.ThreadID() != "thread-123" {
		t.Fatalf("stored thread id = %q", e.ThreadID())
	}
	// initialize then thread/start were issued.
	if len(fake.requests) != 2 || fake.requests[0].method != "initialize" || fake.requests[1].method != "thread/start" {
		t.Fatalf("unexpected request sequence: %+v", fake.requests)
	}
}

func TestEngineSubmitTurnRequiresThread(t *testing.T) {
	fake := &fakeEngineClient{}
	e := NewEngine(EngineConfig{Client: fake, Sender: NewAppEventSender()})
	err := e.SubmitTurn(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("SubmitTurn without active thread should error")
	}
}

func TestEnginePumpDeliversCoreEvents(t *testing.T) {
	fake := &fakeEngineClient{
		events: nil, // set below
	}
	fake.events = []appserverclient.ServerEvent{codexEvent(t, "a"), codexEvent(t, "b")}

	var (
		mu       sync.Mutex
		core     int
		closed   bool
		recvDone = make(chan struct{})
	)
	sender := NewAppEventSender()
	sender.attachFunc(func(msg tea.Msg) {
		mu.Lock()
		defer mu.Unlock()
		switch msg.(type) {
		case CoreEventMsg:
			core++
		case EngineClosedMsg:
			closed = true
			close(recvDone)
		}
	})

	e := NewEngine(EngineConfig{Client: fake, Sender: sender})
	go e.Pump(context.Background())
	<-recvDone

	mu.Lock()
	defer mu.Unlock()
	if core != 2 {
		t.Fatalf("delivered %d core events, want 2", core)
	}
	if !closed {
		t.Fatal("expected EngineClosedMsg on stream end")
	}
}

func TestEnginePumpForwardsErrors(t *testing.T) {
	fake := &fakeEngineClient{events: []appserverclient.ServerEvent{
		{Error: &appserverproto.JSONRPCError{Error: appserverproto.JSONRPCErrorBody{Code: -1, Message: "boom"}}},
	}}
	var (
		mu      sync.Mutex
		errSeen bool
		done    = make(chan struct{})
	)
	sender := NewAppEventSender()
	sender.attachFunc(func(msg tea.Msg) {
		mu.Lock()
		defer mu.Unlock()
		switch msg.(type) {
		case EngineErrorMsg:
			errSeen = true
		case EngineClosedMsg:
			close(done)
		}
	})
	e := NewEngine(EngineConfig{Client: fake, Sender: sender})
	go e.Pump(context.Background())
	<-done
	mu.Lock()
	defer mu.Unlock()
	if !errSeen {
		t.Fatal("expected an EngineErrorMsg for the server error event")
	}
}
