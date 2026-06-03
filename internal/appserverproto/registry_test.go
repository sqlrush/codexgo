package appserverproto

import (
	"encoding/json"
	"testing"
)

// fooParams and fooResult are throwaway types used to exercise the registry
// without depending on area-agent types.
type fooParams struct {
	Name string `json:"name"`
}

type fooResult struct {
	OK bool `json:"ok"`
}

func TestRegisterAndLookup(t *testing.T) {
	const method = "test/registryRoundTrip"
	Register(MethodSpec{
		Method:    method,
		NewParams: func() any { return new(fooParams) },
		NewResult: func() any { return new(fooResult) },
	})

	spec, ok := Lookup(method)
	if !ok {
		t.Fatalf("Lookup(%q) not found", method)
	}
	if spec.Method != method {
		t.Fatalf("spec.Method = %q", spec.Method)
	}

	if _, ok := spec.NewParams().(*fooParams); !ok {
		t.Fatalf("NewParams did not return *fooParams")
	}
	if _, ok := spec.NewResult().(*fooResult); !ok {
		t.Fatalf("NewResult did not return *fooResult")
	}
}

func TestLookupMissing(t *testing.T) {
	if _, ok := Lookup("test/definitelyNotRegistered"); ok {
		t.Fatal("expected missing method to return ok=false")
	}
}

func TestDecodeClientRequestParams(t *testing.T) {
	const method = "test/decodeParams"
	Register(MethodSpec{
		Method:    method,
		NewParams: func() any { return new(fooParams) },
		NewResult: func() any { return new(fooResult) },
	})

	req := JSONRPCRequest{
		ID:     NewIntegerRequestId(1),
		Method: method,
		Params: json.RawMessage(`{"name":"hello"}`),
	}
	got, err := DecodeClientRequestParams(req)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	p, ok := got.(*fooParams)
	if !ok {
		t.Fatalf("decoded type = %T", got)
	}
	if p.Name != "hello" {
		t.Fatalf("Name = %q", p.Name)
	}
}

func TestDecodeClientRequestParamsUnknownMethod(t *testing.T) {
	req := JSONRPCRequest{ID: NewIntegerRequestId(1), Method: "test/unknownDecode"}
	if _, err := DecodeClientRequestParams(req); err == nil {
		t.Fatal("expected error for unknown method")
	}
}

func TestDecodeClientRequestParamsAbsentParamsDecodeNull(t *testing.T) {
	const method = "test/optionUnitParams"
	// Params type is *json.RawMessage so a `null` decodes cleanly.
	Register(MethodSpec{
		Method:    method,
		NewParams: func() any { return new(json.RawMessage) },
		NewResult: func() any { return new(fooResult) },
	})
	req := JSONRPCRequest{ID: NewIntegerRequestId(1), Method: method}
	got, err := DecodeClientRequestParams(req)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	raw, ok := got.(*json.RawMessage)
	if !ok {
		t.Fatalf("decoded type = %T", got)
	}
	if string(*raw) != "null" {
		t.Fatalf("expected null params, got %s", *raw)
	}
}

func TestRegisterNotificationAndDecode(t *testing.T) {
	const method = "test/note"
	RegisterNotification(NotificationSpec{
		Method:    method,
		NewParams: func() any { return new(fooParams) },
	})
	note := JSONRPCNotification{Method: method, Params: json.RawMessage(`{"name":"n"}`)}
	got, err := DecodeServerNotificationParams(note)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.(*fooParams).Name != "n" {
		t.Fatalf("name = %q", got.(*fooParams).Name)
	}
}

func TestRegisterServerRequestAndDecode(t *testing.T) {
	const method = "test/serverReq"
	RegisterServerRequest(ServerRequestSpec{
		Method:    method,
		NewParams: func() any { return new(fooParams) },
		NewResult: func() any { return new(fooResult) },
	})
	req := JSONRPCRequest{ID: NewIntegerRequestId(2), Method: method, Params: json.RawMessage(`{"name":"sr"}`)}
	got, err := DecodeServerRequestParams(req)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.(*fooParams).Name != "sr" {
		t.Fatalf("name = %q", got.(*fooParams).Name)
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	const method = "test/dupPanic"
	Register(MethodSpec{
		Method:    method,
		NewParams: func() any { return new(fooParams) },
		NewResult: func() any { return new(fooResult) },
	})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	Register(MethodSpec{
		Method:    method,
		NewParams: func() any { return new(fooParams) },
		NewResult: func() any { return new(fooResult) },
	})
}

func TestRegisterValidationPanics(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"empty-method", func() {
			Register(MethodSpec{NewParams: func() any { return nil }, NewResult: func() any { return nil }})
		}},
		{"nil-params", func() { Register(MethodSpec{Method: "test/x", NewResult: func() any { return nil }}) }},
		{"nil-note-params", func() { RegisterNotification(NotificationSpec{Method: "test/y"}) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatal("expected panic")
				}
			}()
			tc.fn()
		})
	}
}
