package protocol

// This file ports the small shared enums from `openai_models.rs` and
// `models.rs` that are referenced by other foundational types (config,
// user input, items). They are string-typed aliases whose values match the
// Rust serde rename_all output.

// ReasoningEffort selects model reasoning effort. Rust: lowercase.
type ReasoningEffort string

const (
	ReasoningEffortNone    ReasoningEffort = "none"
	ReasoningEffortMinimal ReasoningEffort = "minimal"
	ReasoningEffortLow     ReasoningEffort = "low"
	ReasoningEffortMedium  ReasoningEffort = "medium"
	ReasoningEffortHigh    ReasoningEffort = "high"
	ReasoningEffortXHigh   ReasoningEffort = "xhigh"
	// ReasoningEffortMax / ReasoningEffortUltra were added upstream in 0.147.
	// Unknown values are carried verbatim (Rust `Custom(String)`) since the type
	// is a string alias.
	ReasoningEffortMax   ReasoningEffort = "max"
	ReasoningEffortUltra ReasoningEffort = "ultra"
)

// ForRequest maps the configured effort to the value sent on the wire: 0.147
// sends `max` for the `ultra` tier (`reasoning_effort_for_request`); every
// other value passes through unchanged.
func (e ReasoningEffort) ForRequest() ReasoningEffort {
	if e == ReasoningEffortUltra {
		return ReasoningEffortMax
	}
	return e
}

// InputModality is a user-input modality tag advertised by a model. Rust:
// lowercase.
type InputModality string

const (
	InputModalityText  InputModality = "text"
	InputModalityImage InputModality = "image"
)

// DefaultInputModalities returns the backward-compatible default set used when
// `input_modalities` is omitted on the wire.
func DefaultInputModalities() []InputModality {
	return []InputModality{InputModalityText, InputModalityImage}
}

// ImageDetail selects the level of detail for an image attachment. Rust:
// lowercase.
type ImageDetail string

const (
	ImageDetailAuto     ImageDetail = "auto"
	ImageDetailLow      ImageDetail = "low"
	ImageDetailHigh     ImageDetail = "high"
	ImageDetailOriginal ImageDetail = "original"
)

// DefaultImageDetail is the default image detail (Rust DEFAULT_IMAGE_DETAIL).
const DefaultImageDetail = ImageDetailHigh

// MessagePhase classifies an assistant message as interim commentary or final
// answer text. Rust: snake_case. Callers must treat a nil/absent phase as
// "phase unknown" and preserve legacy completion semantics.
type MessagePhase string

const (
	MessagePhaseCommentary  MessagePhase = "commentary"
	MessagePhaseFinalAnswer MessagePhase = "final_answer"
)
