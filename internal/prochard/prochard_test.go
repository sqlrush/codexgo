package prochard

import (
	"reflect"
	"testing"
)

func TestEnvKeysWithPrefix(t *testing.T) {
	// non-UTF-8 bytes used to mirror the upstream test that ensures keys with
	// invalid encodings are still matched on their raw bytes.
	nonUTF8LD := "LD_\xf0"

	tests := []struct {
		name    string
		entries []string
		prefix  string
		want    []string
	}{
		{
			name:    "empty",
			entries: nil,
			prefix:  ldPrefix,
			want:    []string{},
		},
		{
			name: "filters only matching keys",
			entries: []string{
				"PATH=/usr/bin",
				"LD_TEST=1",
				"DYLD_FOO=bar",
			},
			prefix: ldPrefix,
			want:   []string{"LD_TEST"},
		},
		{
			name: "matches dyld prefix",
			entries: []string{
				"PATH=/usr/bin",
				"LD_TEST=1",
				"DYLD_FOO=bar",
				"DYLD_INSERT_LIBRARIES=/tmp/x.dylib",
			},
			prefix: dyldPrefix,
			want:   []string{"DYLD_FOO", "DYLD_INSERT_LIBRARIES"},
		},
		{
			name: "non-utf8 key with prefix is retained",
			entries: []string{
				"R\xd6DBURK=\xf0\x9f\x92\xa9",
				nonUTF8LD + "=\xf0\x9f\x92\xa9",
			},
			prefix: ldPrefix,
			want:   []string{nonUTF8LD},
		},
		{
			name: "exact prefix as full key matches",
			entries: []string{
				"LD_=value",
				"LD=value",
			},
			prefix: ldPrefix,
			want:   []string{"LD_"},
		},
		{
			name: "empty value preserved as key",
			entries: []string{
				"LD_PRELOAD=",
			},
			prefix: ldPrefix,
			want:   []string{"LD_PRELOAD"},
		},
		{
			name: "bare key without equals is treated as key",
			entries: []string{
				"LD_BARE",
				"OTHER",
			},
			prefix: ldPrefix,
			want:   []string{"LD_BARE"},
		},
		{
			name: "value containing equals does not affect key",
			entries: []string{
				"LD_LIBRARY_PATH=/a=b:/c",
			},
			prefix: ldPrefix,
			want:   []string{"LD_LIBRARY_PATH"},
		},
		{
			name: "prefix is case sensitive",
			entries: []string{
				"ld_lower=1",
				"LD_UPPER=1",
			},
			prefix: ldPrefix,
			want:   []string{"LD_UPPER"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := envKeysWithPrefix(tt.entries, tt.prefix)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("envKeysWithPrefix(%q, %q) = %q, want %q", tt.entries, tt.prefix, got, tt.want)
			}
		})
	}
}

func TestEnvKeysWithPrefixDoesNotMutateInput(t *testing.T) {
	entries := []string{"LD_A=1", "PATH=/usr/bin", "LD_B=2"}
	snapshot := append([]string(nil), entries...)

	_ = envKeysWithPrefix(entries, ldPrefix)

	if !reflect.DeepEqual(entries, snapshot) {
		t.Fatalf("input was mutated: got %q, want %q", entries, snapshot)
	}
}

func TestExitCodesAreStable(t *testing.T) {
	tests := []struct {
		name string
		got  int
		want int
	}{
		{"prctl", PrctlFailedExitCode, 5},
		{"ptrace_deny_attach", PtraceDenyAttachFailedExitCode, 6},
		{"set_rlimit_core", SetRlimitCoreFailedExitCode, 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s exit code = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}
}
