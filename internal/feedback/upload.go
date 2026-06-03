package feedback

import (
	"fmt"
	"time"

	sentry "github.com/getsentry/sentry-go"
)

// sentryDSN is the Codex feedback DSN. It MUST match codex for drop-in
// compatibility. Mirrors Rust SENTRY_DSN.
const sentryDSN = "https://ae32ed50620d7a7792c1ce5df38b3e3e@o33249.ingest.us.sentry.io/4510195390611458"

// uploadTimeout is the flush timeout for the upload. Mirrors Rust
// UPLOAD_TIMEOUT_SECS.
const uploadTimeout = 10 * time.Second

// reservedTagKeys are tag keys that the snapshot/classification own and that
// client/recorded tags may not override. Mirrors the `reserved` array in Rust
// `upload_tags`.
var reservedTagKeys = map[string]struct{}{
	"thread_id":      {},
	"classification": {},
	"cli_version":    {},
	"session_source": {},
	"reason":         {},
}

// UploadTags computes the final tag map for an upload. Reserved keys are set
// from the snapshot/classification first; client tags then snapshot tags fill
// remaining non-reserved keys without overriding existing entries. Mirrors Rust
// `FeedbackSnapshot::upload_tags`.
func (s *FeedbackSnapshot) UploadTags(
	classification string,
	reason *string,
	clientTags map[string]string,
	sessionSource *string,
) map[string]string {
	tags := map[string]string{
		"thread_id":      s.ThreadID,
		"classification": classification,
		"cli_version":    cliVersion,
	}
	if sessionSource != nil {
		tags["session_source"] = *sessionSource
	}
	if reason != nil {
		tags["reason"] = *reason
	}

	fill := func(src map[string]string) {
		for _, key := range sortedKeys(src) {
			if _, reserved := reservedTagKeys[key]; reserved {
				continue
			}
			if _, exists := tags[key]; exists {
				continue
			}
			tags[key] = src[key]
		}
	}
	fill(clientTags)
	fill(s.tags)

	return tags
}

// cliVersion is the codex CLI version reported in feedback tags. It must match
// the codex release being ported.
const cliVersion = "0.136.0"

// UploadFeedback uploads the snapshot to Sentry with the requested attachments
// and tags. Mirrors Rust `FeedbackSnapshot::upload_feedback`.
//
// The caller must have applied any user-consent gate before setting
// options.IncludeLogs or passing diagnostic attachments.
func (s *FeedbackSnapshot) UploadFeedback(options FeedbackUploadOptions) error {
	client, err := sentry.NewClient(sentry.ClientOptions{Dsn: sentryDSN})
	if err != nil {
		return fmt.Errorf("feedback: build sentry client: %w", err)
	}
	defer client.Flush(uploadTimeout)

	tags := s.UploadTags(options.Classification, options.Reason, options.Tags, options.SessionSource)

	level := sentry.LevelInfo
	switch options.Classification {
	case "bug", "bad_result", "safety_check":
		level = sentry.LevelError
	}

	title := titleFor(options.Classification, s.ThreadID)
	event := sentry.NewEvent()
	event.Level = level
	event.Message = title
	event.Tags = tags

	if options.Reason != nil {
		event.Exception = []sentry.Exception{{
			Type:  title,
			Value: *options.Reason,
		}}
	}

	for _, a := range s.feedbackAttachments(
		options.IncludeLogs,
		options.ExtraAttachments,
		options.ExtraAttachmentPaths,
		options.LogsOverride,
	) {
		contentType := ""
		if a.ContentType != nil {
			contentType = *a.ContentType
		}
		event.Attachments = append(event.Attachments, &sentry.Attachment{
			Filename:    a.Filename,
			ContentType: contentType,
			Payload:     a.Buffer,
		})
	}

	scope := sentry.NewScope()
	client.CaptureEvent(event, nil, scope)
	return nil
}
