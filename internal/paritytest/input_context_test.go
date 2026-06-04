package paritytest

// Input context-fragment parity: codexgo now seeds the codex initial-context
// messages (`<permissions instructions>` developer message + `<environment_context>`
// user message) into a new thread's history, so the /responses `input` carries them
// like the reference binary. This asserts the item count, per-item roles, and the
// permissions + environment_context content match codex. Both binaries run on the
// same host with the same cwd, so the rendered text (cwd/date/timezone/sandbox) is
// byte-identical.
//
// Remaining tracked sub-gap (docs/PARITY.md): codex's first developer message
// bundles a SECOND content part, `<skills_instructions>` (a scan of SKILL.md
// files), which codexgo does not yet emit. Env-gated on CODEX_PARITY_BIN.

import (
	"encoding/json"
	"strings"
	"testing"
)

// inputMessages captures a binary's /responses request and returns its decoded
// input items.
func inputMessages(t *testing.T, who, bin string) []map[string]any {
	t.Helper()
	req := runPlainTurnCapture(t, who, bin)
	var in []map[string]any
	if err := json.Unmarshal(req["input"], &in); err != nil {
		t.Fatalf("%s decode input: %v", who, err)
	}
	return in
}

// itemRole returns the message role.
func itemRole(it map[string]any) string {
	r, _ := it["role"].(string)
	return r
}

// contentTexts returns the input_text parts of a message.
func contentTexts(it map[string]any) []string {
	c, _ := it["content"].([]any)
	var out []string
	for _, p := range c {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if txt, ok := m["text"].(string); ok {
			out = append(out, txt)
		}
	}
	return out
}

// firstTextWithPrefix returns the first content part across all items that starts
// with prefix, or "".
func firstTextWithPrefix(items []map[string]any, prefix string) string {
	for _, it := range items {
		for _, txt := range contentTexts(it) {
			if strings.HasPrefix(txt, prefix) {
				return txt
			}
		}
	}
	return ""
}

// TestParityInputContext asserts codexgo's input carries the same initial-context
// structure and permissions/environment_context content as codex.
func TestParityInputContext(t *testing.T) {
	refBin := referenceBin(t)
	cgoBin := buildCodexgo(t)

	ref := inputMessages(t, "codex", refBin)
	cgo := inputMessages(t, "codexgo", cgoBin)

	// 1) Same item count and per-item roles.
	if len(ref) != len(cgo) {
		t.Fatalf("input item count mismatch: codex=%d codexgo=%d", len(ref), len(cgo))
	}
	for i := range ref {
		if itemRole(ref[i]) != itemRole(cgo[i]) {
			t.Errorf("input[%d] role mismatch: codex=%q codexgo=%q", i, itemRole(ref[i]), itemRole(cgo[i]))
		}
	}

	// 2) permissions instructions content is byte-identical.
	refPerm := firstTextWithPrefix(ref, "<permissions instructions>")
	cgoPerm := firstTextWithPrefix(cgo, "<permissions instructions>")
	if refPerm == "" {
		t.Fatalf("codex emitted no <permissions instructions>")
	}
	if refPerm != cgoPerm {
		t.Errorf("permissions instructions mismatch\n codex:   %q\n codexgo: %q", refPerm, cgoPerm)
	}

	// 3) environment_context content is byte-identical (same host => same
	//    cwd/date/timezone/sandbox).
	refEnv := firstTextWithPrefix(ref, "<environment_context>")
	cgoEnv := firstTextWithPrefix(cgo, "<environment_context>")
	if refEnv == "" {
		t.Fatalf("codex emitted no <environment_context>")
	}
	if refEnv != cgoEnv {
		t.Errorf("environment_context mismatch\n codex:   %q\n codexgo: %q", refEnv, cgoEnv)
	}

	// 4) Document the remaining skills sub-gap rather than failing on it.
	if skills := firstTextWithPrefix(ref, "<skills_instructions>"); skills != "" {
		if firstTextWithPrefix(cgo, "<skills_instructions>") == "" {
			t.Logf("TRACKED sub-gap (docs/PARITY.md): codex emits <skills_instructions> (%d bytes) that codexgo does not yet render", len(skills))
		}
	}
}
