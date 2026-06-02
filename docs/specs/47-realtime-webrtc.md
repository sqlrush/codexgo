# 47 — Realtime WebRTC Voice Mode

| | |
|---|---|
| **Phase** | 10 — CLI & peripheral |
| **Status** | Not started |
| **Depends on** | 06 |
| **Size** | L |
| **Drop-in critical** | partial (feature parity) |

## 目标 / Goal
Port `codex-realtime-webrtc`: the realtime/voice mode. Upstream uses native macOS
AVAudioEngine; reimplement cross-platform in Go with `pion/webrtc` per the native
fidelity decision.

## 源参考 / Source reference
- `reference-codex/codex-rs/realtime-webrtc/src/` (`RealtimeWebrtcSession`,
  SDP offer/answer, event stream `Connected`/`LocalAudioLevel`/`Closed`/`Failed`).
- Protocol realtime ops/events in `codex-protocol` (spec 02);
  `tui/src/...` `/realtime` command (spec 39).

## 功能需求 / Functional requirements
1. Realtime session lifecycle: `start()` → SDP offer; `apply_answer_sdp(answer)` to
   complete negotiation; `close()`; event stream (Connected, LocalAudioLevel,
   Closed, Failed) — same surface as Codex.
2. Audio capture/playback (microphone in, speaker out) via a cross-platform Go audio
   layer feeding pion tracks.
3. Wire to the realtime `Op`/`EventMsg` variants (spec 02) and the OpenAI realtime
   endpoint via the API client (spec 06).
4. Graceful unsupported-platform behavior where audio devices are unavailable
   (Codex returns `UnsupportedPlatform` on non-macOS — `codexgo` instead aims for
   cross-platform support; document any platform gaps as deviations).

## 验收方案 / Acceptance criteria
- SDP offer/answer negotiation completes against a mock realtime peer; the event
  stream matches Codex's event surface.
- Audio round-trips through pion tracks on at least macOS + Linux.
- `/realtime` in the TUI starts/stops a session and reflects audio levels.

## 风险与难点 / Risks
- Highest-uncertainty peripheral: cross-platform audio I/O in Go is fragmented (no
  single mature lib); may need platform-specific backends (CoreAudio/ALSA/WASAPI).
- This is also the area where Codex itself is macOS-only — going cross-platform is a
  *superset*; scope carefully and consider deferring beyond first parity release.

## 非目标 / Non-goals
- Replicating macOS-specific AVAudioEngine internals; that's an implementation detail.
