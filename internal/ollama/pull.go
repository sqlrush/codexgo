package ollama

// PullEventKind discriminates the variants of PullEvent. It mirrors the variants
// of the Rust PullEvent enum (which derives only Debug/Clone, so it has no wire
// format).
type PullEventKind int

const (
	// PullEventStatus is a human-readable status message (e.g., "verifying",
	// "writing").
	PullEventStatus PullEventKind = iota
	// PullEventChunkProgress is a byte-level progress update for a specific layer
	// digest.
	PullEventChunkProgress
	// PullEventSuccess indicates the pull finished successfully.
	PullEventSuccess
	// PullEventError carries an error message.
	PullEventError
)

// PullEvent is an event emitted while pulling a model from Ollama. It is a Go
// rendering of the Rust PullEvent enum: Kind selects the variant, and the
// remaining fields carry the variant payload.
//
// PullEvent is a value type; construct events with the NewPull* helpers to keep
// callers from setting fields that do not belong to a given variant.
type PullEvent struct {
	// Kind is the variant discriminator.
	Kind PullEventKind
	// Status is the message for Status events and the error message for Error
	// events.
	Status string
	// Digest is the layer digest for ChunkProgress events.
	Digest string
	// Total is the total byte count for ChunkProgress events, when known.
	Total *uint64
	// Completed is the completed byte count for ChunkProgress events, when known.
	Completed *uint64
}

// NewPullStatus builds a Status event.
func NewPullStatus(status string) PullEvent {
	return PullEvent{Kind: PullEventStatus, Status: status}
}

// NewPullChunkProgress builds a ChunkProgress event.
func NewPullChunkProgress(digest string, total, completed *uint64) PullEvent {
	return PullEvent{
		Kind:      PullEventChunkProgress,
		Digest:    digest,
		Total:     total,
		Completed: completed,
	}
}

// NewPullSuccess builds a Success event.
func NewPullSuccess() PullEvent {
	return PullEvent{Kind: PullEventSuccess}
}

// NewPullError builds an Error event carrying msg.
func NewPullError(msg string) PullEvent {
	return PullEvent{Kind: PullEventError, Status: msg}
}

// PullProgressReporter observes pull progress events. Implementations decide how
// to render progress (CLI, TUI, logs, ...). It mirrors the Rust
// PullProgressReporter trait.
type PullProgressReporter interface {
	// OnEvent is invoked once per pull event in stream order.
	OnEvent(event PullEvent) error
}
