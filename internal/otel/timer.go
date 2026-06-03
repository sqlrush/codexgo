package otel

import "time"

// Timer measures elapsed time and records it as a duration histogram. Mirrors
// Rust `Timer`. Unlike Rust there is no Drop; call [Timer.Record] explicitly.
type Timer struct {
	name      string
	tags      []Tag
	client    *MetricsClient
	startTime time.Time
}

func newTimer(name string, tags []Tag, client *MetricsClient) *Timer {
	cloned := make([]Tag, len(tags))
	copy(cloned, tags)
	return &Timer{
		name:      name,
		tags:      cloned,
		client:    client,
		startTime: time.Now(),
	}
}

// Record records the elapsed duration with optional additional tags. The
// additional tags are placed first, then the timer's tags, matching Rust
// `Timer::record`.
func (t *Timer) Record(additionalTags []Tag) error {
	tags := make([]Tag, 0, len(t.tags)+len(additionalTags))
	tags = append(tags, additionalTags...)
	tags = append(tags, t.tags...)
	return t.client.RecordDuration(t.name, time.Since(t.startTime), tags)
}
