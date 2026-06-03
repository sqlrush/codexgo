package sandboxsummary

import (
	"reflect"
	"testing"
)

func TestSummarizeSandboxPolicy(t *testing.T) {
	tests := []struct {
		name   string
		policy SandboxPolicy
		want   string
	}{
		{
			name:   "danger full access",
			policy: DangerFullAccess(),
			want:   "danger-full-access",
		},
		{
			name:   "read only without network",
			policy: ReadOnly(false),
			want:   "read-only",
		},
		{
			name:   "read only with enabled network",
			policy: ReadOnly(true),
			want:   "read-only (network access enabled)",
		},
		{
			name:   "external sandbox restricted has no suffix",
			policy: ExternalSandbox(NetworkRestricted),
			want:   "external-sandbox",
		},
		{
			name:   "external sandbox enabled adds suffix",
			policy: ExternalSandbox(NetworkEnabled),
			want:   "external-sandbox (network access enabled)",
		},
		{
			name:   "external sandbox zero value is restricted",
			policy: SandboxPolicy{Kind: KindExternalSandbox},
			want:   "external-sandbox",
		},
		{
			name:   "workspace write defaults include tmp entries",
			policy: WorkspaceWrite(nil, false, false, false),
			want:   "workspace-write [workdir, /tmp, $TMPDIR]",
		},
		{
			name:   "workspace write excluding tmp entries",
			policy: WorkspaceWrite(nil, false, true, true),
			want:   "workspace-write [workdir]",
		},
		{
			name:   "workspace write with root and network",
			policy: WorkspaceWrite([]string{"/repo"}, true, true, true),
			want:   "workspace-write [workdir, /repo] (network access enabled)",
		},
		{
			name:   "workspace write windows-style root and network",
			policy: WorkspaceWrite([]string{`C:\repo`}, true, true, true),
			want:   `workspace-write [workdir, C:\repo] (network access enabled)`,
		},
		{
			name:   "workspace write multiple roots preserve order",
			policy: WorkspaceWrite([]string{"/a", "/b"}, false, false, false),
			want:   "workspace-write [workdir, /tmp, $TMPDIR, /a, /b]",
		},
		{
			name:   "unknown kind falls back to raw tag",
			policy: SandboxPolicy{Kind: SandboxPolicyKind("mystery")},
			want:   "mystery",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SummarizeSandboxPolicy(tt.policy)
			if got != tt.want {
				t.Errorf("SummarizeSandboxPolicy() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWorkspaceWriteDoesNotMutateInput(t *testing.T) {
	roots := []string{"/repo"}
	policy := WorkspaceWrite(roots, false, false, false)

	// Mutating the original slice must not affect the stored policy.
	roots[0] = "/tampered"
	if policy.WritableRoots[0] != "/repo" {
		t.Fatalf("WorkspaceWrite did not copy writable roots: got %q", policy.WritableRoots[0])
	}

	// Summarizing must not mutate the policy's roots either.
	_ = SummarizeSandboxPolicy(policy)
	if !reflect.DeepEqual(policy.WritableRoots, []string{"/repo"}) {
		t.Fatalf("SummarizeSandboxPolicy mutated writable roots: %v", policy.WritableRoots)
	}
}

// fakeProfile is a test double implementing PermissionProfile.
type fakeProfile struct {
	policy         SandboxPolicy
	ok             bool
	networkEnabled bool
	gotCwd         string
}

func (f *fakeProfile) ToLegacySandboxPolicy(cwd string) (SandboxPolicy, bool) {
	f.gotCwd = cwd
	return f.policy, f.ok
}

func (f *fakeProfile) NetworkSandboxPolicyEnabled() bool { return f.networkEnabled }

func TestSummarizePermissionProfile(t *testing.T) {
	tests := []struct {
		name           string
		profile        *fakeProfile
		cwd            string
		workspaceRoots []string
		want           string
	}{
		{
			name: "workspace write uses runtime roots and hides internal writes",
			profile: &fakeProfile{
				// The resolved policy's own roots are intentionally a hidden
				// internal path; the summary must ignore them in favor of the
				// runtime workspace roots.
				policy: WorkspaceWrite([]string{"/Users/test/.codex/memories"}, false, false, false),
				ok:     true,
			},
			cwd:            "/repo",
			workspaceRoots: []string{"/repo", "/repo-extra"},
			want:           "workspace-write [workdir, /tmp, $TMPDIR, /repo-extra]",
		},
		{
			name: "workspace write with network and excluded tmp",
			profile: &fakeProfile{
				policy: WorkspaceWrite(nil, true, true, true),
				ok:     true,
			},
			cwd:            "/repo",
			workspaceRoots: []string{"/repo"},
			want:           "workspace-write [workdir] (network access enabled)",
		},
		{
			name: "non workspace policy delegates to summarize",
			profile: &fakeProfile{
				policy: ReadOnly(true),
				ok:     true,
			},
			cwd:            "/repo",
			workspaceRoots: []string{"/repo", "/other"},
			want:           "read-only (network access enabled)",
		},
		{
			name: "external sandbox delegates to summarize",
			profile: &fakeProfile{
				policy: ExternalSandbox(NetworkEnabled),
				ok:     true,
			},
			cwd:  "/repo",
			want: "external-sandbox (network access enabled)",
		},
		{
			name: "error path without network",
			profile: &fakeProfile{
				ok:             false,
				networkEnabled: false,
			},
			cwd:  "/repo",
			want: "custom permissions",
		},
		{
			name: "error path with network enabled",
			profile: &fakeProfile{
				ok:             false,
				networkEnabled: true,
			},
			cwd:  "/repo",
			want: "custom permissions (network access enabled)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SummarizePermissionProfile(tt.profile, tt.cwd, tt.workspaceRoots)
			if got != tt.want {
				t.Errorf("SummarizePermissionProfile() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSummarizePermissionProfileDoesNotMutateRoots(t *testing.T) {
	roots := []string{"/repo", "/extra"}
	profile := &fakeProfile{policy: WorkspaceWrite(nil, false, false, false), ok: true}

	_ = SummarizePermissionProfile(profile, "/repo", roots)

	if !reflect.DeepEqual(roots, []string{"/repo", "/extra"}) {
		t.Fatalf("workspaceRoots was mutated: %v", roots)
	}
}

func strPtr(s string) *string { return &s }

func TestCreateConfigSummaryEntries(t *testing.T) {
	tests := []struct {
		name   string
		config ConfigSummary
		model  string
		want   []Entry
	}{
		{
			name: "non responses wire api omits reasoning entries",
			config: ConfigSummary{
				Workdir:        "/repo",
				Provider:       "openai",
				ApprovalPolicy: "on-request",
				SandboxPolicy:  ReadOnly(false),
				WireAPI:        "chat",
				// Reasoning fields set but must be ignored for non-responses.
				ReasoningEffort:  strPtr("high"),
				ReasoningSummary: strPtr("auto"),
			},
			model: "gpt-x",
			want: []Entry{
				{Key: "workdir", Value: "/repo"},
				{Key: "model", Value: "gpt-x"},
				{Key: "provider", Value: "openai"},
				{Key: "approval", Value: "on-request"},
				{Key: "sandbox", Value: "read-only"},
			},
		},
		{
			name: "responses wire api with reasoning values",
			config: ConfigSummary{
				Workdir:          "/repo",
				Provider:         "openai",
				ApprovalPolicy:   "never",
				SandboxPolicy:    WorkspaceWrite(nil, true, false, false),
				WireAPI:          WireResponses,
				ReasoningEffort:  strPtr("medium"),
				ReasoningSummary: strPtr("detailed"),
			},
			model: "gpt-5",
			want: []Entry{
				{Key: "workdir", Value: "/repo"},
				{Key: "model", Value: "gpt-5"},
				{Key: "provider", Value: "openai"},
				{Key: "approval", Value: "never"},
				{Key: "sandbox", Value: "workspace-write [workdir, /tmp, $TMPDIR] (network access enabled)"},
				{Key: "reasoning effort", Value: "medium"},
				{Key: "reasoning summaries", Value: "detailed"},
			},
		},
		{
			name: "responses wire api with absent reasoning values defaults to none",
			config: ConfigSummary{
				Workdir:        "/w",
				Provider:       "p",
				ApprovalPolicy: "untrusted",
				SandboxPolicy:  DangerFullAccess(),
				WireAPI:        WireResponses,
			},
			model: "m",
			want: []Entry{
				{Key: "workdir", Value: "/w"},
				{Key: "model", Value: "m"},
				{Key: "provider", Value: "p"},
				{Key: "approval", Value: "untrusted"},
				{Key: "sandbox", Value: "danger-full-access"},
				{Key: "reasoning effort", Value: "none"},
				{Key: "reasoning summaries", Value: "none"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CreateConfigSummaryEntries(tt.config, tt.model)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CreateConfigSummaryEntries() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestNetworkAccessIsEnabled(t *testing.T) {
	if NetworkRestricted.IsEnabled() {
		t.Error("NetworkRestricted.IsEnabled() = true, want false")
	}
	if !NetworkEnabled.IsEnabled() {
		t.Error("NetworkEnabled.IsEnabled() = false, want true")
	}
}
