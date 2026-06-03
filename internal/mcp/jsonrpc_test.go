package mcp

import (
	"encoding/json"
	"testing"
)

func TestRequestMarshalJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		id     int64
		method string
		params any
		want   string
	}{
		{
			name:   "with params",
			id:     1,
			method: "tools/list",
			params: map[string]any{"cursor": "abc"},
			want:   `{"id":1,"jsonrpc":"2.0","method":"tools/list","params":{"cursor":"abc"}}`,
		},
		{
			name:   "nil params omits key",
			id:     7,
			method: "ping",
			params: nil,
			want:   `{"id":7,"jsonrpc":"2.0","method":"ping"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req, err := newRequest(tc.id, tc.method, tc.params)
			if err != nil {
				t.Fatalf("newRequest: %v", err)
			}
			got, err := json.Marshal(req)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !jsonEqual(t, got, tc.want) {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestNotificationMarshalJSON(t *testing.T) {
	t.Parallel()
	note, err := newNotification("notifications/initialized", nil)
	if err != nil {
		t.Fatalf("newNotification: %v", err)
	}
	got, err := json.Marshal(note)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	if !jsonEqual(t, got, want) {
		t.Fatalf("got %s, want %s", got, want)
	}
	// A notification must not carry an "id".
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(got, &probe); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if _, ok := probe["id"]; ok {
		t.Fatalf("notification must not contain an id field: %s", got)
	}
}

func TestMarshalParams(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      any
		wantNil bool
		want    string
	}{
		{name: "nil", in: nil, wantNil: true},
		{name: "empty raw", in: json.RawMessage(nil), wantNil: true},
		{name: "raw passthrough", in: json.RawMessage(`{"a":1}`), want: `{"a":1}`},
		{name: "struct", in: PaginatedParams{Cursor: strPtr("x")}, want: `{"cursor":"x"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := marshalParams(tc.in)
			if err != nil {
				t.Fatalf("marshalParams: %v", err)
			}
			if tc.wantNil {
				if got != nil {
					t.Fatalf("want nil, got %s", got)
				}
				return
			}
			if !jsonEqual(t, got, tc.want) {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestResponseClassification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		raw            string
		isResponse     bool
		isServerReq    bool
		isServerNotify bool
	}{
		{
			name:       "result response",
			raw:        `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`,
			isResponse: true,
		},
		{
			name:       "error response",
			raw:        `{"jsonrpc":"2.0","id":2,"error":{"code":-32000,"message":"boom"}}`,
			isResponse: true,
		},
		{
			name:        "server request",
			raw:         `{"jsonrpc":"2.0","id":3,"method":"elicitation/create","params":{}}`,
			isServerReq: true,
		},
		{
			name:           "server notification",
			raw:            `{"jsonrpc":"2.0","method":"notifications/message","params":{}}`,
			isServerNotify: true,
		},
		{
			name:           "method with null id is notification",
			raw:            `{"jsonrpc":"2.0","id":null,"method":"x"}`,
			isServerNotify: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var msg Response
			if err := json.Unmarshal([]byte(tc.raw), &msg); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := msg.IsResponse(); got != tc.isResponse {
				t.Errorf("IsResponse=%v want %v", got, tc.isResponse)
			}
			if got := msg.IsServerRequest(); got != tc.isServerReq {
				t.Errorf("IsServerRequest=%v want %v", got, tc.isServerReq)
			}
			if got := msg.IsServerNotification(); got != tc.isServerNotify {
				t.Errorf("IsServerNotification=%v want %v", got, tc.isServerNotify)
			}
		})
	}
}

func TestRPCErrorError(t *testing.T) {
	t.Parallel()
	var nilErr *RPCError
	if got := nilErr.Error(); got != "<nil rpc error>" {
		t.Fatalf("nil RPCError: got %q", got)
	}
	withData := &RPCError{Code: -32001, Message: "bad", Data: json.RawMessage(`{"x":1}`)}
	if got := withData.Error(); got != `jsonrpc error -32001: bad ({"x":1})` {
		t.Fatalf("with data: got %q", got)
	}
	plain := &RPCError{Code: -32000, Message: "oops"}
	if got := plain.Error(); got != "jsonrpc error -32000: oops" {
		t.Fatalf("plain: got %q", got)
	}
}

func TestDecodeIntID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		raw    string
		wantID int64
		wantOK bool
	}{
		{name: "int", raw: "42", wantID: 42, wantOK: true},
		{name: "null", raw: "null", wantOK: false},
		{name: "empty", raw: "", wantOK: false},
		{name: "string", raw: `"x"`, wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			id, ok := decodeIntID(json.RawMessage(tc.raw))
			if ok != tc.wantOK || id != tc.wantID {
				t.Fatalf("decodeIntID(%q)=(%d,%v) want (%d,%v)", tc.raw, id, ok, tc.wantID, tc.wantOK)
			}
		})
	}
}

// jsonEqual reports whether got and want are equal JSON documents (ignoring key
// order). It fails the test if either side is not valid JSON.
func jsonEqual(t *testing.T, got []byte, want string) bool {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("got is not valid JSON: %v (%s)", err, got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("want is not valid JSON: %v (%s)", err, want)
	}
	gn, _ := json.Marshal(g)
	wn, _ := json.Marshal(w)
	return string(gn) == string(wn)
}
