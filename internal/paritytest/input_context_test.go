package paritytest

// Input context-fragment parity: codexgo seeds the codex initial-context
// messages (the `<permissions instructions>` + `<skills_instructions>` developer
// message and the `<environment_context>` user message) into a new thread's
// history, so the /responses `input` carries them like the reference binary.
// This asserts the item count, per-item roles, and the permissions /
// skills_instructions / environment_context content match codex. Both binaries
// run on the same host with the same cwd and a fresh CODEX_HOME (so the skills
// scan sees exactly the embedded system skills), making the rendered text
// byte-identical. Env-gated on CODEX_PARITY_BIN.

import (
	"encoding/json"
	"strings"
	"testing"
)

// inputMessages captures a binary's /responses request and returns its decoded
// input items plus the per-run CODEX_HOME (for path normalization).
func inputMessages(t *testing.T, who, bin string) ([]map[string]any, string) {
	t.Helper()
	req, home := runPlainTurnCaptureHome(t, who, bin)
	var in []map[string]any
	if err := json.Unmarshal(req["input"], &in); err != nil {
		t.Fatalf("%s decode input: %v", who, err)
	}
	return in, home
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

	ref, refHome := inputMessages(t, "codex", refBin)
	cgo, cgoHome := inputMessages(t, "codexgo", cgoBin)

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

	// 4) skills_instructions content is byte-identical modulo the per-run
	//    CODEX_HOME paths. Both binaries run with a fresh home, so the scan
	//    renders exactly the embedded system skills materialized under
	//    skills/.system.
	refSkills := normalizeHomePaths(firstTextWithPrefix(ref, "<skills_instructions>"), refHome)
	cgoSkills := normalizeHomePaths(firstTextWithPrefix(cgo, "<skills_instructions>"), cgoHome)
	if refSkills == "" {
		t.Fatalf("codex emitted no <skills_instructions>")
	}
	if refSkills != cgoSkills {
		t.Errorf("skills_instructions mismatch (codex %d bytes, codexgo %d bytes)\n codex head: %.300q\n codexgo head: %.300q",
			len(refSkills), len(cgoSkills), refSkills, cgoSkills)
	}
}
