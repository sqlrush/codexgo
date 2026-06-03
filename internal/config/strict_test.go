package config

import (
	"strings"
	"testing"
)

func TestUnknownConfigField(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "no unknown", body: "model = \"x\"\n", want: ""},
		{name: "unknown top-level", body: "bogus_key = 1\n", want: "bogus_key"},
		{name: "unknown feature", body: "[features]\nnot_a_real_feature = true\n", want: "features.not_a_real_feature"},
		{
			name: "unknown profile feature",
			body: "[profiles.work.features]\nnot_real = true\n",
			want: "profiles.work.features.not_real",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := ParseTomlValue([]byte(tt.body))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := UnknownConfigField(value); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStrictConfigError(t *testing.T) {
	value, err := ParseTomlValue([]byte("bogus = 1\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := StrictConfigError(value, false); err != nil {
		t.Fatalf("non-strict should not error, got %v", err)
	}
	err = StrictConfigError(value, true)
	if err == nil || !strings.Contains(err.Error(), "unknown configuration field `bogus`") {
		t.Fatalf("want strict error, got %v", err)
	}
}
