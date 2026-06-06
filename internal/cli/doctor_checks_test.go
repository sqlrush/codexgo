package cli

import (
	"context"
	"sort"
	"testing"
)

// TestBuildDoctorChecksEmitsCanonicalIDs asserts the doctor emits exactly the 18
// check IDs codex 0.136.0 emits, so doctor --json matches codex check-for-check.
func TestBuildDoctorChecksEmitsCanonicalIDs(t *testing.T) {
	t.Setenv("CODEXGO_HOME", t.TempDir())
	t.Setenv("CODEXGO_DOCTOR_SKIP_NETWORK", "1")

	checks := buildDoctorChecks(context.Background(), RootOptions{})

	if len(checks) != len(doctorCheckIDs) {
		t.Fatalf("got %d checks, want %d", len(checks), len(doctorCheckIDs))
	}

	gotIDs := make([]string, 0, len(checks))
	for _, c := range checks {
		gotIDs = append(gotIDs, c.ID)
		if c.Status == "" {
			t.Errorf("check %q has empty status", c.ID)
		}
		if c.Category == "" {
			t.Errorf("check %q has empty category", c.ID)
		}
	}

	wantIDs := append([]string(nil), doctorCheckIDs...)
	sort.Strings(wantIDs)
	sortedGot := append([]string(nil), gotIDs...)
	sort.Strings(sortedGot)
	for i, want := range wantIDs {
		if sortedGot[i] != want {
			t.Errorf("check id set mismatch at %d: got %q, want %q", i, sortedGot[i], want)
		}
	}
}

// TestDoctorCheckIDsAreUnique guards against accidental duplicate IDs in the
// emitted set, which would break check-for-check comparison with codex.
func TestDoctorCheckIDsAreUnique(t *testing.T) {
	t.Setenv("CODEXGO_HOME", t.TempDir())
	t.Setenv("CODEXGO_DOCTOR_SKIP_NETWORK", "1")

	checks := buildDoctorChecks(context.Background(), RootOptions{})
	seen := map[string]bool{}
	for _, c := range checks {
		if seen[c.ID] {
			t.Errorf("duplicate check id: %q", c.ID)
		}
		seen[c.ID] = true
	}
}

// TestNetworkChecksSkippedWhenOffline asserts that the network reachability
// checks report a skipped status when CODEX_DOCTOR_SKIP_NETWORK is set, keeping
// offline and deterministic runs fast.
func TestNetworkChecksSkippedWhenOffline(t *testing.T) {
	t.Setenv("CODEXGO_HOME", t.TempDir())
	t.Setenv("CODEXGO_DOCTOR_SKIP_NETWORK", "1")

	checks := buildDoctorChecks(context.Background(), RootOptions{})
	byID := map[string]doctorCheck{}
	for _, c := range checks {
		byID[c.ID] = c
	}
	for _, id := range []string{"network.provider_reachability", "network.websocket_reachability"} {
		c, ok := byID[id]
		if !ok {
			t.Fatalf("missing check %q", id)
		}
		if c.Status != statusSkipped {
			t.Errorf("check %q status = %q, want skipped", id, c.Status)
		}
	}
}

// TestSystemEnvironmentCheckAlwaysOK verifies the always-informational
// system.environment check reports ok with details.
func TestSystemEnvironmentCheckAlwaysOK(t *testing.T) {
	c := systemEnvironmentCheck()
	if c.Status != statusOK {
		t.Errorf("system.environment status = %q, want ok", c.Status)
	}
	if c.ID != "system.environment" {
		t.Errorf("system.environment id = %q", c.ID)
	}
	if len(c.Details) == 0 {
		t.Errorf("system.environment has no details")
	}
}
