package cli

import "testing"

// TestBundledDefaultModelSlug locks the catalog-derived default model used when
// a real provider client is wired and config.toml sets no model: codex resolves
// it via ModelsManager::get_default_model (the first picker-visible preset),
// which is gpt-5.5 in the bundled 0.136.0 catalog. A regression to the mock
// slug here would make every logged-in session fail with the backend's
// "'gpt-mock' model is not supported" error.
func TestBundledDefaultModelSlug(t *testing.T) {
	got := bundledDefaultModelSlug()
	if got != "gpt-5.5" {
		t.Fatalf("bundledDefaultModelSlug() = %q, want %q", got, "gpt-5.5")
	}
	if got == defaultMockModelSlug {
		t.Fatalf("catalog default must never be the mock slug %q", defaultMockModelSlug)
	}
}
