package paritytest

// Per-tool-spec parity: even though the FULL advertised tool set still differs
// (codexgo lacks the goals/tool_search tools — tracked in docs/PARITY.md), the
// specs codexgo DOES advertise must be byte-identical to codex's, and the
// advertised ORDER must be a subsequence of codex's spec_plan order. This locks
// in the view_image/update_plan/exec_command/write_stdin/apply_patch schemas so
// a regression fails loudly. Env-gated on CODEX_PARITY_BIN.

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

	// Tools codexgo advertises today that must match codex byte-for-byte. With
	// the UnifiedExec bridge, the PTY pair (exec_command + write_stdin) is the
	// advertised shell family on PTY-capable hosts, exactly like codex.
	for _, name := range []string{"view_image", "update_plan", "exec_command", "write_stdin", "apply_patch"} {
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

// TestParityToolOrder asserts codexgo advertises no tool codex does not, and
// that the advertised names appear in codex's order (an ordered subsequence).
// Once the remaining registry gaps (goals, tool_search) land, this tightens
// into full-array equality.
func TestParityToolOrder(t *testing.T) {
	refBin := referenceBin(t)
	cgoBin := buildCodexgo(t)

	refOrder, _ := capturedTools(t, "codex", refBin)
	cgoOrder, _ := capturedTools(t, "codexgo", cgoBin)

	// Subsequence check: every codexgo tool must appear in codex's list, in the
	// same relative order.
	i := 0
	for _, name := range cgoOrder {
		found := false
		for i < len(refOrder) {
			if refOrder[i] == name {
				found = true
				i++
				break
			}
			i++
		}
		if !found {
			t.Errorf("codexgo advertises %q out of codex order (or codex does not advertise it)\n codex:   %s\n codexgo: %s",
				name, strings.Join(refOrder, ","), strings.Join(cgoOrder, ","))
			break
		}
	}

	if len(cgoOrder) < len(refOrder) {
		missing := missingNames(refOrder, cgoOrder)
		t.Logf("known gap: codexgo advertises %d/%d tools; missing %s",
			len(cgoOrder), len(refOrder), strings.Join(missing, ","))
	}
}

// missingNames returns the names in ref that are absent from got.
func missingNames(ref, got []string) []string {
	have := make(map[string]bool, len(got))
	for _, name := range got {
		have[name] = true
	}
	out := make([]string, 0, len(ref))
	for _, name := range ref {
		if !have[name] {
			out = append(out, name)
		}
	}
	return out
}
