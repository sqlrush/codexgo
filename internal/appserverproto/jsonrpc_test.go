package appserverproto

import (
	"encoding/json"
	"testing"
)

// roundTrip marshals v, asserts the JSON equals wantJSON (after canonicalizing
// both sides), then unmarshals back into a fresh value and re-marshals to
// confirm stability.
func assertJSON(t *testing.T, got any, wantJSON string) {
	t.Helper()
	gotBytes, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !jsonEqual(t, gotBytes, []byte(wantJSON)) {
		t.Fatalf("marshal mismatch:\n got: %s\nwant: %s", gotBytes, wantJSON)
	}
}

// jsonEqual compares two JSON documents structurally (order-insensitive for
// object keys), matching the "byte-for-byte after key-order canonicalization"
// requirement.
func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		t.Fatalf("unmarshal a (%s): %v", a, err)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		t.Fatalf("unmarshal b (%s): %v", b, err)
	}
	ar, _ := json.Marshal(av)
	br, _ := json.Marshal(bv)
	return string(ar) == string(br)
}

func TestRequestIdRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		id   RequestId
		want string
	}{
		{"integer", NewIntegerRequestId(7), "7"},
		{"integer-zero", NewIntegerRequestId(0), "0"},
		{"integer-negative", NewIntegerRequestId(-3), "-3"},
		{"string", NewStringRequestId("req-1"), `"req-1"`},
		{"string-numeric", NewStringRequestId("42"), `"42"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.id)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(b) != tc.want {
				t.Fatalf("marshal = %s, want %s", b, tc.want)
			}
			var back RequestId
			if err := json.Unmarshal(b, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if back != tc.id {
				t.Fatalf("round-trip = %+v, want %+v", back, tc.id)
			}
		})
	}
}

func TestRequestIdRejectsInvalid(t *testing.T) {
	var id RequestId
	if err := json.Unmarshal([]byte("true"), &id); err == nil {
		t.Fatal("expected error for boolean RequestId")
	}
}

func TestJSONRPCRequestOmitsEmptyParamsAndTrace(t *testing.T) {
	req := JSONRPCRequest{ID: NewIntegerRequestId(1), Method: "thread/start"}
	assertJSON(t, req, `{"id":1,"method":"thread/start"}`)
}

func TestJSONRPCRequestWithParams(t *testing.T) {
	req := JSONRPCRequest{
		ID:     NewStringRequestId("abc"),
		Method: "thread/read",
		Params: json.RawMessage(`{"threadId":"t1"}`),
	}
	assertJSON(t, req, `{"id":"abc","method":"thread/read","params":{"threadId":"t1"}}`)

	var back JSONRPCRequest
	if err := json.Unmarshal([]byte(`{"id":"abc","method":"thread/read","params":{"threadId":"t1"}}`), &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Method != "thread/read" {
		t.Fatalf("method = %q", back.Method)
	}
}

func TestJSONRPCNotificationRoundTrip(t *testing.T) {
	note := JSONRPCNotification{Method: "thread/started", Params: json.RawMessage(`{"threadId":"t1"}`)}
	assertJSON(t, note, `{"method":"thread/started","params":{"threadId":"t1"}}`)

	bare := JSONRPCNotification{Method: "initialized"}
	assertJSON(t, bare, `{"method":"initialized"}`)
}

func TestJSONRPCResponseRoundTrip(t *testing.T) {
	resp := JSONRPCResponse{ID: NewIntegerRequestId(5), Result: json.RawMessage(`{}`)}
	assertJSON(t, resp, `{"id":5,"result":{}}`)
}

func TestJSONRPCErrorRoundTrip(t *testing.T) {
	e := JSONRPCError{
		Error: JSONRPCErrorBody{Code: -32601, Message: "method not found"},
		ID:    NewIntegerRequestId(9),
	}
	assertJSON(t, e, `{"error":{"code":-32601,"message":"method not found"},"id":9}`)

	withData := JSONRPCError{
		Error: JSONRPCErrorBody{Code: 1, Message: "boom", Data: json.RawMessage(`{"k":"v"}`)},
		ID:    NewStringRequestId("x"),
	}
	assertJSON(t, withData, `{"error":{"code":1,"data":{"k":"v"},"message":"boom"},"id":"x"}`)
}

func TestJSONRPCMessageClassification(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want MessageKind
	}{
		{"request", `{"id":1,"method":"thread/start","params":{}}`, MessageKindRequest},
		{"notification", `{"method":"thread/started","params":{}}`, MessageKindNotification},
		{"response", `{"id":1,"result":{}}`, MessageKindResponse},
		{"error", `{"id":1,"error":{"code":-1,"message":"x"}}`, MessageKindError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var msg JSONRPCMessage
			if err := json.Unmarshal([]byte(tc.in), &msg); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if msg.Kind != tc.want {
				t.Fatalf("kind = %d, want %d", msg.Kind, tc.want)
			}
			// Re-marshal should produce structurally-equal JSON.
			out, err := json.Marshal(msg)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !jsonEqual(t, out, []byte(tc.in)) {
				t.Fatalf("re-marshal mismatch:\n got: %s\nwant: %s", out, tc.in)
			}
		})
	}
}

func TestJSONRPCMessageRejectsUnknown(t *testing.T) {
	var msg JSONRPCMessage
	if err := json.Unmarshal([]byte(`{"foo":"bar"}`), &msg); err == nil {
		t.Fatal("expected error for unclassifiable message")
	}
}
