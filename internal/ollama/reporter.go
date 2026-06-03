package ollama

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// digestProgress tracks the latest total/completed byte counts seen for one
// layer digest.
type digestProgress struct {
	total     uint64
	completed uint64
}

// CliProgressReporter is a minimal reporter that writes inline progress to a
// writer (stderr by default). It mirrors the Rust CliProgressReporter, including
// the carriage-return redraw behavior, the suppressed "pulling manifest"
// status, the one-time "Downloading model" header, and the speed estimate.
type CliProgressReporter struct {
	out              io.Writer
	printedHeader    bool
	lastLineLen      int
	lastCompletedSum uint64
	lastInstant      time.Time
	totalsByDigest   map[string]*digestProgress
	now              func() time.Time
}

// NewCliProgressReporter constructs a CliProgressReporter that writes to stderr.
// It mirrors the Rust CliProgressReporter::new.
func NewCliProgressReporter() *CliProgressReporter {
	return NewCliProgressReporterWithWriter(os.Stderr)
}

// NewCliProgressReporterWithWriter constructs a CliProgressReporter that writes
// to out. It exists so callers (and tests) can redirect progress output; the
// default constructor targets stderr exactly as the Rust code does.
func NewCliProgressReporterWithWriter(out io.Writer) *CliProgressReporter {
	now := time.Now
	return &CliProgressReporter{
		out:            out,
		totalsByDigest: make(map[string]*digestProgress),
		lastInstant:    now(),
		now:            now,
	}
}

// OnEvent renders a single pull event, returning any write error. It mirrors the
// Rust CliProgressReporter::on_event.
func (r *CliProgressReporter) OnEvent(event PullEvent) error {
	switch event.Kind {
	case PullEventStatus:
		return r.onStatus(event.Status)
	case PullEventChunkProgress:
		return r.onChunkProgress(event)
	case PullEventError:
		// Handled by the caller; doing nothing here avoids printing it twice.
		return nil
	case PullEventSuccess:
		if _, err := io.WriteString(r.out, "\n"); err != nil {
			return err
		}
		return flush(r.out)
	default:
		return nil
	}
}

func (r *CliProgressReporter) onStatus(status string) error {
	// Avoid noisy manifest messages; otherwise show status inline.
	if strings.EqualFold(status, "pulling manifest") {
		return nil
	}
	pad := saturatingSub(r.lastLineLen, len(status))
	line := fmt.Sprintf("\r%s%s", status, strings.Repeat(" ", pad))
	r.lastLineLen = len(status)
	if _, err := io.WriteString(r.out, line); err != nil {
		return err
	}
	return flush(r.out)
}

func (r *CliProgressReporter) onChunkProgress(event PullEvent) error {
	if event.Total != nil {
		r.entry(event.Digest).total = *event.Total
	}
	if event.Completed != nil {
		r.entry(event.Digest).completed = *event.Completed
	}

	var sumTotal, sumCompleted uint64
	for _, dp := range r.totalsByDigest {
		sumTotal += dp.total
		sumCompleted += dp.completed
	}
	if sumTotal == 0 {
		return nil
	}

	if !r.printedHeader {
		gb := float64(sumTotal) / (1024.0 * 1024.0 * 1024.0)
		header := fmt.Sprintf("Downloading model: total %.2f GB\n", gb)
		if _, err := io.WriteString(r.out, "\r\x1b[2K"); err != nil {
			return err
		}
		if _, err := io.WriteString(r.out, header); err != nil {
			return err
		}
		r.printedHeader = true
	}

	now := r.now()
	dt := now.Sub(r.lastInstant).Seconds()
	if dt < 0.001 {
		dt = 0.001
	}
	dbytes := float64(saturatingSubU64(sumCompleted, r.lastCompletedSum))
	speedMBs := dbytes / (1024.0 * 1024.0) / dt
	r.lastCompletedSum = sumCompleted
	r.lastInstant = now

	doneGB := float64(sumCompleted) / (1024.0 * 1024.0 * 1024.0)
	totalGB := float64(sumTotal) / (1024.0 * 1024.0 * 1024.0)
	pct := float64(sumCompleted) * 100.0 / float64(sumTotal)
	text := fmt.Sprintf("%.2f/%.2f GB (%.1f%%) %.1f MB/s", doneGB, totalGB, pct, speedMBs)
	pad := saturatingSub(r.lastLineLen, len(text))
	line := fmt.Sprintf("\r%s%s", text, strings.Repeat(" ", pad))
	r.lastLineLen = len(text)
	if _, err := io.WriteString(r.out, line); err != nil {
		return err
	}
	return flush(r.out)
}

func (r *CliProgressReporter) entry(digest string) *digestProgress {
	dp, ok := r.totalsByDigest[digest]
	if !ok {
		dp = &digestProgress{}
		r.totalsByDigest[digest] = dp
	}
	return dp
}

// TuiProgressReporter delegates to a CliProgressReporter, keeping UI and CLI
// behavior aligned until a dedicated TUI integration exists. It mirrors the Rust
// TuiProgressReporter newtype.
type TuiProgressReporter struct {
	inner *CliProgressReporter
}

// NewTuiProgressReporter constructs a TuiProgressReporter writing to stderr.
func NewTuiProgressReporter() *TuiProgressReporter {
	return &TuiProgressReporter{inner: NewCliProgressReporter()}
}

// OnEvent forwards the event to the underlying CLI reporter.
func (r *TuiProgressReporter) OnEvent(event PullEvent) error {
	return r.inner.OnEvent(event)
}

// flush flushes the writer when it supports flushing (e.g., bufio.Writer). Plain
// io.Writers such as *os.File need no flush.
func flush(w io.Writer) error {
	if f, ok := w.(interface{ Flush() error }); ok {
		return f.Flush()
	}
	return nil
}

func saturatingSub(a, b int) int {
	if a < b {
		return 0
	}
	return a - b
}

func saturatingSubU64(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}
