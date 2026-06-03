package codemode

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

// evalJS runs deterministic JavaScript in a fresh goja runtime and returns the
// resulting value, mirroring how an exec cell evaluates model-authored JS.
func evalJS(t *testing.T, rt *goja.Runtime, src string) goja.Value {
	t.Helper()
	value, err := rt.RunString(src)
	if err != nil {
		t.Fatalf("RunString(%q): %v", src, err)
	}
	return value
}

// TestSerializeOutputTextPrimitives verifies that text()/notify() stringify
// primitives directly, mirroring serialize_output_text.
func TestSerializeOutputTextPrimitives(t *testing.T) {
	rt := goja.New()
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"string", `"hello"`, "hello"},
		{"int", `42`, "42"},
		{"float", `3.5`, "3.5"},
		{"bool-true", `true`, "true"},
		{"null", `null`, "null"},
		{"undefined", `undefined`, "undefined"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := serializeOutputText(rt, evalJS(t, rt, tc.src))
			if err != nil {
				t.Fatalf("serializeOutputText: %v", err)
			}
			if got != tc.want {
				t.Fatalf("serializeOutputText(%s) = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// TestSerializeOutputTextNilValue verifies the nil-value guard returns
// "undefined".
func TestSerializeOutputTextNilValue(t *testing.T) {
	rt := goja.New()
	got, err := serializeOutputText(rt, nil)
	if err != nil {
		t.Fatalf("serializeOutputText(nil): %v", err)
	}
	if got != "undefined" {
		t.Fatalf("serializeOutputText(nil) = %q, want \"undefined\"", got)
	}
}

// TestSerializeOutputTextNonPrimitive verifies objects/arrays are JSON.stringify'd
// and a bare function (JSON.stringify -> undefined) falls back to its string form.
func TestSerializeOutputTextNonPrimitive(t *testing.T) {
	rt := goja.New()

	got, err := serializeOutputText(rt, evalJS(t, rt, `({a:1,b:[2,3]})`))
	if err != nil {
		t.Fatalf("serializeOutputText(object): %v", err)
	}
	if got != `{"a":1,"b":[2,3]}` {
		t.Fatalf("object stringify = %q", got)
	}

	got, err = serializeOutputText(rt, evalJS(t, rt, `(function foo(){})`))
	if err != nil {
		t.Fatalf("serializeOutputText(function): %v", err)
	}
	// JSON.stringify(function) is undefined, so the lossy string form is used.
	if !strings.Contains(got, "function") {
		t.Fatalf("function fallback = %q, want a function string form", got)
	}
}

// TestJSONStringify covers the (string, ok, err) contract: a value yields a
// string with ok=true, and a bare function yields ok=false (undefined).
func TestJSONStringify(t *testing.T) {
	rt := goja.New()

	s, ok, err := jsonStringify(rt, evalJS(t, rt, `({x:1})`))
	if err != nil || !ok || s != `{"x":1}` {
		t.Fatalf("jsonStringify(object) = (%q,%v,%v), want ({\"x\":1},true,nil)", s, ok, err)
	}

	_, ok, err = jsonStringify(rt, evalJS(t, rt, `(function(){})`))
	if err != nil || ok {
		t.Fatalf("jsonStringify(function) ok=%v err=%v, want ok=false err=nil", ok, err)
	}
}

// TestJSValueToJSONRoundTrip verifies a JS value is decoded into a Go value, and
// that round-tripping back through jsonToJSValue and JSON.stringify is stable.
func TestJSValueToJSONRoundTrip(t *testing.T) {
	rt := goja.New()
	decoded, ok, err := jsValueToJSON(rt, evalJS(t, rt, `({name:"a",count:2,nested:{ok:true},list:[1,2]})`))
	if err != nil || !ok {
		t.Fatalf("jsValueToJSON ok=%v err=%v", ok, err)
	}
	want := map[string]any{
		"name":   "a",
		"count":  float64(2),
		"nested": map[string]any{"ok": true},
		"list":   []any{float64(1), float64(2)},
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decoded = %#v, want %#v", decoded, want)
	}

	// Round-trip the decoded value back into JS and stringify it.
	jsValue, err := jsonToJSValue(rt, decoded)
	if err != nil {
		t.Fatalf("jsonToJSValue: %v", err)
	}
	s, ok, err := jsonStringify(rt, jsValue)
	if err != nil || !ok {
		t.Fatalf("re-stringify ok=%v err=%v", ok, err)
	}
	if !strings.Contains(s, `"name":"a"`) || !strings.Contains(s, `"count":2`) {
		t.Fatalf("round-trip stringify = %q", s)
	}
}

// TestValueToErrorText verifies a thrown JS error surfaces a human-readable
// message (preferring .stack), mirroring value_to_error_text and the error path
// an exec cell reports.
func TestValueToErrorText(t *testing.T) {
	rt := goja.New()
	_, err := rt.RunString(`throw new Error("boom")`)
	if err == nil {
		t.Fatal("expected a thrown error")
	}
	text := valueToErrorText(rt, err)
	if !strings.Contains(text, "boom") {
		t.Fatalf("valueToErrorText = %q, want it to contain \"boom\"", text)
	}
}

// detailString is a small helper to build a *string for image detail overrides.
func detailString(s string) *string { return &s }

// TestNormalizeOutputImageValid covers the valid image() argument shapes:
// a bare URL, an object with image_url+detail, and a detail override.
func TestNormalizeOutputImageValid(t *testing.T) {
	rt := goja.New()
	httpURL := "https://example.com/cat.png"
	dataURL := "data:image/png;base64,AAAA"

	cases := []struct {
		name       string
		src        string
		override   *string
		wantURL    string
		wantDetail ImageDetail
	}{
		{"bare-https-url-default-detail", `"` + httpURL + `"`, nil, httpURL, DefaultImageDetail},
		{"bare-data-url-default-detail", `"` + dataURL + `"`, nil, dataURL, DefaultImageDetail},
		{"object-with-detail", `({image_url:"` + httpURL + `",detail:"low"})`, nil, httpURL, ImageDetailLow},
		{"override-detail-wins", `({image_url:"` + httpURL + `",detail:"low"})`, detailString("high"), httpURL, ImageDetailHigh},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item, err := normalizeOutputImage(rt, evalJS(t, rt, tc.src), tc.override)
			if err != nil {
				t.Fatalf("normalizeOutputImage: %v", err)
			}
			if item.Kind != ContentItemInputImage {
				t.Fatalf("kind = %v, want InputImage", item.Kind)
			}
			if item.ImageURL != tc.wantURL {
				t.Fatalf("image url = %q, want %q", item.ImageURL, tc.wantURL)
			}
			if item.Detail == nil || *item.Detail != tc.wantDetail {
				t.Fatalf("detail = %v, want %v", item.Detail, tc.wantDetail)
			}
		})
	}
}

// TestNormalizeOutputImageMCPBlock verifies an MCP image content block is
// converted into a data: URL with a default mime type, and honors the codex
// image-detail meta key.
func TestNormalizeOutputImageMCPBlock(t *testing.T) {
	rt := goja.New()
	src := `({
		type: "image",
		data: "QUJD",
		mimeType: "image/jpeg",
		_meta: { "codex/imageDetail": "original" }
	})`
	item, err := normalizeOutputImage(rt, evalJS(t, rt, src), nil)
	if err != nil {
		t.Fatalf("normalizeOutputImage(mcp): %v", err)
	}
	if item.ImageURL != "data:image/jpeg;base64,QUJD" {
		t.Fatalf("image url = %q", item.ImageURL)
	}
	if item.Detail == nil || *item.Detail != ImageDetailOriginal {
		t.Fatalf("detail = %v, want original", item.Detail)
	}
}

// TestNormalizeOutputImageErrors covers the rejection paths: empty/missing URL,
// non-http(s)/data URL, invalid detail, and a non-image MCP block.
func TestNormalizeOutputImageErrors(t *testing.T) {
	rt := goja.New()
	cases := []struct {
		name        string
		src         string
		override    *string
		wantErrPart string
	}{
		{"empty-string", `""`, nil, imageHelperExpectsMessage},
		{"non-url-scheme", `"ftp://example.com/x.png"`, nil, "http(s) or data URL"},
		{"invalid-detail", `({image_url:"https://x/y.png",detail:"huge"})`, nil, "image detail must be one of"},
		{"override-invalid-detail", `"https://x/y.png"`, detailString("huge"), "image detail must be one of"},
		{"non-image-mcp-block", `({type:"text",text:"hi"})`, nil, "image only accepts MCP image blocks"},
		{"array-rejected", `[1,2,3]`, nil, imageHelperExpectsMessage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeOutputImage(rt, evalJS(t, rt, tc.src), tc.override)
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErrPart) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tc.wantErrPart)
			}
		})
	}
}
