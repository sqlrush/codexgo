package msghistory

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
)

// HistoryMetadata fetches the history file's stable identifier (inode on Unix)
// and current entry count, mirroring the Rust `history_metadata`.
//
// It returns (0, 0) when the file does not exist or its metadata cannot be read.
// If metadata succeeds but the file cannot be opened or scanned, it returns
// (logID, 0) so callers can still detect that a history file exists.
func HistoryMetadata(config HistoryConfig) (logID uint64, count int) {
	return historyMetadataForFile(historyFilepath(config))
}

func historyMetadataForFile(path string) (uint64, int) {
	info, err := os.Stat(path)
	if err != nil {
		// NotFound and any other stat error both map to (0, 0).
		return 0, 0
	}
	logID, _ := logIdentity(info)

	file, err := os.Open(path)
	if err != nil {
		return logID, 0
	}
	defer file.Close()

	count := 0
	buf := make([]byte, 8192)
	for {
		n, rerr := file.Read(buf)
		for _, b := range buf[:n] {
			if b == '\n' {
				count++
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				break
			}
			return logID, 0
		}
		if n == 0 {
			break
		}
	}
	return logID, count
}

// Lookup returns a single history entry by file identity and zero-based offset,
// mirroring the Rust `lookup`.
//
// It returns the entry when the current history file's identifier matches logID
// (or logID is 0) and a valid JSON record exists at offset. It returns
// (HistoryEntry{}, false) on any mismatch, I/O error, or parse failure.
//
// The lookup acquires a shared advisory lock and retries on contention; callers
// on a hot path should run it on a dedicated goroutine.
func Lookup(ctx context.Context, logID uint64, offset int, config HistoryConfig) (HistoryEntry, bool) {
	return lookupHistoryEntry(ctx, historyFilepath(config), logID, offset)
}

func lookupHistoryEntry(ctx context.Context, path string, logID uint64, offset int) (HistoryEntry, bool) {
	file, err := os.Open(path)
	if err != nil {
		return HistoryEntry{}, false
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return HistoryEntry{}, false
	}
	currentLogID, ok := logIdentity(info)
	if !ok {
		return HistoryEntry{}, false
	}
	if logID != 0 && currentLogID != logID {
		return HistoryEntry{}, false
	}

	for attempt := 0; attempt < maxRetries; attempt++ {
		if ctx.Err() != nil {
			return HistoryEntry{}, false
		}
		lockErr := tryLockShared(file)
		switch {
		case lockErr == nil:
			defer func() { _ = unlock(file) }()
			return readEntryAtOffset(file, offset)
		case errors.Is(lockErr, errWouldBlock):
			if sleepCtx(ctx, retrySleep) != nil {
				return HistoryEntry{}, false
			}
		default:
			return HistoryEntry{}, false
		}
	}
	return HistoryEntry{}, false
}

func readEntryAtOffset(file *os.File, offset int) (HistoryEntry, bool) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return HistoryEntry{}, false
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	idx := 0
	for scanner.Scan() {
		if idx == offset {
			var entry HistoryEntry
			if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
				return HistoryEntry{}, false
			}
			return entry, true
		}
		idx++
	}
	return HistoryEntry{}, false
}
