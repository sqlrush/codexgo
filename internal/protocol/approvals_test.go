package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestAskForApprovalUnitVariants(t *testing.T) {
	cases := []struct {
		kind AskForApprovalKind
		want string
	}{
		{AskForApprovalUnlessTrusted, `"untrusted"`},
		{AskForApprovalOnFailure, `"on-failure"`},
		{AskForApprovalOnRequest, `"on-request"`},
		{AskForApprovalNever, `"never"`},
	}
	for _, tc := range cases {
		a := AskForApproval{Kind: tc.kind}
		b, err := json.Marshal(a)
		if err != nil {
			t.Fatalf("marshal %s: %v", tc.kind, err)
		}
		if string(b) != tc.want {
			t.Fatalf("JSON mismatch for %s:\n got: %s\nwant: %s", tc.kind, b, tc.want)
		}
		var got AskForApproval
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", tc.kind, err)
		}
		if !reflect.DeepEqual(got, a) {
			t.Fatalf("round-trip mismatch for %s: %+v != %+v", tc.kind, got, a)
		}
	}
}

func TestAskForApprovalGranular(t *testing.T) {
	a := NewAskForApprovalGranular(GranularApprovalConfig{
		SandboxApproval: true,
		Rules:           false,
		McpElicitations: true,
	})
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"granular":{"sandbox_approval":true,"rules":false,"skill_approval":false,"request_permissions":false,"mcp_elicitations":true}}`
	if string(b) != want {
		t.Fatalf("JSON mismatch:\n got: %s\nwant: %s", b, want)
	}
	var got AskForApproval
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, a) {
		t.Fatalf("round-trip mismatch: %+v != %+v", got, a)
	}
}

func TestGranularApprovalConfigDefaultsOptionalFlags(t *testing.T) {
	// skill_approval and request_permissions are #[serde(default)] in Rust.
	in := []byte(`{"sandbox_approval":true,"rules":false,"mcp_elicitations":true}`)
	var c GranularApprovalConfig
	if err := json.Unmarshal(in, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := GranularApprovalConfig{
		SandboxApproval:    true,
		Rules:              false,
		SkillApproval:      false,
		RequestPermissions: false,
		McpElicitations:    true,
	}
	if c != want {
		t.Fatalf("defaults mismatch: %+v != %+v", c, want)
	}
}

func TestNetworkApprovalProtocolAliases(t *testing.T) {
	for _, v := range []string{"https", "https_connect", "http-connect"} {
		if got := NormalizeNetworkApprovalProtocol(v); got != NetworkApprovalProtocolHTTPS {
			t.Errorf("NormalizeNetworkApprovalProtocol(%q) = %q, want https", v, got)
		}
	}
}

func TestNetworkPolicyDecisionPayload(t *testing.T) {
	proto := NetworkApprovalProtocolHTTPS
	host := "example.com"
	var port uint16 = 443
	p := NetworkPolicyDecisionPayload{
		Decision: NetworkPolicyDecisionAsk,
		Source:   NetworkDecisionSourceDecider,
		Protocol: &proto,
		Host:     &host,
		Port:     &port,
	}
	if !p.IsAskFromDecider() {
		t.Fatalf("expected IsAskFromDecider true")
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got NetworkPolicyDecisionPayload
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, p) {
		t.Fatalf("round-trip mismatch: %+v != %+v", got, p)
	}
}
