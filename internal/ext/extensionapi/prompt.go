package extensionapi

// PromptSlot identifies where a prompt fragment is inserted during prompt
// assembly. Mirrors the Rust `PromptSlot` enum.
type PromptSlot int

// PromptSlot variants.
const (
	// PromptSlotDeveloperPolicy targets the developer-policy slot.
	PromptSlotDeveloperPolicy PromptSlot = iota
	// PromptSlotDeveloperCapabilities targets the developer-capabilities slot.
	PromptSlotDeveloperCapabilities
	// PromptSlotContextualUser targets the contextual-user slot.
	PromptSlotContextualUser
	// PromptSlotSeparateDeveloper targets the separate top-level developer slot.
	PromptSlotSeparateDeveloper
)

// PromptFragment is one model-visible prompt fragment targeting a slot. Mirrors
// the Rust `PromptFragment` struct; the fields are immutable after
// construction.
type PromptFragment struct {
	slot PromptSlot
	text string
}

// NewPromptFragment creates a prompt fragment for the given slot. Mirrors Rust
// `PromptFragment::new`.
func NewPromptFragment(slot PromptSlot, text string) PromptFragment {
	return PromptFragment{slot: slot, text: text}
}

// DeveloperPolicyFragment creates a developer-policy prompt fragment. Mirrors
// Rust `PromptFragment::developer_policy`.
func DeveloperPolicyFragment(text string) PromptFragment {
	return NewPromptFragment(PromptSlotDeveloperPolicy, text)
}

// DeveloperCapabilityFragment creates a developer-capabilities prompt fragment.
// Mirrors Rust `PromptFragment::developer_capability`.
func DeveloperCapabilityFragment(text string) PromptFragment {
	return NewPromptFragment(PromptSlotDeveloperCapabilities, text)
}

// SeparateDeveloperFragment creates a separate top-level developer prompt
// fragment. Mirrors Rust `PromptFragment::separate_developer`.
func SeparateDeveloperFragment(text string) PromptFragment {
	return NewPromptFragment(PromptSlotSeparateDeveloper, text)
}

// Slot returns the target prompt slot. Mirrors Rust `PromptFragment::slot`.
func (f PromptFragment) Slot() PromptSlot {
	return f.slot
}

// Text returns the model-visible text. Mirrors Rust `PromptFragment::text`.
func (f PromptFragment) Text() string {
	return f.text
}
