package appserverproto

import (
	"encoding/json"
	"testing"
)

func u16Ptr(v uint16) *uint16 { return &v }

func TestCodexErrorInfoV2RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		val  CodexErrorInfoV2
		want string
	}{
		{"unit", CodexErrorInfoV2{Kind: CodexErrorInfoV2ContextWindowExceeded}, `"contextWindowExceeded"`},
		{"unit-other", CodexErrorInfoV2{Kind: CodexErrorInfoV2Other}, `"other"`},
		{
			"http-with-status",
			CodexErrorInfoV2{Kind: CodexErrorInfoV2HTTPConnectionFailed, HTTPStatusCode: u16Ptr(503)},
			`{"httpConnectionFailed":{"httpStatusCode":503}}`,
		},
		{
			"http-null-status",
			CodexErrorInfoV2{Kind: CodexErrorInfoV2ResponseStreamDisconnected},
			`{"responseStreamDisconnected":{"httpStatusCode":null}}`,
		},
		{
			"turn-kind",
			func() CodexErrorInfoV2 {
				tk := NonSteerableTurnKindCompact
				return CodexErrorInfoV2{Kind: CodexErrorInfoV2ActiveTurnNotSteerable, TurnKind: &tk}
			}(),
			`{"activeTurnNotSteerable":{"turnKind":"compact"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.val)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !jsonEqual(t, b, []byte(tc.want)) {
				t.Fatalf("marshal = %s, want %s", b, tc.want)
			}
			var back CodexErrorInfoV2
			if err := json.Unmarshal(b, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			reb, err := json.Marshal(back)
			if err != nil {
				t.Fatalf("re-marshal: %v", err)
			}
			if !jsonEqual(t, reb, []byte(tc.want)) {
				t.Fatalf("round-trip = %s, want %s", reb, tc.want)
			}
		})
	}
}

func TestAskForApprovalV2RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		val  AskForApprovalV2
		want string
	}{
		{"untrusted", AskForApprovalV2{Kind: AskForApprovalV2UnlessTrusted}, `"untrusted"`},
		{"on-failure", AskForApprovalV2{Kind: AskForApprovalV2OnFailure}, `"on-failure"`},
		{"on-request", AskForApprovalV2{Kind: AskForApprovalV2OnRequest}, `"on-request"`},
		{"never", AskForApprovalV2{Kind: AskForApprovalV2Never}, `"never"`},
		{
			"granular",
			NewAskForApprovalV2Granular(GranularApprovalConfigV2{
				SandboxApproval: true,
				Rules:           true,
				McpElicitations: true,
			}),
			`{"granular":{"sandbox_approval":true,"rules":true,"skill_approval":false,"request_permissions":false,"mcp_elicitations":true}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.val)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !jsonEqual(t, b, []byte(tc.want)) {
				t.Fatalf("marshal = %s, want %s", b, tc.want)
			}
			var back AskForApprovalV2
			if err := json.Unmarshal(b, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			reb, err := json.Marshal(back)
			if err != nil {
				t.Fatalf("re-marshal: %v", err)
			}
			if !jsonEqual(t, reb, []byte(tc.want)) {
				t.Fatalf("round-trip = %s, want %s", reb, tc.want)
			}
		})
	}
}

func TestApprovalsReviewerV2Normalize(t *testing.T) {
	cases := map[string]ApprovalsReviewerV2{
		"user":              ApprovalsReviewerV2User,
		"guardian_subagent": ApprovalsReviewerV2AutoReview,
		"auto_review":       ApprovalsReviewerV2AutoReview,
	}
	for in, want := range cases {
		if got := NormalizeApprovalsReviewerV2(in); got != want {
			t.Fatalf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSandboxModeV2Values(t *testing.T) {
	cases := map[SandboxModeV2]string{
		SandboxModeV2ReadOnly:         `"read-only"`,
		SandboxModeV2WorkspaceWrite:   `"workspace-write"`,
		SandboxModeV2DangerFullAccess: `"danger-full-access"`,
	}
	for v, want := range cases {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(b) != want {
			t.Fatalf("marshal = %s, want %s", b, want)
		}
	}
}
