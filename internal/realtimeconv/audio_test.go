package realtimeconv

import (
	"encoding/base64"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func u32(v uint32) *uint32 { return &v }

func TestAudioDurationMS(t *testing.T) {
	tests := []struct {
		name  string
		frame protocol.RealtimeAudioFrame
		want  uint32
	}{
		{
			name:  "declared samples",
			frame: protocol.RealtimeAudioFrame{SampleRate: 24000, SamplesPerChannel: u32(24000)},
			want:  1000,
		},
		{
			name:  "half second",
			frame: protocol.RealtimeAudioFrame{SampleRate: 48000, SamplesPerChannel: u32(24000)},
			want:  500,
		},
		{
			name:  "zero rate clamps to one",
			frame: protocol.RealtimeAudioFrame{SampleRate: 0, SamplesPerChannel: u32(5)},
			want:  5000,
		},
		{
			name: "decoded from base64 mono 16-bit",
			// 4 bytes -> 2 samples (16-bit) -> 1 channel -> 2 samples/channel.
			frame: protocol.RealtimeAudioFrame{
				SampleRate:  1000,
				NumChannels: 1,
				Data:        base64.StdEncoding.EncodeToString([]byte{0, 0, 0, 0}),
			},
			want: 2,
		},
		{
			name: "decoded stereo",
			// 8 bytes -> 4 samples -> 2 channels -> 2 samples/channel.
			frame: protocol.RealtimeAudioFrame{
				SampleRate:  1000,
				NumChannels: 2,
				Data:        base64.StdEncoding.EncodeToString([]byte{0, 0, 0, 0, 0, 0, 0, 0}),
			},
			want: 2,
		},
		{
			name:  "no samples, bad base64",
			frame: protocol.RealtimeAudioFrame{SampleRate: 1000, Data: "!!!notbase64"},
			want:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := tt.frame
			if got := audioDurationMS(&frame); got != tt.want {
				t.Fatalf("audioDurationMS = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestUpdateOutputAudioState(t *testing.T) {
	frame := func(item string, samples uint32) *protocol.RealtimeAudioFrame {
		id := item
		return &protocol.RealtimeAudioFrame{
			SampleRate:        1000,
			SamplesPerChannel: u32(samples),
			ItemID:            &id,
		}
	}

	t.Run("nil for frame without item id", func(t *testing.T) {
		got := updateOutputAudioState(nil, &protocol.RealtimeAudioFrame{SampleRate: 1000, SamplesPerChannel: u32(10)})
		if got != nil {
			t.Fatalf("expected nil state, got %+v", got)
		}
	})

	t.Run("nil for zero duration", func(t *testing.T) {
		got := updateOutputAudioState(nil, frame("a", 0))
		if got != nil {
			t.Fatalf("expected nil state, got %+v", got)
		}
	})

	t.Run("starts fresh state", func(t *testing.T) {
		// 1000 samples / 1000 Hz * 1000 = 1000ms
		got := updateOutputAudioState(nil, frame("a", 1000))
		if got == nil || got.itemID != "a" || got.audioEndMS != 1000 {
			t.Fatalf("unexpected state %+v", got)
		}
	})

	t.Run("accumulates same item", func(t *testing.T) {
		state := &outputAudioState{itemID: "a", audioEndMS: 1000}
		got := updateOutputAudioState(state, frame("a", 500))
		if got.audioEndMS != 1500 {
			t.Fatalf("audioEndMS = %d, want 1500", got.audioEndMS)
		}
		// Original must be unchanged (immutability).
		if state.audioEndMS != 1000 {
			t.Fatalf("original mutated: %d", state.audioEndMS)
		}
	})

	t.Run("resets on different item", func(t *testing.T) {
		state := &outputAudioState{itemID: "a", audioEndMS: 1000}
		got := updateOutputAudioState(state, frame("b", 500))
		if got.itemID != "b" || got.audioEndMS != 500 {
			t.Fatalf("unexpected state %+v", got)
		}
	})
}

func TestSaturatingAddU32(t *testing.T) {
	max := ^uint32(0)
	if got := saturatingAddU32(1, 2); got != 3 {
		t.Fatalf("got %d", got)
	}
	if got := saturatingAddU32(max, 5); got != max {
		t.Fatalf("expected saturation, got %d", got)
	}
}
