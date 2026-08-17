package realtimeconv

import (
	"encoding/base64"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// outputAudioState tracks the cumulative duration of model audio for the current
// output item so a barge-in (user speech) can truncate it precisely. Mirrors the
// Rust OutputAudioState.
type outputAudioState struct {
	itemID     string
	audioEndMS uint32
}

// updateOutputAudioState folds a model audio frame into the running output-audio
// duration, mirroring the Rust update_output_audio_state. It is a no-op for
// frames without an item id or with zero measurable duration. When the frame's
// item id matches the current state the duration accumulates (saturating);
// otherwise a fresh state begins.
func updateOutputAudioState(state *outputAudioState, frame *protocol.RealtimeAudioFrame) *outputAudioState {
	if frame == nil || frame.ItemID == nil {
		return state
	}
	itemID := *frame.ItemID
	audioEndMS := audioDurationMS(frame)
	if audioEndMS == 0 {
		return state
	}

	if state != nil && state.itemID == itemID {
		return &outputAudioState{
			itemID:     state.itemID,
			audioEndMS: saturatingAddU32(state.audioEndMS, audioEndMS),
		}
	}
	return &outputAudioState{itemID: itemID, audioEndMS: audioEndMS}
}

// audioDurationMS computes a frame's duration in milliseconds from its declared
// or decoded samples-per-channel and sample rate. Mirrors the Rust
// audio_duration_ms.
func audioDurationMS(frame *protocol.RealtimeAudioFrame) uint32 {
	samplesPerChannel, ok := framesPerChannel(frame)
	if !ok {
		return 0
	}
	sampleRate := uint64(frame.SampleRate)
	if sampleRate < 1 {
		sampleRate = 1
	}
	return uint32((uint64(samplesPerChannel) * 1000) / sampleRate)
}

// framesPerChannel resolves the per-channel sample count, preferring the
// declared value and otherwise decoding the base64 PCM payload. Mirrors the Rust
// frame.samples_per_channel.or(decoded_samples_per_channel(frame)).
func framesPerChannel(frame *protocol.RealtimeAudioFrame) (uint32, bool) {
	if frame.SamplesPerChannel != nil {
		return *frame.SamplesPerChannel, true
	}
	return decodedSamplesPerChannel(frame)
}

// decodedSamplesPerChannel derives the per-channel sample count from a base64
// PCM payload assuming 16-bit (2-byte) samples. Mirrors the Rust
// decoded_samples_per_channel; returns ok=false on decode failure, zero
// channels, or overflow.
func decodedSamplesPerChannel(frame *protocol.RealtimeAudioFrame) (uint32, bool) {
	bytes, err := base64.StdEncoding.DecodeString(frame.Data)
	if err != nil {
		return 0, false
	}
	channels := int(frame.NumChannels)
	if channels < 1 {
		channels = 1
	}
	samples := (len(bytes) / 2) / channels
	if samples < 0 || uint64(samples) > uint64(^uint32(0)) {
		return 0, false
	}
	return uint32(samples), true
}

// saturatingAddU32 adds two uint32 values, clamping at the maximum rather than
// wrapping, matching the Rust saturating_add.
func saturatingAddU32(a, b uint32) uint32 {
	sum := uint64(a) + uint64(b)
	if sum > uint64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(sum)
}
