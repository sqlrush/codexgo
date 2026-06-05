package paritytest

// Sandbox-mode environment_context parity: the read-only differential
// (TestParityInputContext) only exercises the default sandbox. This sibling runs
// BOTH binaries with a non-default sandbox_mode in the SAME fresh cwd and
// asserts the user-message `<environment_context>` (which carries the
// `<filesystem>` description) is byte-identical. It pins the workspace-write
// managed/restricted profile (write + read-only carveout entries) and the
// danger-full-access disabled/unrestricted profile captured from codex 0.136.0.
// Env-gated on CODEX_PARITY_BIN.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeSandboxParityConfig writes the parity provider config plus a fixed
// sandbox_mode and approval_policy, the shape both binaries accept.
func writeSandboxParityConfig(t *testing.T, home, serverURL, mode string) {
	t.Helper()
	cfg := "" +
		"model = \"" + parityModelSlug + "\"\n" +
		"model_provider = \"parity\"\n" +
		"approval_policy = \"never\"\n" +
		"sandbox_mode = \"" + mode + "\"\n" +
		"\n" +
		"[model_providers.parity]\n" +
		"name = \"parity\"\n" +
		"base_url = \"" + serverURL + "/v1\"\n" +
		"wire_api = \"responses\"\n" +
		"requires_openai_auth = false\n" +
		"env_key = \"" + fakeEnvKey + "\"\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write sandbox config.toml: %v", err)
	}
}

// captureEnvironmentContext runs `bin exec --json` in cwd with the given
// sandbox_mode and returns the user-message `<environment_context>` text.
func captureEnvironmentContext(t *testing.T, who, bin, cwd, mode string) string {
	t.Helper()
	srv := newCapturingServer(t)
	defer srv.Close()

	home := t.TempDir()
	writeSandboxParityConfig(t, home, srv.URL, mode)
	cmd := exec.Command(bin, "exec", "--json", "--skip-git-repo-check", turnPrompt)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(),
		"CODEX_HOME="+home,
		fakeEnvKey+"="+fakeAPIKey,
		"OPENAI_API_KEY=",
		"CODEX_API_KEY=",
		"CODEX_ACCESS_TOKEN=",
	)
	// Note: TZ is intentionally NOT pinned here. Both binaries resolve the host
	// timezone the same way on the same host, so current_date/timezone agree;
	// this test focuses on the <filesystem> XML. The TZ canonicalization itself
	// (e.g. TZ=UTC -> "GMT") is exercised by TestParityInputContextTimezone.
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s exec (%s) failed: %v\nstderr:\n%s", who, mode, err, stderr.String())
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.requests) == 0 {
		t.Fatalf("%s (%s) sent no /responses request", who, mode)
	}
	var req map[string]json.RawMessage
	if err := json.Unmarshal(srv.requests[0], &req); err != nil {
		t.Fatalf("%s (%s) decode request body: %v", who, mode, err)
	}
	var in []map[string]any
	if err := json.Unmarshal(req["input"], &in); err != nil {
		t.Fatalf("%s (%s) decode input: %v", who, mode, err)
	}
	env := firstTextWithPrefix(in, "<environment_context>")
	if env == "" {
		t.Fatalf("%s (%s) emitted no <environment_context>", who, mode)
	}
	return env
}

// TestParityInputContextSandboxModes asserts codexgo's environment_context is
// byte-identical to codex for each non-default sandbox mode. Both binaries run
// in the SAME fresh temp cwd so the workspace_roots / carveout paths match.
func TestParityInputContextSandboxModes(t *testing.T) {
	refBin := referenceBin(t)
	cgoBin := buildCodexgo(t)

	for _, mode := range []string{"workspace-write", "danger-full-access"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			// A single shared cwd for both binaries so the rendered absolute
			// paths (cwd, workspace_roots, carveouts) are identical.
			cwd := t.TempDir()
			refEnv := captureEnvironmentContext(t, "codex", refBin, cwd, mode)
			cgoEnv := captureEnvironmentContext(t, "codexgo", cgoBin, cwd, mode)
			if refEnv != cgoEnv {
				t.Errorf("environment_context mismatch (%s)\n codex:   %q\n codexgo: %q", mode, refEnv, cgoEnv)
			}
		})
	}
}
