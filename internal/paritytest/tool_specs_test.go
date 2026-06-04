package paritytest

// Per-tool-spec parity: even though the FULL advertised tool set still differs
// (codexgo lacks the goals/tool_search/write_stdin tools — tracked in
// docs/PARITY.md), the specs codexgo DOES advertise must be byte-identical to
// codex's. This locks in the view_image and update_plan schemas (previously empty
// stubs, now ported from create_view_image_tool / plan_spec.rs) so a regression
// fails loudly. Env-gated on CODEX_PARITY_BIN.

import (
	"encoding/json"
	"testing"
)

// toolsByName captures a binary's /responses request and returns a map from tool
// name to the canonical JSON of that tool spec.
func toolsByName(t *testing.T, who, bin string) map[string]string {
	t.Helper()
	req := runPlainTurnCapture(t, who, bin)
	var tools []json.RawMessage
	if err := json.Unmarshal(req["tools"], &tools); err != nil {
		t.Fatalf("%s decode tools: %v", who, err)
	}
	out := make(map[string]string, len(tools))
	for _, raw := range tools {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		name, _ := m["name"].(string)
		if name == "" {
			continue
		}
		out[name] = canonField(t, raw)
	}
	return out
}

// TestParityToolSpecs asserts the built-in tool specs codexgo advertises are
// byte-identical to codex's for the tools both binaries expose.
func TestParityToolSpecs(t *testing.T) {
	refBin := referenceBin(t)
	cgoBin := buildCodexgo(t)

	ref := toolsByName(t, "codex", refBin)
	cgo := toolsByName(t, "codexgo", cgoBin)

	// Tools codexgo advertises today that must match codex byte-for-byte.
	for _, name := range []string{"view_image", "update_plan", "exec_command", "apply_patch"} {
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
