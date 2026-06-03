package realtimewebrtc

import "math"

// maxPeak is i16::MAX, the upper bound used by the Rust audio_level_to_peak
// conversion. Local audio peaks are reported in [0, maxPeak].
const maxPeak = math.MaxInt16

// audioLevelToPeak converts a linear WebRTC audio level in [0, 1] into a peak
// value in [0, 32767], matching the Rust implementation:
//
//	(audio_level.clamp(0.0, 1.0) * i16::MAX as f64).round() as u16
func audioLevelToPeak(audioLevel float64) uint16 {
	clamped := math.Max(0.0, math.Min(1.0, audioLevel))
	return uint16(math.Round(clamped * float64(maxPeak)))
}
