package cli

import (
	"reflect"
	"testing"
)

func TestParseOverrideEntry(t *testing.T) {
	tests := []struct {
		name      string
		entry     string
		wantKey   string
		wantValue any
		wantErr   bool
	}{
		{name: "string value", entry: "model=gpt-5", wantKey: "model", wantValue: "gpt-5"},
		{name: "bool value", entry: "features.shell_tool=true", wantKey: "features.shell_tool", wantValue: true},
		{name: "int value", entry: "limit=5", wantKey: "limit", wantValue: int64(5)},
		{name: "quoted string", entry: `name="hi there"`, wantKey: "name", wantValue: "hi there"},
		{name: "array value", entry: "list=[1, 2, 3]", wantKey: "list", wantValue: []any{int64(1), int64(2), int64(3)}},
		{name: "empty value", entry: "key=", wantKey: "key", wantValue: ""},
		{name: "value with equals", entry: "url=http://x?a=b", wantKey: "url", wantValue: "http://x?a=b"},
		{name: "missing equals", entry: "noequals", wantErr: true},
		{name: "empty key", entry: "=value", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, value, err := parseOverrideEntry(tt.entry)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if key != tt.wantKey {
				t.Errorf("key = %q, want %q", key, tt.wantKey)
			}
			if !reflect.DeepEqual(value, tt.wantValue) {
				t.Errorf("value = %#v, want %#v", value, tt.wantValue)
			}
		})
	}
}

func TestConfigOverridesPrependAppend(t *testing.T) {
	root := ConfigOverrides{}.Append("a=1", "b=2")
	sub := ConfigOverrides{}.Append("c=3")
	merged := sub.Prepend(root)
	got := merged.Raw()
	want := []string{"a=1", "b=2", "c=3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("merged = %v, want %v", got, want)
	}

	// The originals must not be mutated (immutability).
	if !reflect.DeepEqual(root.Raw(), []string{"a=1", "b=2"}) {
		t.Errorf("root mutated: %v", root.Raw())
	}
	if !reflect.DeepEqual(sub.Raw(), []string{"c=3"}) {
		t.Errorf("sub mutated: %v", sub.Raw())
	}
}

func TestConfigOverridesParse(t *testing.T) {
	o := ConfigOverrides{}.Append("model=gpt-5", "features.apps=false")
	parsed, err := o.Parse()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("got %d overrides, want 2", len(parsed))
	}
	if parsed[0].Path != "model" || parsed[0].Value != "gpt-5" {
		t.Errorf("override[0] = %+v", parsed[0])
	}
	if parsed[1].Path != "features.apps" || parsed[1].Value != false {
		t.Errorf("override[1] = %+v", parsed[1])
	}
}
