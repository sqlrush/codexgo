package msghistory

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// maxRetries bounds the lock-acquisition retry loop, mirroring `MAX_RETRIES`.
const maxRetries = 10

// retrySleep is the backoff between lock attempts, mirroring `RETRY_SLEEP`.
const retrySleep = 100 * time.Millisecond

// fileMode mirrors the Rust `mode(0o600)` owner-only permission bits.
const fileMode os.FileMode = 0o600

// AppendEntry appends a text entry associated with conversationID to the history
// file, mirroring the Rust `append_entry`.
//
// It uses advisory file locking with a retry loop so concurrent writes from
// multiple processes do not interleave. The entry is silently skipped when the
// configured persistence is HistoryPersistenceNone.
//
// It returns an error when the history file cannot be opened/created or the
// exclusive lock cannot be acquired after maxRetries attempts.
func AppendEntry(ctx context.Context, text, conversationID string, config HistoryConfig) error {
	switch config.Persistence {
	case HistoryPersistenceSaveAll:
		// Save everything: proceed.
	case HistoryPersistenceNone:
		return nil
	default:
		// Unknown persistence values fall through to the no-op behavior so
		// callers never accidentally write history they did not ask for.
		return nil
	}

	path := historyFilepath(config)
	if parent := filepath.Dir(path); parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("create history parent dir %q: %w", parent, err)
		}
	}

	// Compute timestamp (seconds since the Unix epoch).
	now := time.Now()
	if now.Before(time.Unix(0, 0)) {
		return fmt.Errorf("system clock before Unix epoch")
	}
	ts := uint64(now.Unix())

	// Construct the JSON line first so we can write it in a single syscall.
	entry := HistoryEntry{
		SessionID: conversationID,
		Ts:        ts,
		Text:      text,
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to serialise history entry: %w", err)
	}
	line := append(encoded, '\n')

	// Open append-only with owner-only permissions, matching the Rust unix arm.
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, fileMode)
	if err != nil {
		return fmt.Errorf("open history file %q: %w", path, err)
	}
	defer file.Close()

	if err := ensureOwnerOnlyPermissions(file); err != nil {
		return err
	}

	return appendUnderLock(ctx, file, line, config.MaxBytes)
}

// appendUnderLock acquires the exclusive advisory lock (retrying on contention),
// writes line at end of file, then enforces the size limit, mirroring the
// blocking body of the Rust `append_entry`.
func appendUnderLock(ctx context.Context, file *os.File, line []byte, maxBytes *uint64) error {
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := tryLockExclusive(file)
		switch {
		case err == nil:
			defer func() { _ = unlock(file) }()
			if _, serr := file.Seek(0, io.SeekEnd); serr != nil {
				return fmt.Errorf("seek history file end: %w", serr)
			}
			if _, werr := file.Write(line); werr != nil {
				return fmt.Errorf("write history entry: %w", werr)
			}
			if serr := file.Sync(); serr != nil {
				return fmt.Errorf("flush history file: %w", serr)
			}
			return enforceHistoryLimit(file, maxBytes)
		case errors.Is(err, errWouldBlock):
			if sleepErr := sleepCtx(ctx, retrySleep); sleepErr != nil {
				return sleepErr
			}
		default:
			return fmt.Errorf("lock history file: %w", err)
		}
	}
	return fmt.Errorf("could not acquire exclusive lock on history file after %d attempts: %w", maxRetries, errWouldBlock)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// enforceHistoryLimit trims the history file to honor maxBytes, dropping the
// oldest lines while holding the write lock so the newest entry is always
// retained. When the file exceeds the hard cap it rewrites the remaining tail to
// the soft cap, mirroring the Rust `enforce_history_limit`.
func enforceHistoryLimit(file *os.File, maxBytes *uint64) error {
	if maxBytes == nil {
		return nil
	}
	limit := *maxBytes
	if limit == 0 {
		return nil
	}

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat history file: %w", err)
	}
	currentLen := uint64(info.Size())
	if currentLen <= limit {
		return nil
	}

	lineLengths, err := scanLineLengths(file)
	if err != nil {
		return err
	}
	if len(lineLengths) == 0 {
		return nil
	}

	lastIndex := len(lineLengths) - 1
	trimTarget := trimTargetBytes(limit, lineLengths[lastIndex])

	var dropBytes uint64
	idx := 0
	for currentLen > trimTarget && idx < lastIndex {
		currentLen = saturatingSub(currentLen, lineLengths[idx])
		dropBytes += lineLengths[idx]
		idx++
	}
	if dropBytes == 0 {
		return nil
	}

	if _, err := file.Seek(int64(dropBytes), io.SeekStart); err != nil {
		return fmt.Errorf("seek history tail: %w", err)
	}
	tail, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("read history tail: %w", err)
	}

	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("truncate history file: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind history file: %w", err)
	}
	if _, err := file.Write(tail); err != nil {
		return fmt.Errorf("rewrite history tail: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("flush trimmed history file: %w", err)
	}
	return nil
}

// scanLineLengths returns the byte length of every newline-terminated line in
// the file, including the trailing newline, mirroring the BufReader::read_line
// loop in the Rust implementation.
func scanLineLengths(file *os.File) ([]uint64, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind history file: %w", err)
	}
	reader := bufio.NewReader(file)
	var lengths []uint64
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			lengths = append(lengths, uint64(len(line)))
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("scan history lines: %w", err)
		}
	}
	return lengths, nil
}

// trimTargetBytes computes the soft-cap trim target, mirroring the Rust
// `trim_target_bytes`.
func trimTargetBytes(maxBytes, newestEntryLen uint64) uint64 {
	softCap := uint64(clampFloat(float64(maxBytes)*historySoftCapRatio, 1.0, float64(maxBytes)))
	if newestEntryLen > softCap {
		return newestEntryLen
	}
	return softCap
}

func clampFloat(v, lo, hi float64) float64 {
	v = float64(int64(v)) // floor for non-negative values
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func saturatingSub(a, b uint64) uint64 {
	if b > a {
		return 0
	}
	return a - b
}
