package analytics

// AcceptedLineFingerprintEventInput collects the fields needed to build an
// accepted-line-fingerprints event. Mirrors Rust
// `AcceptedLineFingerprintEventInput`.
type AcceptedLineFingerprintEventInput struct {
	EventType            string
	TurnID               string
	ThreadID             string
	ProductSurface       *string
	ModelSlug            *string
	CompletedAt          uint64
	RepoHash             *string
	AcceptedAddedLines   uint64
	AcceptedDeletedLines uint64
	LineFingerprints     []AcceptedLineFingerprint
}

// AcceptedLineFingerprintEventRequests builds the upload requests for an
// accepted-line-fingerprints fact. Mirrors Rust
// `accepted_line_fingerprint_event_requests`.
//
// As in codex, local fingerprints are computed but NOT uploaded: the event's
// line_fingerprints is always empty to avoid sending path/line hashes.
func AcceptedLineFingerprintEventRequests(input AcceptedLineFingerprintEventInput) []*CodexAcceptedLineFingerprintsEventRequest {
	return []*CodexAcceptedLineFingerprintsEventRequest{
		{
			EventType: "codex_accepted_line_fingerprints",
			EventParams: CodexAcceptedLineFingerprintsEventParams{
				EventType:            input.EventType,
				TurnID:               input.TurnID,
				ThreadID:             input.ThreadID,
				ProductSurface:       input.ProductSurface,
				ModelSlug:            input.ModelSlug,
				CompletedAt:          input.CompletedAt,
				RepoHash:             input.RepoHash,
				AcceptedAddedLines:   input.AcceptedAddedLines,
				AcceptedDeletedLines: input.AcceptedDeletedLines,
				// Keep computing local fingerprints for parsing/attribution,
				// but do not upload path/line hashes in the event payload.
				LineFingerprints: []AcceptedLineFingerprint{},
			},
		},
	}
}
