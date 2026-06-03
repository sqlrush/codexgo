package protocol

// RealtimeAudioFrame is a chunk of realtime audio (base64 PCM in Data). Ported
// from codex-rs/protocol/src/protocol.rs.
type RealtimeAudioFrame struct {
	Data              string  `json:"data"`
	SampleRate        uint32  `json:"sample_rate"`
	NumChannels       uint16  `json:"num_channels"`
	SamplesPerChannel *uint32 `json:"samples_per_channel,omitempty"`
	ItemID            *string `json:"item_id,omitempty"`
}
