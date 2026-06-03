package rollout

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// writeRolloutItem appends a single rollout item to w as a RolloutLine, stamped
// with the current UTC time at millisecond precision (matching the Rust writer).
func writeRolloutItem(w io.Writer, item RolloutItem) error {
	timestamp := nowUTC().Format(rolloutTimestampLayout)
	line := RolloutLine{Timestamp: timestamp, Item: item}
	return writeJSONLine(w, line)
}

// writeJSONLine serializes value to a single JSON line (no embedded newlines)
// followed by '\n'.
func writeJSONLine(w io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal rollout line: %w", err)
	}
	data = append(data, '\n')
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write rollout line: %w", err)
	}
	return nil
}

// AppendRolloutItemToPath appends one already-filtered rollout item to an
// existing rollout JSONL file.
//
// This is for metadata updates to unloaded threads. Live sessions should use
// RolloutRecorder.RecordCanonicalItems so writes remain ordered with the rest of
// the session stream. Mirrors the Rust `append_rollout_item_to_path`.
func AppendRolloutItemToPath(rolloutPath string, item RolloutItem) error {
	file, err := os.OpenFile(rolloutPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open rollout file for append: %w", err)
	}
	defer file.Close()
	if err := writeRolloutItem(file, item); err != nil {
		return err
	}
	return file.Sync()
}
