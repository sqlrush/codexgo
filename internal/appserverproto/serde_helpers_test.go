package appserverproto

import (
	"encoding/json"
	"testing"
)

func strPtr(s string) *string { return &s }

func TestEmptyPathAsNone(t *testing.T) {
	cases := []struct {
		name string
		in   *string
		want bool // want nil result
	}{
		{"nil", nil, true},
		{"empty", strPtr(""), true},
		{"value", strPtr("/tmp/x"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EmptyPathAsNone(tc.in)
			if (got == nil) != tc.want {
				t.Fatalf("EmptyPathAsNone(%v) nil=%v, want nil=%v", tc.in, got == nil, tc.want)
			}
		})
	}
}

// doubleOptHolder exercises a DoubleOption embedded in a struct as a value
// field. A value field is required (not a pointer): Go's encoding/json sets a
// pointer field to nil for `null` rather than invoking UnmarshalJSON, which
// would collapse the absent/null distinction. With a value field, absence
// leaves the zero value (Present=false) while null and value both invoke
// UnmarshalJSON.
type doubleOptHolder struct {
	Field DoubleOption[string] `json:"field"`
}

func TestDoubleOptionStates(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		present bool
		set     bool
		value   string
	}{
		{"absent", `{}`, false, false, ""},
		{"null", `{"field":null}`, true, false, ""},
		{"value", `{"field":"hi"}`, true, true, "hi"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var h doubleOptHolder
			if err := json.Unmarshal([]byte(tc.in), &h); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if h.Field.Present != tc.present || h.Field.Set != tc.set || h.Field.Value != tc.value {
				t.Fatalf("got %+v, want present=%v set=%v value=%q", h.Field, tc.present, tc.set, tc.value)
			}
		})
	}
}

func TestDoubleOptionMarshal(t *testing.T) {
	val := NewDoubleOptionValue("x")
	b, err := json.Marshal(val)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `"x"` {
		t.Fatalf("value marshal = %s", b)
	}

	null := NewDoubleOptionNull[string]()
	b, err = json.Marshal(null)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != "null" {
		t.Fatalf("null marshal = %s", b)
	}
}
