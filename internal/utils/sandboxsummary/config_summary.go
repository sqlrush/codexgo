package sandboxsummary

// Entry is a single key/value pair in a configuration summary.
//
// The Rust source returns `Vec<(&'static str, String)>`; this struct is the Go
// equivalent of one tuple. Keys are fixed string literals matching the Rust
// source exactly.
type Entry struct {
	// Key is the stable, human-facing label (e.g. "workdir", "sandbox").
	Key string
	// Value is the rendered value for the key.
	Value string
}

// WireResponses is the wire-api value that triggers the additional reasoning
// entries. It mirrors `WireApi::Responses`, whose Display form is "responses".
const WireResponses = "responses"

// reasoningNone is the placeholder used when a reasoning field is absent,
// mirroring Rust's `unwrap_or_else(|| "none".to_string())`.
const reasoningNone = "none"

// ConfigSummary holds the already-resolved configuration values that
// CreateConfigSummaryEntries formats.
//
// In the Rust source these are read from `Config`: cwd, the selected model,
// provider id, approval policy Display, the effective legacy sandbox policy,
// the provider's wire API, and the optional reasoning effort / summary Display
// values. Resolving those lives in the (separately ported) config and protocol
// crates; this package only formats them, so the caller passes the resolved
// values directly.
//
// The optional reasoning fields use *string so absence ("none") is
// distinguishable from an explicit value. They are consulted only when WireAPI
// equals WireResponses.
type ConfigSummary struct {
	// Workdir is the configured working directory in display string form
	// (Rust: config.cwd.display()).
	Workdir string

	// Provider is the model provider id (Rust: config.model_provider_id).
	Provider string

	// ApprovalPolicy is the Display form of the approval policy
	// (Rust: config.permissions.approval_policy.value()). Expected kebab-case
	// values: "untrusted", "on-failure", "on-request", "granular", "never".
	ApprovalPolicy string

	// SandboxPolicy is the effective legacy sandbox policy whose summary is
	// rendered via SummarizeSandboxPolicy.
	SandboxPolicy SandboxPolicy

	// WireAPI is the Display form of the provider's wire API. When it equals
	// WireResponses the reasoning entries are appended.
	WireAPI string

	// ReasoningEffort is the Display form of config.model_reasoning_effort, or
	// nil when unset.
	ReasoningEffort *string

	// ReasoningSummary is the Display form of config.model_reasoning_summary,
	// or nil when unset.
	ReasoningSummary *string
}

// CreateConfigSummaryEntries builds an ordered list of key/value pairs
// summarizing the effective configuration.
//
// It is a faithful port of the Rust `create_config_summary_entries`. The
// entries, their order, and their keys are identical:
//
//	workdir, model, provider, approval, sandbox
//	[reasoning effort, reasoning summaries]  (only when WireAPI == responses)
//
// The model argument is the active model name, matching the Rust function's
// separate `model` parameter. The returned slice is freshly allocated on every
// call; the input ConfigSummary is never mutated.
func CreateConfigSummaryEntries(config ConfigSummary, model string) []Entry {
	entries := []Entry{
		{Key: "workdir", Value: config.Workdir},
		{Key: "model", Value: model},
		{Key: "provider", Value: config.Provider},
		{Key: "approval", Value: config.ApprovalPolicy},
		{Key: "sandbox", Value: SummarizeSandboxPolicy(config.SandboxPolicy)},
	}

	if config.WireAPI == WireResponses {
		entries = append(entries,
			Entry{Key: "reasoning effort", Value: reasoningValue(config.ReasoningEffort)},
			Entry{Key: "reasoning summaries", Value: reasoningValue(config.ReasoningSummary)},
		)
	}

	return entries
}

// reasoningValue returns the dereferenced value, or "none" when the pointer is
// nil, mirroring Rust's `map(...).unwrap_or_else(|| "none".to_string())`.
func reasoningValue(value *string) string {
	if value == nil {
		return reasoningNone
	}
	return *value
}
