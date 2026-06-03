package feedback

import (
	"fmt"
	"os"
)

// attachment is the internal attachment representation prior to conversion into
// the Sentry SDK attachment type. Mirrors sentry::protocol::Attachment fields
// used by codex.
type attachment struct {
	Buffer      []byte
	Filename    string
	ContentType *string
}

// feedbackAttachments assembles the ordered attachment list for an upload.
// Mirrors Rust `FeedbackSnapshot::feedback_attachments`. The order is:
// codex-logs.log, extra in-memory attachments, connectivity diagnostics, then
// path-backed attachments (unreadable files are skipped).
func (s *FeedbackSnapshot) feedbackAttachments(
	includeLogs bool,
	extraAttachments []FeedbackAttachment,
	extraAttachmentPaths []FeedbackAttachmentPath,
	logsOverride []byte,
) []attachment {
	textPlain := "text/plain"
	var attachments []attachment

	if includeLogs {
		buffer := logsOverride
		if buffer == nil {
			buffer = append([]byte(nil), s.bytes...)
		}
		ct := textPlain
		attachments = append(attachments, attachment{
			Buffer:      buffer,
			Filename:    "codex-logs.log",
			ContentType: &ct,
		})
	}

	for _, a := range extraAttachments {
		attachments = append(attachments, attachment{
			Buffer:      append([]byte(nil), a.Buffer...),
			Filename:    a.Filename,
			ContentType: cloneStringPtr(a.ContentType),
		})
	}

	if text, ok := s.FeedbackDiagnosticsAttachmentText(includeLogs); ok {
		ct := textPlain
		attachments = append(attachments, attachment{
			Buffer:      []byte(text),
			Filename:    FeedbackDiagnosticsAttachmentFilename,
			ContentType: &ct,
		})
	}

	for _, ap := range extraAttachmentPaths {
		data, err := os.ReadFile(ap.Path)
		if err != nil {
			// Mirror codex: log-and-skip unreadable attachments.
			continue
		}
		filename := attachmentFilename(ap)
		ct := textPlain
		attachments = append(attachments, attachment{
			Buffer:      data,
			Filename:    filename,
			ContentType: &ct,
		})
	}

	return attachments
}

func attachmentFilename(ap FeedbackAttachmentPath) string {
	if ap.AttachmentFilenameOverride != nil {
		return *ap.AttachmentFilenameOverride
	}
	base := baseName(ap.Path)
	if base == "" {
		return "extra-log.log"
	}
	return base
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}

func cloneStringPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// displayClassification maps a classification id to its display label. Mirrors
// Rust `display_classification`.
func displayClassification(classification string) string {
	switch classification {
	case "bug":
		return "Bug"
	case "bad_result":
		return "Bad result"
	case "good_result":
		return "Good result"
	case "safety_check":
		return "Safety check"
	default:
		return "Other"
	}
}

// titleFor builds the Sentry event title. Mirrors the format string used in
// Rust `upload_feedback`.
func titleFor(classification, threadID string) string {
	return fmt.Sprintf("[%s]: Codex session %s", displayClassification(classification), threadID)
}
