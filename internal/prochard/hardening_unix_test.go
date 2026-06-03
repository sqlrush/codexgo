//go:build darwin || linux || freebsd || openbsd || netbsd

package prochard

import (
	"os"
	"testing"
)

func TestRemoveEnvVarsWithPrefix(t *testing.T) {
	const (
		matchA = "PROCHARD_TEST_LD_A"
		matchB = "PROCHARD_TEST_LD_B"
		keep   = "PROCHARD_TEST_KEEP"
	)

	t.Setenv(matchA, "1")
	t.Setenv(matchB, "2")
	t.Setenv(keep, "stays")

	removeEnvVarsWithPrefix("PROCHARD_TEST_LD_")

	if v, ok := os.LookupEnv(matchA); ok {
		t.Errorf("%s should have been removed, still set to %q", matchA, v)
	}
	if v, ok := os.LookupEnv(matchB); ok {
		t.Errorf("%s should have been removed, still set to %q", matchB, v)
	}
	if v, ok := os.LookupEnv(keep); !ok || v != "stays" {
		t.Errorf("%s should be retained as %q, got (%q, %v)", keep, "stays", v, ok)
	}
}

func TestRemoveEnvVarsWithPrefixNoMatch(t *testing.T) {
	const keep = "PROCHARD_TEST_OTHER"
	t.Setenv(keep, "v")

	removeEnvVarsWithPrefix("PROCHARD_TEST_NOSUCH_")

	if v, ok := os.LookupEnv(keep); !ok || v != "v" {
		t.Errorf("%s should be untouched, got (%q, %v)", keep, v, ok)
	}
}
