package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSandboxPolicyKnownJSON(t *testing.T) {
	cases := []struct {
		name   string
		policy SandboxPolicy
		want   string
	}{
		{
			name:   "danger full access",
			policy: NewDangerFullAccessSandboxPolicy(),
			want:   `{"type":"danger-full-access"}`,
		},
		{
			name:   "read only default network",
			policy: NewReadOnlySandboxPolicy(false),
			want:   `{"type":"read-only"}`,
		},
		{
			name:   "read only with network",
			policy: NewReadOnlySandboxPolicy(true),
			want:   `{"network_access":true,"type":"read-only"}`,
		},
		{
			name: "external sandbox default network",
			policy: SandboxPolicy{
				Type:          SandboxPolicyTypeExternalSandbox,
				NetworkAccess: NetworkAccessRestricted,
			},
			want: `{"network_access":"restricted","type":"external-sandbox"}`,
		},
		{
			name: "workspace write minimal",
			policy: SandboxPolicy{
				Type: SandboxPolicyTypeWorkspaceWrite,
			},
			want: `{"exclude_slash_tmp":false,"exclude_tmpdir_env_var":false,"network_access":false,"type":"workspace-write"}`,
		},
		{
			name: "workspace write with roots",
			policy: SandboxPolicy{
				Type:              SandboxPolicyTypeWorkspaceWrite,
				WritableRoots:     []AbsolutePath{"/work", "/tmp/x"},
				NetworkAccessBool: true,
			},
			want: `{"exclude_slash_tmp":false,"exclude_tmpdir_env_var":false,"network_access":true,"type":"workspace-write","writable_roots":["/work","/tmp/x"]}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.policy)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			// json.Marshal of a map sorts keys, giving canonical order.
			if string(b) != tc.want {
				t.Fatalf("JSON mismatch:\n got: %s\nwant: %s", b, tc.want)
			}

			var got SandboxPolicy
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(got, tc.policy) {
				t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, tc.policy)
			}
		})
	}
}

func TestSandboxPolicyExternalSandboxEnabled(t *testing.T) {
	in := []byte(`{"type":"external-sandbox","network_access":"enabled"}`)
	var p SandboxPolicy
	if err := json.Unmarshal(in, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.NetworkAccess != NetworkAccessEnabled {
		t.Fatalf("expected enabled, got %q", p.NetworkAccess)
	}
}
