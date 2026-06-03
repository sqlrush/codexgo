package codemode

import (
	"encoding/json"
	"testing"
)

// strPtr returns a pointer to s.
func strPtr(s string) *string { return &s }

// TestRuntimeResponseMarshalVariants verifies the externally-tagged JSON shape
// codex produces for each RuntimeResponse variant, including the always-present
// content_items array and the Result-only error_text field.
func TestRuntimeResponseMarshalVariants(t *testing.T) {
	cell := NewCellID("1")

	cases := []struct {
		name string
		resp RuntimeResponse
		want string
	}{
		{
			name: "result-success-null-error",
			resp: newResultResponse(cell, []FunctionCallOutputContentItem{NewTextItem("hi")}, nil),
			want: `{"Result":{"cell_id":"1","content_items":[{"type":"input_text","text":"hi"}],"error_text":null}}`,
		},
		{
			name: "result-with-error",
			resp: newResultResponse(cell, nil, strPtr("boom")),
			want: `{"Result":{"cell_id":"1","content_items":[],"error_text":"boom"}}`,
		},
		{
			name: "yielded-empty-items",
			resp: newYieldedResponse(cell, nil),
			want: `{"Yielded":{"cell_id":"1","content_items":[]}}`,
		},
		{
			name: "terminated-with-items",
			resp: newTerminatedResponse(cell, []FunctionCallOutputContentItem{NewTextItem("stop")}),
			want: `{"Terminated":{"cell_id":"1","content_items":[{"type":"input_text","text":"stop"}]}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.resp)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(b) != tc.want {
				t.Fatalf("marshal =\n %s\nwant\n %s", b, tc.want)
			}
		})
	}
}

// TestRuntimeResponseMarshalUnknownKindErrors verifies an out-of-range kind is
// rejected rather than producing garbage JSON.
func TestRuntimeResponseMarshalUnknownKindErrors(t *testing.T) {
	resp := RuntimeResponse{Kind: RuntimeResponseKind(99), CellID: NewCellID("x")}
	if _, err := json.Marshal(resp); err == nil {
		t.Fatal("expected error marshaling unknown runtime response kind")
	}
}

// TestWaitOutcomeInto verifies the From<WaitOutcome> for RuntimeResponse bridge.
func TestWaitOutcomeInto(t *testing.T) {
	resp := newResultResponse(NewCellID("7"), nil, nil)
	outcome := WaitOutcome{Kind: WaitOutcomeLiveCell, Response: resp}
	got := outcome.Into()
	if got.Kind != RuntimeResponseResult || got.CellID.AsStr() != "7" {
		t.Fatalf("Into() = %+v, want the wrapped result response", got)
	}
}

// TestCellIDRoundTrip verifies the bare-string JSON encoding and accessors.
func TestCellIDRoundTrip(t *testing.T) {
	c := NewCellID("cell-42")
	if c.String() != "cell-42" || c.AsStr() != "cell-42" {
		t.Fatalf("accessors = (%q,%q)", c.String(), c.AsStr())
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `"cell-42"` {
		t.Fatalf("marshal = %s, want \"cell-42\"", b)
	}
	var got CellID
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != c {
		t.Fatalf("round-trip = %v, want %v", got, c)
	}
}

// TestCellIDIsComparableMapKey verifies CellID works as a map key (cell state is
// keyed by CellID across exec/wait calls in a session).
func TestCellIDIsComparableMapKey(t *testing.T) {
	state := map[CellID]string{}
	a := NewCellID("a")
	state[a] = "first"
	state[NewCellID("a")] = "second" // same logical id overwrites
	state[NewCellID("b")] = "other"

	if len(state) != 2 {
		t.Fatalf("map has %d keys, want 2", len(state))
	}
	if state[a] != "second" {
		t.Fatalf("state[a] = %q, want second (same id overwrites)", state[a])
	}
}

// TestFunctionCallOutputContentItemJSON verifies the internally-tagged content
// item shapes for text and image (with and without detail).
func TestFunctionCallOutputContentItemJSON(t *testing.T) {
	detail := ImageDetailLow
	cases := []struct {
		name string
		item FunctionCallOutputContentItem
		want string
	}{
		{
			name: "text",
			item: NewTextItem("hello"),
			want: `{"type":"input_text","text":"hello"}`,
		},
		{
			name: "image-with-detail",
			item: NewImageItem("https://x/y.png", &detail),
			want: `{"type":"input_image","image_url":"https://x/y.png","detail":"low"}`,
		},
		{
			name: "image-omitted-detail",
			item: NewImageItem("https://x/y.png", nil),
			want: `{"type":"input_image","image_url":"https://x/y.png"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.item)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(b) != tc.want {
				t.Fatalf("marshal = %s, want %s", b, tc.want)
			}
			var got FunctionCallOutputContentItem
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.Kind != tc.item.Kind || got.Text != tc.item.Text || got.ImageURL != tc.item.ImageURL {
				t.Fatalf("round-trip = %+v, want %+v", got, tc.item)
			}
		})
	}
}

// TestImageDetailJSON verifies the lowercase round-trip and parse helper.
func TestImageDetailJSON(t *testing.T) {
	cases := []struct {
		detail ImageDetail
		want   string
	}{
		{ImageDetailAuto, `"auto"`},
		{ImageDetailLow, `"low"`},
		{ImageDetailHigh, `"high"`},
		{ImageDetailOriginal, `"original"`},
	}
	for _, tc := range cases {
		b, err := json.Marshal(tc.detail)
		if err != nil {
			t.Fatalf("marshal %v: %v", tc.detail, err)
		}
		if string(b) != tc.want {
			t.Fatalf("marshal %v = %s, want %s", tc.detail, b, tc.want)
		}
		var got ImageDetail
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", b, err)
		}
		if got != tc.detail {
			t.Fatalf("round-trip %s = %v, want %v", b, got, tc.detail)
		}
	}

	if _, ok := ParseImageDetail("bogus"); ok {
		t.Fatal("ParseImageDetail accepted an invalid value")
	}
	if got, ok := ParseImageDetail("high"); !ok || got != ImageDetailHigh {
		t.Fatalf("ParseImageDetail(high) = (%v,%v)", got, ok)
	}
	if DefaultImageDetail != ImageDetailHigh {
		t.Fatalf("DefaultImageDetail = %v, want high", DefaultImageDetail)
	}
}
