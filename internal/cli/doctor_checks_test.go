package cli

import (
	"context"
	"testing"
)

func TestBuildDoctorChecksCategories(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("CODEX_DOCTOR_SKIP_NETWORK", "1")

	checks := buildDoctorChecks(context.Background(), RootOptions{})

	wantCategories := []string{
		"system", "installation", "terminal", "config", "auth", "state", "git", "provider",
	}
	if len(checks) != len(wantCategories) {
		t.Fatalf("got %d checks, want %d", len(checks), len(wantCategories))
	}
	for i, want := range wantCategories {
		if checks[i].Category != want {
			t.Errorf("check[%d] category = %q, want %q", i, checks[i].Category, want)
		}
		if checks[i].Status == "" {
			t.Errorf("check[%d] (%s) has empty status", i, checks[i].Category)
		}
	}
}

func TestSystemCheckAlwaysOK(t *testing.T) {
	c := systemCheck()
	if c.Status != statusOK {
		t.Errorf("system check status = %q, want ok", c.Status)
	}
	if len(c.Details) == 0 {
		t.Errorf("system check has no details")
	}
}
