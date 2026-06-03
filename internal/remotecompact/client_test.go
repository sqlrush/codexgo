package remotecompact

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/client"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// fakeTransport is a programmable HTTPTransport for tests. It records the last
// executed request and returns a scripted sequence of responses/errors.
type fakeTransport struct {
	responses []client.Response
	errs      []error
	calls     int
	lastReq   client.Request
}

func (f *fakeTransport) Execute(_ context.Context, req client.Request) (client.Response, error) {
	f.lastReq = req
	idx := f.calls
	f.calls++
	var resp client.Response
	if idx < len(f.responses) {
		resp = f.responses[idx]
	}
	var err error
	if idx < len(f.errs) {
		err = f.errs[idx]
	}
	return resp, err
}

func (f *fakeTransport) Stream(context.Context, client.Request) (client.StreamResponse, error) {
	return client.StreamResponse{}, errors.New("stream not supported in fakeTransport")
}

// recordingTelemetry captures OnRequest calls.
type recordingTelemetry struct {
	attempts []uint64
	statuses []int
}

func (r *recordingTelemetry) OnRequest(attempt uint64, status int, _ *client.TransportError, _ time.Duration) {
	r.attempts = append(r.attempts, attempt)
	r.statuses = append(r.statuses, status)
}

func testProvider() api.Provider {
	return api.Provider{
		Name:    "openai",
		BaseURL: "https://api.example.test/v1",
		Retry:   api.DefaultRetryConfig(),
	}
}

func TestCompactClientCompactInputSuccess(t *testing.T) {
	output := []protocol.ResponseItem{
		{Type: protocol.ResponseItemKindCompaction, EncryptedContent: strptr("enc")},
	}
	body, err := json.Marshal(struct {
		Output []protocol.ResponseItem `json:"output"`
	}{Output: output})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	ft := &fakeTransport{responses: []client.Response{{Status: 200, Body: body}}}
	tel := &recordingTelemetry{}
	c := NewCompactClient(ft, testProvider(), api.NoOpAuth{}).WithTelemetry(tel)

	got, err := c.CompactInput(
		context.Background(),
		CompactionInput{Model: "m", Input: []protocol.ResponseItem{userMessage("user", "hi")}},
		http.Header{"X-Test": {"1"}},
		5*time.Second,
	)
	if err != nil {
		t.Fatalf("CompactInput: %v", err)
	}
	if len(got) != 1 || got[0].Type != protocol.ResponseItemKindCompaction {
		t.Fatalf("output = %+v, want one compaction item", got)
	}
	if ft.calls != 1 {
		t.Fatalf("calls = %d, want 1", ft.calls)
	}
	// Verify the endpoint path, method, headers, timeout, and JSON body type.
	if ft.lastReq.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", ft.lastReq.Method)
	}
	wantURL := "https://api.example.test/v1/responses/compact"
	if ft.lastReq.URL != wantURL {
		t.Errorf("url = %s, want %s", ft.lastReq.URL, wantURL)
	}
	if ft.lastReq.Headers.Get("X-Test") != "1" {
		t.Errorf("extra header not forwarded: %v", ft.lastReq.Headers)
	}
	if ft.lastReq.Timeout != 5*time.Second {
		t.Errorf("timeout = %s, want 5s", ft.lastReq.Timeout)
	}
	if ft.lastReq.Body == nil || ft.lastReq.Body.Kind != client.RequestBodyJSON {
		t.Errorf("body = %+v, want JSON body", ft.lastReq.Body)
	}
	if len(tel.attempts) != 1 || tel.statuses[0] != 200 {
		t.Errorf("telemetry = %+v / %+v", tel.attempts, tel.statuses)
	}
}

func TestCompactClientRetriesOn5xxThenSucceeds(t *testing.T) {
	okBody := []byte(`{"output":[]}`)
	ft := &fakeTransport{
		responses: []client.Response{{}, {Status: 200, Body: okBody}},
		errs: []error{
			client.NewHTTPError(503, "u", http.Header{}, "overloaded"),
			nil,
		},
	}
	c := NewCompactClient(ft, testProvider(), api.NoOpAuth{})

	got, err := c.Compact(context.Background(), json.RawMessage(`{}`), nil, 0)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("output = %+v, want empty", got)
	}
	if ft.calls != 2 {
		t.Fatalf("calls = %d, want 2 (one retry)", ft.calls)
	}
}

func TestCompactClientReturnsTransportErrorOn4xx(t *testing.T) {
	ft := &fakeTransport{
		responses: []client.Response{{}},
		errs:      []error{client.NewHTTPError(400, "u", http.Header{}, "bad")},
	}
	c := NewCompactClient(ft, testProvider(), api.NoOpAuth{})

	_, err := c.Compact(context.Background(), json.RawMessage(`{}`), nil, 0)
	if err == nil {
		t.Fatal("expected error on 4xx")
	}
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *api.APIError", err)
	}
	if apiErr.Kind != api.APIErrorTransport {
		t.Errorf("kind = %v, want transport", apiErr.Kind)
	}
	// 4xx (other than 429) is not retried by the default policy.
	if ft.calls != 1 {
		t.Errorf("calls = %d, want 1 (no retry on 4xx)", ft.calls)
	}
}

func TestCompactClientParseErrorIsStreamError(t *testing.T) {
	ft := &fakeTransport{responses: []client.Response{{Status: 200, Body: []byte("not json")}}}
	c := NewCompactClient(ft, testProvider(), api.NoOpAuth{})

	_, err := c.Compact(context.Background(), json.RawMessage(`{}`), nil, 0)
	if err == nil {
		t.Fatal("expected parse error")
	}
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) || apiErr.Kind != api.APIErrorStream {
		t.Fatalf("error = %v, want APIErrorStream", err)
	}
}
