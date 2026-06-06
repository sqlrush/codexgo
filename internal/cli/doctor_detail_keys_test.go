package cli

import (
	"context"
	"sort"
	"strings"
	"testing"
)

// detailKeys returns the sorted set of detail labels a check emits, mirroring the
// key set codex's structured_json_details would produce.
func detailKeys(c doctorCheck) []string {
	keys, _ := structuredJSONDetails(c.Details)
	out := make([]string, 0, len(keys))
	for k := range keys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// hasKey reports whether the check emits a detail labeled key.
func hasKey(c doctorCheck, key string) bool {
	for _, k := range detailKeys(c) {
		if k == key {
			return true
		}
	}
	return false
}

// TestDoctorDetailKeySetsMatchCodex asserts each check's deterministic detail
// labels match the labels codex 0.136.0 emits for the same environment. Keys that
// are environment-conditional in codex (auth-mode-dependent reachability labels,
// proxy env presence, npm-managed install rows, terminal program vars) are not
// asserted here because they legitimately vary with the host environment.
func TestDoctorDetailKeySetsMatchCodex(t *testing.T) {
	t.Setenv("CODEXGO_HOME", t.TempDir())
	t.Setenv("CODEXGO_DOCTOR_SKIP_NETWORK", "1")

	checks := buildDoctorChecks(context.Background(), RootOptions{})
	byID := map[string]doctorCheck{}
	for _, c := range checks {
		byID[c.ID] = c
	}

	// wantKeys lists detail labels each check must always emit on this platform.
	wantKeys := map[string][]string{
		"config.load": {
			"CODEXGO_HOME", "config.toml", "cwd", "enabled feature flags",
			"feature flag overrides", "feature flags enabled", "log dir",
			"mcp servers", "model", "model provider", "sqlite home",
		},
		"runtime.provenance": {
			"commit", "current executable", "install method", "platform", "version",
		},
		"system.environment": {"os", "os language", "os type", "os version"},
		"sandbox.helpers": {
			"approval policy", "codex-linux-sandbox helper",
			"execve wrapper helper", "filesystem sandbox", "network sandbox",
		},
		"state.paths": {
			"CODEXGO_HOME", "active rollout files", "archived rollout files",
			"goals DB", "goals DB integrity", "log DB", "log DB integrity",
			"log dir", "memories DB", "memories DB integrity", "sqlite home",
			"state DB", "state DB integrity",
		},
		"state.rollout_db_parity": {
			"default model provider", "rollout DB active files",
			"rollout DB archived files", "rollout DB malformed file names",
			"rollout DB rows", "rollout DB scan cap reached",
			"rollout DB scan errors",
		},
		"app_server.status": {
			"control socket", "daemon state dir", "mode", "pid file",
			"settings", "status", "update-loop pid file",
		},
		"terminal.title": {
			"terminal title activity", "terminal title items",
			"terminal title project source", "terminal title project value",
			"terminal title source",
		},
		"updates.status": {
			"check for update on startup", "update action", "version cache",
		},
	}

	for id, want := range wantKeys {
		c, ok := byID[id]
		if !ok {
			t.Fatalf("missing check %q", id)
		}
		for _, key := range want {
			if !hasKey(c, key) {
				t.Errorf("check %q missing detail key %q; got %v", id, key, detailKeys(c))
			}
		}
	}
}

// TestConfigLoadConfigTomlIsArrayWhenMissing asserts config.toml collapses into a
// repeated-label array ([path, "missing"]) when the file is absent, matching the
// JSON array codex emits.
func TestConfigLoadConfigTomlIsArrayWhenMissing(t *testing.T) {
	t.Setenv("CODEXGO_HOME", t.TempDir())
	t.Setenv("CODEXGO_DOCTOR_SKIP_NETWORK", "1")

	_, check := configLoadCheck(RootOptions{})
	structured, _ := structuredJSONDetails(check.Details)
	got, ok := structured["config.toml"]
	if !ok {
		t.Fatalf("config.toml detail missing; details=%v", check.Details)
	}
	if got.One != nil {
		t.Fatalf("config.toml should be a repeated array, got scalar %q", *got.One)
	}
	if len(got.Many) != 2 || got.Many[1] != "missing" {
		t.Errorf("config.toml = %v, want [<path>, missing]", got.Many)
	}
}

// TestConfigLoadDefaultsModelAndProvider asserts the unset model renders as
// "<default>" and the unset provider defaults to "openai", matching codex.
func TestConfigLoadDefaultsModelAndProvider(t *testing.T) {
	t.Setenv("CODEXGO_HOME", t.TempDir())
	t.Setenv("CODEXGO_DOCTOR_SKIP_NETWORK", "1")

	_, check := configLoadCheck(RootOptions{})
	structured, _ := structuredJSONDetails(check.Details)
	if v := structured["model"]; v.One == nil || *v.One != "<default>" {
		t.Errorf("model = %#v, want <default>", v)
	}
	if v := structured["model provider"]; v.One == nil || *v.One != "openai" {
		t.Errorf("model provider = %#v, want openai", v)
	}
}

// TestSandboxHelpersFilesystemNetworkKeys asserts the sandbox check reports
// separate filesystem/network sandbox labels rather than a single "sandbox mode".
func TestSandboxHelpersFilesystemNetworkKeys(t *testing.T) {
	t.Setenv("CODEXGO_HOME", t.TempDir())
	t.Setenv("CODEXGO_DOCTOR_SKIP_NETWORK", "1")

	dctx, _ := configLoadCheck(RootOptions{})
	c := sandboxHelpersCheck(dctx)
	for _, key := range []string{"filesystem sandbox", "network sandbox"} {
		if !hasKey(c, key) {
			t.Errorf("sandbox.helpers missing %q; got %v", key, detailKeys(c))
		}
	}
	if hasKey(c, "sandbox mode") {
		t.Errorf("sandbox.helpers should not emit legacy 'sandbox mode' key")
	}
}

// TestStateRolloutParityKeys asserts the rollout/state DB parity check emits the
// codex "rollout DB ..." label family with the state-DB-missing skip row.
func TestStateRolloutParityKeys(t *testing.T) {
	t.Setenv("CODEXGO_HOME", t.TempDir())
	t.Setenv("CODEXGO_DOCTOR_SKIP_NETWORK", "1")

	dctx, _ := configLoadCheck(RootOptions{})
	c := stateRolloutParityCheck(dctx)
	structured, _ := structuredJSONDetails(c.Details)
	if v := structured["rollout DB rows"]; v.One == nil || !strings.Contains(*v.One, "skipped") {
		t.Errorf("rollout DB rows = %#v, want skipped (state DB missing)", v)
	}
}
