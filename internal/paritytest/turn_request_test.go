package paritytest

// Full request-body parity: capture the POST /v1/responses body BOTH binaries
// send for the same plain turn and compare every top-level field (after stripping
// volatile per-run values). This is the broadest request-side differential — it
// reveals divergences in instructions (base system prompt), tools, reasoning,
// include, tool_choice, store, parallel_tool_calls, service_tier, model, etc.
// Env-gated on CODEX_PARITY_BIN.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// runPlainTurnCapture runs `bin exec --json` against a capturing server and
// returns the first decoded request body.
func runPlainTurnCapture(t *testing.T, who, bin string) map[string]json.RawMessage {
	t.Helper()
	req, _ := runPlainTurnCaptureHome(t, who, bin)
	return req
}

// runPlainTurnCaptureHome additionally returns the per-run CODEX_HOME so
// callers can normalize home-embedded absolute paths (the skills_instructions
// scan renders SKILL.md paths under CODEX_HOME) before comparing captures.
func runPlainTurnCaptureHome(t *testing.T, who, bin string) (map[string]json.RawMessage, string) {
	t.Helper()
	srv := newCapturingServer(t)
	defer srv.Close()

	home := t.TempDir()
	writeParityConfig(t, home, srv.URL)
	cmd := exec.Command(bin, "exec", "--json", "--skip-git-repo-check", turnPrompt)
	cmd.Env = append(os.Environ(),
		"CODEX_HOME="+home,
		"CODEXGO_HOME="+home,
		fakeEnvKey+"="+fakeAPIKey,
		"OPENAI_API_KEY=",
		"CODEX_API_KEY=",
		"CODEXGO_API_KEY=",
		"CODEX_ACCESS_TOKEN=",
		"CODEXGO_ACCESS_TOKEN=",
	)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s exec failed: %v\nstderr:\n%s", who, err, stderr.String())
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.requests) == 0 {
		t.Fatalf("%s sent no /responses request", who)
	}
	var req map[string]json.RawMessage
	if err := json.Unmarshal(srv.requests[0], &req); err != nil {
		t.Fatalf("%s decode request body: %v", who, err)
	}
	return req, home
}

// normalizeHomePaths replaces the per-run CODEX_HOME (in both its literal and
// symlink-resolved forms — macOS /var vs /private/var) with a stable
// placeholder so home-embedded absolute paths compare across runs.
func normalizeHomePaths(s, home string) string {
	s = strings.ReplaceAll(s, home, "<CODEX_HOME>")
	if resolved, err := filepath.EvalSymlinks(home); err == nil && resolved != home {
		s = strings.ReplaceAll(s, resolved, "<CODEX_HOME>")
	}
	return s
}

// canonField re-marshals a raw field value in canonical (sorted-key) form so two
// semantically-equal values compare byte-for-byte.
func canonField(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode field: %v", err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("re-marshal field: %v", err)
	}
	return string(b)
}

// volatileRequestFields are top-level request fields that legitimately differ per
// run (so they are compared for presence only, not value).
var volatileRequestFields = map[string]bool{
	"prompt_cache_key": true, // codex: thread id (UUIDv7); codexgo: monotonic id
	"client_metadata":  true, // carries the per-install x-codex-installation-id
}

// documentedGapFields are top-level request fields where codexgo is a KNOWN,
// tracked divergence from codex (see docs/PARITY.md "request-body gaps"). They
// are compared for presence (codexgo must still send the field) and logged
// with byte sizes, but a value difference is reported via t.Log, not t.Error.
//
// The map is now EMPTY: every top-level request field — including
// `instructions` (personality rendering), `tools` (the full 11-tool registry),
// and `input` (permissions + skills_instructions + environment_context, with
// per-run CODEX_HOME paths normalized) — is byte-identical to codex and
// enforced below, so any regression fails the test loudly.
var documentedGapFields = map[string]bool{}

// TestParityRequestBody asserts codexgo sends a /responses request whose
// non-volatile top-level fields are ALL byte-identical to real codex for a
// plain turn (per-run CODEX_HOME paths normalized) — full request-level
// drop-in for this scenario.
func TestParityRequestBody(t *testing.T) {
	refBin := referenceBin(t)
	cgoBin := buildCodexgo(t)

	ref, refHome := runPlainTurnCaptureHome(t, "codex", refBin)
	cgo, cgoHome := runPlainTurnCaptureHome(t, "codexgo", cgoBin)

	// 1) Same set of top-level keys (codexgo must send every field codex does).
	refKeys := sortedKeys(ref)
	cgoKeys := sortedKeys(cgo)
	if strings.Join(refKeys, ",") != strings.Join(cgoKeys, ",") {
		t.Errorf("top-level request key set mismatch\n codex:   %v\n codexgo: %v", refKeys, cgoKeys)
	}

	// 2) Each non-volatile field byte-identical (canonicalized; per-run
	//    CODEX_HOME paths normalized), except the documented large-port gaps
	//    which are logged for tracking.
	for _, k := range refKeys {
		cVal, ok := cgo[k]
		if !ok {
			t.Errorf("codexgo missing request field %q", k)
			continue
		}
		if volatileRequestFields[k] {
			continue
		}
		rc := normalizeHomePaths(canonField(t, ref[k]), refHome)
		cc := normalizeHomePaths(canonField(t, cVal), cgoHome)
		if rc == cc {
			continue
		}
		if documentedGapFields[k] {
			t.Logf("TRACKED request-body gap %q (docs/PARITY.md): codex=%d bytes, codexgo=%d bytes", k, len(rc), len(cc))
			continue
		}
		t.Errorf("request field %q mismatch\n codex:   %s\n codexgo: %s", k, truncate(rc, 400), truncate(cc, 400))
	}
}

// sortedKeys returns the map keys in sorted order.
func sortedKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// truncate shortens s to at most n runes, appending an ellipsis marker.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…(" + itoa(len(s)) + " bytes)"
}

// itoa is a tiny strconv.Itoa to avoid an extra import in this test file.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
