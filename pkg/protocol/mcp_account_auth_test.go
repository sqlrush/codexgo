package protocol

import (
	"encoding/json"
	"testing"
)

func strPtr(s string) *string { return &s }

func TestKnownPlanWireValues(t *testing.T) {
	cases := []struct {
		plan KnownPlan
		want string
	}{
		{KnownPlanFree, `"free"`},
		{KnownPlanProLite, `"prolite"`},
		{KnownPlanSelfServeBusinessUsageBased, `"self_serve_business_usage_based"`},
		{KnownPlanEnterpriseCbpUsageBased, `"enterprise_cbp_usage_based"`},
		{KnownPlanEnterprise, `"enterprise"`},
		{KnownPlanEdu, `"edu"`},
	}
	for _, tc := range cases {
		got, err := json.Marshal(tc.plan)
		if err != nil {
			t.Fatalf("marshal %v: %v", tc.plan, err)
		}
		if string(got) != tc.want {
			t.Errorf("KnownPlan %v: got %s want %s", tc.plan, got, tc.want)
		}
		var back KnownPlan
		if err := json.Unmarshal([]byte(tc.want), &back); err != nil {
			t.Fatalf("unmarshal %s: %v", tc.want, err)
		}
		if back != tc.plan {
			t.Errorf("round-trip %s: got %v want %v", tc.want, back, tc.plan)
		}
	}
}

func TestKnownPlanAliases(t *testing.T) {
	var p KnownPlan
	if err := json.Unmarshal([]byte(`"hc"`), &p); err != nil {
		t.Fatalf("hc: %v", err)
	}
	if p != KnownPlanEnterprise {
		t.Errorf(`alias "hc": got %v want enterprise`, p)
	}
	if err := json.Unmarshal([]byte(`"education"`), &p); err != nil {
		t.Fatalf("education: %v", err)
	}
	if p != KnownPlanEdu {
		t.Errorf(`alias "education": got %v want edu`, p)
	}
	if err := json.Unmarshal([]byte(`"mystery"`), &p); err == nil {
		t.Errorf("expected error for unknown KnownPlan value")
	}
}

func TestAuthPlanTypeUntagged(t *testing.T) {
	// Known arm.
	var known AuthPlanType
	if err := json.Unmarshal([]byte(`"team"`), &known); err != nil {
		t.Fatalf("team: %v", err)
	}
	if known.Kind != AuthPlanTypeKnown || known.Known != KnownPlanTeam {
		t.Errorf("team: got %+v", known)
	}
	// Alias resolves to Known.
	var hc AuthPlanType
	if err := json.Unmarshal([]byte(`"hc"`), &hc); err != nil {
		t.Fatalf("hc: %v", err)
	}
	if hc.Kind != AuthPlanTypeKnown || hc.Known != KnownPlanEnterprise {
		t.Errorf("hc: got %+v", hc)
	}
	// Unknown arm.
	var unknown AuthPlanType
	if err := json.Unmarshal([]byte(`"mystery-tier"`), &unknown); err != nil {
		t.Fatalf("mystery: %v", err)
	}
	if unknown.Kind != AuthPlanTypeUnknown || unknown.Unknown != "mystery-tier" {
		t.Errorf("mystery: got %+v", unknown)
	}
	// Marshal round-trips both arms to bare strings.
	for _, tc := range []struct {
		in   AuthPlanType
		want string
	}{
		{KnownAuthPlanType(KnownPlanPro), `"pro"`},
		{UnknownAuthPlanType("mystery-tier"), `"mystery-tier"`},
	} {
		got, err := json.Marshal(tc.in)
		if err != nil {
			t.Fatalf("marshal %+v: %v", tc.in, err)
		}
		if string(got) != tc.want {
			t.Errorf("marshal %+v: got %s want %s", tc.in, got, tc.want)
		}
	}
}

func TestAuthPlanTypeFromRawValue(t *testing.T) {
	if got := AuthPlanTypeFromRawValue("HC"); got.Kind != AuthPlanTypeKnown || got.Known != KnownPlanEnterprise {
		t.Errorf("HC: got %+v", got)
	}
	if got := AuthPlanTypeFromRawValue("weird"); got.Kind != AuthPlanTypeUnknown || got.Unknown != "weird" {
		t.Errorf("weird: got %+v", got)
	}
}

func TestAccountPlanTypeWire(t *testing.T) {
	cases := []struct {
		plan PlanType
		want string
	}{
		{PlanTypeFree, `"free"`},
		{PlanTypeProLite, `"prolite"`},
		{PlanTypeSelfServeBusinessUsageBased, `"self_serve_business_usage_based"`},
		{PlanTypeEnterpriseCbpUsageBased, `"enterprise_cbp_usage_based"`},
	}
	for _, tc := range cases {
		got, err := json.Marshal(tc.plan)
		if err != nil {
			t.Fatalf("marshal %v: %v", tc.plan, err)
		}
		if string(got) != tc.want {
			t.Errorf("PlanType %v: got %s want %s", tc.plan, got, tc.want)
		}
		var back PlanType
		if err := json.Unmarshal([]byte(tc.want), &back); err != nil {
			t.Fatalf("unmarshal %s: %v", tc.want, err)
		}
		if back != tc.plan {
			t.Errorf("round-trip %s: got %v want %v", tc.want, back, tc.plan)
		}
	}
}

func TestAccountPlanTypeUnknownFallback(t *testing.T) {
	var p PlanType
	if err := json.Unmarshal([]byte(`"brand-new-tier"`), &p); err != nil {
		t.Fatalf("unknown: %v", err)
	}
	if p != PlanTypeUnknown {
		t.Errorf("unknown fallback: got %v want unknown", p)
	}
}

func TestAccountPlanTypeFamilyHelpers(t *testing.T) {
	if !PlanTypeTeam.IsTeamLike() || !PlanTypeSelfServeBusinessUsageBased.IsTeamLike() {
		t.Error("team-like helpers wrong")
	}
	if PlanTypeBusiness.IsTeamLike() {
		t.Error("business should not be team-like")
	}
	if !PlanTypeBusiness.IsBusinessLike() || !PlanTypeEnterpriseCbpUsageBased.IsBusinessLike() {
		t.Error("business-like helpers wrong")
	}
	for _, p := range []PlanType{
		PlanTypeTeam, PlanTypeSelfServeBusinessUsageBased, PlanTypeBusiness,
		PlanTypeEnterpriseCbpUsageBased, PlanTypeEnterprise, PlanTypeEdu,
	} {
		if !p.IsWorkspaceAccount() {
			t.Errorf("%v should be workspace account", p)
		}
	}
	if PlanTypePro.IsWorkspaceAccount() {
		t.Error("pro should not be workspace account")
	}
}

func TestPlanTypeConversions(t *testing.T) {
	if got := PlanTypeFromKnownPlan(KnownPlanEnterpriseCbpUsageBased); got != PlanTypeEnterpriseCbpUsageBased {
		t.Errorf("from KnownPlan: got %v", got)
	}
	if got := PlanTypeFromAuthPlanType(KnownAuthPlanType(KnownPlanEnterprise)); got != PlanTypeEnterprise {
		t.Errorf("from AuthPlanType known: got %v", got)
	}
	if got := PlanTypeFromAuthPlanType(UnknownAuthPlanType("mystery-tier")); got != PlanTypeUnknown {
		t.Errorf("from AuthPlanType unknown: got %v", got)
	}
}

func TestParsedCommandRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   ParsedCommand
		want string
	}{
		{
			name: "read",
			in:   NewReadParsedCommand("cat foo.txt", "foo.txt", "/abs/foo.txt"),
			want: `{"cmd":"cat foo.txt","name":"foo.txt","path":"/abs/foo.txt","type":"read"}`,
		},
		{
			name: "list_files_some",
			in:   NewListFilesParsedCommand("ls src", strPtr("src")),
			want: `{"cmd":"ls src","path":"src","type":"list_files"}`,
		},
		{
			name: "list_files_none",
			in:   NewListFilesParsedCommand("ls", nil),
			want: `{"cmd":"ls","path":null,"type":"list_files"}`,
		},
		{
			name: "search",
			in:   NewSearchParsedCommand("rg foo src", strPtr("foo"), strPtr("src")),
			want: `{"cmd":"rg foo src","path":"src","query":"foo","type":"search"}`,
		},
		{
			name: "search_nulls",
			in:   NewSearchParsedCommand("rg", nil, nil),
			want: `{"cmd":"rg","path":null,"query":null,"type":"search"}`,
		},
		{
			name: "unknown",
			in:   NewUnknownParsedCommand("frobnicate"),
			want: `{"cmd":"frobnicate","type":"unknown"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("marshal: got %s want %s", got, tc.want)
			}
			var back ParsedCommand
			if err := json.Unmarshal(got, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			remarshaled, err := json.Marshal(back)
			if err != nil {
				t.Fatalf("re-marshal: %v", err)
			}
			if string(remarshaled) != tc.want {
				t.Errorf("round-trip: got %s want %s", remarshaled, tc.want)
			}
		})
	}
}

func TestParsedCommandUnknownTypeRejected(t *testing.T) {
	var p ParsedCommand
	if err := json.Unmarshal([]byte(`{"type":"weird","cmd":"x"}`), &p); err == nil {
		t.Error("expected error decoding unknown ParsedCommand type")
	}
}

func TestRefreshTokenFailedError(t *testing.T) {
	err := NewRefreshTokenFailedError(RefreshTokenFailedExpired, "token expired")
	if err.Error() != "token expired" {
		t.Errorf("Error(): got %q", err.Error())
	}
	if err.Reason != RefreshTokenFailedExpired {
		t.Errorf("Reason: got %v", err.Reason)
	}
}

func TestMcpApprovalMetaConstants(t *testing.T) {
	pairs := map[string]string{
		ApprovalKindKey:            "codex_approval_kind",
		ApprovalKindMcpToolCall:    "mcp_tool_call",
		ApprovalKindToolSuggestion: "tool_suggestion",
		RequestTypeKey:             "codex_request_type",
		RequestTypeApprovalRequest: "approval_request",
		ApprovalsReviewerKey:       "approvals_reviewer",
		PersistKey:                 "persist",
		PersistSession:             "session",
		PersistAlways:              "always",
		SourceKey:                  "source",
		SourceConnector:            "connector",
		ConnectorIDKey:             "connector_id",
		ConnectorNameKey:           "connector_name",
		ConnectorDescriptionKey:    "connector_description",
		ToolNameKey:                "tool_name",
		ToolTitleKey:               "tool_title",
		ToolDescriptionKey:         "tool_description",
		ToolParamsKey:              "tool_params",
		ToolParamsDisplayKey:       "tool_params_display",
	}
	for got, want := range pairs {
		if got != want {
			t.Errorf("constant mismatch: got %q want %q", got, want)
		}
	}
}

func TestProviderAccountConstructors(t *testing.T) {
	if NewAPIKeyProviderAccount().Kind != ProviderAccountAPIKey {
		t.Error("api key kind wrong")
	}
	c := NewChatgptProviderAccount("a@b.com", PlanTypePro)
	if c.Kind != ProviderAccountChatgpt || c.Email != "a@b.com" || c.PlanType != PlanTypePro {
		t.Errorf("chatgpt account wrong: %+v", c)
	}
	if NewAmazonBedrockProviderAccount().Kind != ProviderAccountAmazonBedrock {
		t.Error("bedrock kind wrong")
	}
}
