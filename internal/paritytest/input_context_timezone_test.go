package paritytest

// Timezone-rendering parity: the read-only differential (TestParityInputContext)
// runs both binaries on the same host WITHOUT pinning TZ, so it cannot expose a
// timezone-resolution gap. This sibling pins an explicit TZ (and one unset case)
// and asserts the `<timezone>` line of the user-message `<environment_context>`
// is byte-identical to the real codex 0.136.0 binary.
//
// codex resolves the zone via iana_time_zone::get_timezone()
// (core/src/session/turn_context.rs::local_time_context), which on macOS goes
// through CoreFoundation's CFTimeZoneCopySystem. CoreFoundation honors TZ but
// canonicalizes it: the alias `UTC` resolves to its link target `GMT`, while an
// unset/invalid TZ falls back to the system zone. codexgo's resolver
// (internal/core.ianaTimezone) mirrors that exactly. Env-gated on
// CODEX_PARITY_BIN.

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// captureTimezoneLine runs `bin exec --json` with the given TZ (pinned, or
// removed from the environment when tz=="") and returns the trimmed `<timezone>`
// line of the user-message `<environment_context>`.
func captureTimezoneLine(t *testing.T, who, bin, tz string) string {
	t.Helper()
	srv := newCapturingServer(t)
	defer srv.Close()

	home := t.TempDir()
	writeParityConfig(t, home, srv.URL)
	cmd := exec.Command(bin, "exec", "--json", "--skip-git-repo-check", turnPrompt)
	env := append(os.Environ(),
		"CODEX_HOME="+home,
		fakeEnvKey+"="+fakeAPIKey,
		"OPENAI_API_KEY=",
		"CODEX_API_KEY=",
		"CODEX_ACCESS_TOKEN=",
	)
	env = withTZ(env, tz)
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s exec (TZ=%q) failed: %v\nstderr:\n%s", who, tz, err, stderr.String())
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.requests) == 0 {
		t.Fatalf("%s (TZ=%q) sent no /responses request", who, tz)
	}
	var req map[string]json.RawMessage
	if err := json.Unmarshal(srv.requests[0], &req); err != nil {
		t.Fatalf("%s (TZ=%q) decode request body: %v", who, tz, err)
	}
	var in []map[string]any
	if err := json.Unmarshal(req["input"], &in); err != nil {
		t.Fatalf("%s (TZ=%q) decode input: %v", who, tz, err)
	}
	envCtx := firstTextWithPrefix(in, "<environment_context>")
	if envCtx == "" {
		t.Fatalf("%s (TZ=%q) emitted no <environment_context>", who, tz)
	}
	for _, line := range strings.Split(envCtx, "\n") {
		if strings.Contains(line, "<timezone>") {
			return strings.TrimSpace(line)
		}
	}
	t.Fatalf("%s (TZ=%q) <environment_context> has no <timezone> line:\n%s", who, tz, envCtx)
	return ""
}

// withTZ returns env with TZ pinned to tz, or with any TZ entry removed when
// tz is the sentinel unsetTZ.
func withTZ(env []string, tz string) []string {
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if strings.HasPrefix(e, "TZ=") {
			continue
		}
		out = append(out, e)
	}
	if tz != unsetTZ {
		out = append(out, "TZ="+tz)
	}
	return out
}

// unsetTZ is the sentinel that means "do not set TZ in the child environment".
const unsetTZ = "\x00UNSET"

// TestParityInputContextTimezone asserts codexgo's rendered `<timezone>` line is
// byte-identical to codex for an explicit UTC (the canonicalization case), an
// explicit region, and an unset TZ (the system-zone fallback).
func TestParityInputContextTimezone(t *testing.T) {
	refBin := referenceBin(t)
	cgoBin := buildCodexgo(t)

	cases := []struct {
		name string
		tz   string
	}{
		{"utc_canonicalizes_to_gmt", "UTC"},
		{"explicit_region", "America/New_York"},
		{"unset_uses_system_zone", unsetTZ},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			refTZ := captureTimezoneLine(t, "codex", refBin, tc.tz)
			cgoTZ := captureTimezoneLine(t, "codexgo", cgoBin, tc.tz)
			if refTZ != cgoTZ {
				t.Errorf("timezone line mismatch (TZ=%q)\n codex:   %q\n codexgo: %q", tc.tz, refTZ, cgoTZ)
			}
		})
	}
}
