package paritytest

// Per-tool-spec parity: codexgo must advertise codex's FULL default tool
// registry (11 tools for a plain gpt-5.x turn) with byte-identical specs in the
// exact spec_plan order. This locks in every advertised schema — the shell PTY
// pair, plan/goal/user-input utilities, apply_patch, view_image, tool_search,
// and the hosted web_search — so a regression fails loudly. Env-gated on
// CODEX_PARITY_BIN.

import (
	"encoding/json"
	"strings"
	"testing"
)

// capturedTools captures a binary's /responses request and returns both the
// advertised tool names in request order and a map from tool name to the
// canonical JSON of that tool spec.
func capturedTools(t *testing.T, who, bin string) ([]string, map[string]string) {
	t.Helper()
	req := runPlainTurnCapture(t, who, bin)
	var tools []json.RawMessage
	if err := json.Unmarshal(req["tools"], &tools); err != nil {
		t.Fatalf("%s decode tools: %v", who, err)
	}
	order := make([]string, 0, len(tools))
	byName := make(map[string]string, len(tools))
	for _, raw := range tools {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		name, _ := m["name"].(string)
		if name == "" {
			// Hosted tools (e.g. web_search) carry only a type tag.
			name, _ = m["type"].(string)
		}
		if name == "" {
			continue
		}
		order = append(order, name)
		byName[name] = canonField(t, raw)
	}
	return order, byName
}

// toolsByName captures a binary's /responses request and returns a map from tool
// name to the canonical JSON of that tool spec.
func toolsByName(t *testing.T, who, bin string) map[string]string {
	t.Helper()
	_, byName := capturedTools(t, who, bin)
	return byName
}

// TestParityToolSpecs asserts the built-in tool specs codexgo advertises are
// byte-identical to codex's for the tools both binaries expose.
func TestParityToolSpecs(t *testing.T) {
	refBin := referenceBin(t)
	cgoBin := buildCodexgo(t)

	ref := toolsByName(t, "codex", refBin)
	cgo := toolsByName(t, "codexgo", cgoBin)

	// Every tool in codex's default plain-turn registry must match codex
	// byte-for-byte: the UnifiedExec PTY pair, the core utility tools (plan,
	// goals trio, request_user_input, apply_patch, view_image), tool_search
	// (deferred multi-agent discovery), and the hosted web_search spec.
	for _, name := range []string{
		"exec_command", "write_stdin",
		"update_plan", "get_goal", "create_goal", "update_goal",
		"request_user_input", "apply_patch", "view_image",
		"tool_search", "web_search",
	} {
		rv, ok := ref[name]
		if !ok {
			t.Fatalf("codex did not advertise %q (got %d tools)", name, len(ref))
		}
		cv, ok := cgo[name]
		if !ok {
			t.Errorf("codexgo did not advertise %q", name)
			continue
		}
		if rv != cv {
			t.Errorf("%q spec mismatch\n codex:   %s\n codexgo: %s", name, rv, cv)
		}
	}
}

// TestParityToolOrder asserts codexgo advertises exactly codex's tool list for
// a plain turn — same names, same order, nothing extra, nothing missing.
func TestParityToolOrder(t *testing.T) {
	refBin := referenceBin(t)
	cgoBin := buildCodexgo(t)

	refOrder, _ := capturedTools(t, "codex", refBin)
	cgoOrder, _ := capturedTools(t, "codexgo", cgoBin)

	if strings.Join(refOrder, ",") != strings.Join(cgoOrder, ",") {
		t.Errorf("advertised tool order mismatch\n codex:   %s\n codexgo: %s",
			strings.Join(refOrder, ","), strings.Join(cgoOrder, ","))
	}
}
