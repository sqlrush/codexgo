package memories

import (
	"encoding/json"
	"testing"
)

func TestSearchMatchModeMarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		mode SearchMatchMode
		want string
	}{
		{"any", AnyMode(), `{"type":"any"}`},
		{"all on same line", AllOnSameLineMode(), `{"type":"all_on_same_line"}`},
		{"all within lines", AllWithinLinesMode(3), `{"type":"all_within_lines","line_count":3}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.mode)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("marshal = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestSearchMatchModeUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want SearchMatchMode
	}{
		{"any", `{"type":"any"}`, AnyMode()},
		{"all on same line", `{"type":"all_on_same_line"}`, AllOnSameLineMode()},
		{"all within lines", `{"type":"all_within_lines","line_count":5}`, AllWithinLinesMode(5)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got SearchMatchMode
			if err := json.Unmarshal([]byte(tc.in), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got != tc.want {
				t.Fatalf("unmarshal = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestSearchMatchModeUnmarshalUnknown(t *testing.T) {
	var got SearchMatchMode
	if err := json.Unmarshal([]byte(`{"type":"bogus"}`), &got); err == nil {
		t.Fatal("expected error for unknown match mode")
	}
}

func TestAddAdHocNoteResponseMarshalsEmptyObject(t *testing.T) {
	b, err := json.Marshal(AddAdHocNoteResponse{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != "{}" {
		t.Fatalf("AddAdHocNoteResponse = %s, want {}", b)
	}
}
