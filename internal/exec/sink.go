package exec

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// JSONLSink renders each [ThreadEvent] as a single JSON line, matching codex
// exec's --json output. On shutdown it optionally writes the final agent message
// to a file. It is the Go analogue of EventProcessorWithJsonOutput's emit path.
type JSONLSink struct {
	out             io.Writer
	errOut          io.Writer
	lastMessagePath string
}

// NewJSONLSink builds a JSONL sink writing events to out, warnings to errOut, and
// (when lastMessagePath is non-empty) the final message to that file on finish.
func NewJSONLSink(out, errOut io.Writer, lastMessagePath string) *JSONLSink {
	return &JSONLSink{out: out, errOut: errOut, lastMessagePath: lastMessagePath}
}

// Emit writes one event as a JSON line. A serialization failure is itself
// emitted as an error event so the stream stays well-formed (mirroring the Rust
// emit fallback).
func (s *JSONLSink) Emit(ev ThreadEvent) {
	b, err := json.Marshal(ev)
	if err != nil {
		fallback, _ := json.Marshal(map[string]string{
			"type":    string(ThreadEventKindError),
			"message": fmt.Sprintf("failed to serialize exec json event: %v", err),
		})
		fmt.Fprintln(s.out, string(fallback))
		return
	}
	fmt.Fprintln(s.out, string(b))
}

// Warn writes a local warning as an error item event (the JSONL stream has no
// out-of-band warning channel; the Rust process_warning maps to an error item).
func (s *JSONLSink) Warn(message string) {
	s.Emit(ThreadEvent{
		Kind: ThreadEventKindItemCompleted,
		ItemCompleted: &ItemEvent{Item: ThreadItem{
			ID:      warningItemID,
			Details: ThreadItemDetails{Kind: ThreadItemDetailKindError, Error: &ErrorItem{Message: message}},
		}},
	})
}

// Finish writes the final message to the last-message file when configured and a
// successful turn produced one.
func (s *JSONLSink) Finish(finalMessage *string, emitFinal bool) {
	if emitFinal && s.lastMessagePath != "" {
		writeLastMessageFile(s.lastMessagePath, finalMessage, s.errOut)
	}
}

// warningItemID is the synthetic id used for JSONL warning items.
const warningItemID = "warning"

// TextSink renders a human-readable summary: progress to stderr and the final
// agent message to stdout. It is the reduced Go analogue of
// EventProcessorWithHumanOutput. The full ANSI styling is intentionally omitted;
// the wire-faithful surface (the final message on stdout, the last-message file)
// is preserved.
type TextSink struct {
	out             io.Writer
	errOut          io.Writer
	lastMessagePath string
}

// NewTextSink builds a text sink writing the final message to out and progress
// to errOut.
func NewTextSink(out, errOut io.Writer, lastMessagePath string) *TextSink {
	return &TextSink{out: out, errOut: errOut, lastMessagePath: lastMessagePath}
}

// Emit renders progress lines to stderr for the events a human cares about.
func (s *TextSink) Emit(ev ThreadEvent) {
	switch ev.Kind {
	case ThreadEventKindItemStarted:
		s.renderItem(ev.ItemStarted, false)
	case ThreadEventKindItemCompleted:
		s.renderItem(ev.ItemCompleted, true)
	case ThreadEventKindError:
		if ev.Error != nil {
			fmt.Fprintln(s.errOut, "ERROR:", ev.Error.Message)
		}
	case ThreadEventKindTurnFailed:
		if ev.TurnFailed != nil {
			fmt.Fprintln(s.errOut, "ERROR:", ev.TurnFailed.Error.Message)
		}
	}
}

// renderItem prints a one-line summary of an item for the human view.
func (s *TextSink) renderItem(ie *ItemEvent, completed bool) {
	if ie == nil {
		return
	}
	switch ie.Item.Details.Kind {
	case ThreadItemDetailKindCommandExecution:
		ce := ie.Item.Details.CommandExecution
		if !completed {
			fmt.Fprintln(s.errOut, "exec", ce.Command)
		}
	case ThreadItemDetailKindFileChange:
		if !completed {
			fmt.Fprintln(s.errOut, "apply patch")
		}
	case ThreadItemDetailKindWebSearch:
		if !completed {
			fmt.Fprintln(s.errOut, "web search:", ie.Item.Details.WebSearch.Query)
		}
	case ThreadItemDetailKindReasoning:
		if completed {
			fmt.Fprintln(s.errOut, ie.Item.Details.Reasoning.Text)
		}
	case ThreadItemDetailKindError:
		if completed {
			fmt.Fprintln(s.errOut, ie.Item.Details.Error.Message)
		}
	}
}

// Warn prints a local warning to stderr.
func (s *TextSink) Warn(message string) {
	fmt.Fprintln(s.errOut, "warning:", message)
}

// Finish writes the last-message file when configured and prints the final agent
// message to stdout (the automation-friendly contract: the final answer lands on
// stdout).
func (s *TextSink) Finish(finalMessage *string, emitFinal bool) {
	if emitFinal && s.lastMessagePath != "" {
		writeLastMessageFile(s.lastMessagePath, finalMessage, s.errOut)
	}
	if emitFinal && finalMessage != nil {
		fmt.Fprintln(s.out, *finalMessage)
	}
}

// writeLastMessageFile writes the final message (empty when nil) to path,
// warning on stderr when the message was absent or the write failed. It mirrors
// event_processor::handle_last_message.
func writeLastMessageFile(path string, message *string, errOut io.Writer) {
	content := ""
	if message != nil {
		content = *message
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fmt.Fprintf(errOut, "Failed to write last message file %q: %v\n", path, err)
		return
	}
	if message == nil {
		fmt.Fprintf(errOut, "Warning: no last agent message; wrote empty content to %s\n", path)
	}
}
