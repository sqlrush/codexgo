package appserverproto

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func boolPtr(b bool) *bool    { return &b }
func u32Ptr(v uint32) *uint32 { return &v }
func u64Ptr(v uint64) *uint64 { return &v }
func i64Ptr(v int64) *int64   { return &v }

// roundTrip marshals val, asserts it matches want byte-for-byte (after JSON key
// canonicalization), then unmarshals into a fresh value and re-marshals to
// confirm a stable round trip.
func roundTrip[T any](t *testing.T, val T, want string) {
	t.Helper()
	b, err := json.Marshal(val)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !jsonEqual(t, b, []byte(want)) {
		t.Fatalf("marshal = %s, want %s", b, want)
	}
	var back T
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	reb, err := json.Marshal(back)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !jsonEqual(t, reb, []byte(want)) {
		t.Fatalf("round-trip = %s, want %s", reb, want)
	}
}

func TestFsParamsAndResponsesRoundTrip(t *testing.T) {
	roundTrip(t, FsReadFileParams{Path: "/tmp/example.txt"},
		`{"path":"/tmp/example.txt"}`)
	roundTrip(t, FsReadFileResponse{DataBase64: "aGVsbG8="},
		`{"dataBase64":"aGVsbG8="}`)
	roundTrip(t, FsWriteFileParams{Path: "/tmp/example.bin", DataBase64: "AAE="},
		`{"path":"/tmp/example.bin","dataBase64":"AAE="}`)
	roundTrip(t, FsWriteFileResponse{}, `{}`)

	// recursive is always emitted (null when absent).
	roundTrip(t, FsCreateDirectoryParams{Path: "/tmp/example", Recursive: nil},
		`{"path":"/tmp/example","recursive":null}`)
	roundTrip(t, FsCreateDirectoryParams{Path: "/tmp/example", Recursive: boolPtr(false)},
		`{"path":"/tmp/example","recursive":false}`)
	roundTrip(t, FsCreateDirectoryResponse{}, `{}`)

	roundTrip(t, FsGetMetadataParams{Path: "/p"}, `{"path":"/p"}`)
	roundTrip(t, FsGetMetadataResponse{
		IsDirectory: false, IsFile: true, IsSymlink: false,
		CreatedAtMs: 123, ModifiedAtMs: 456,
	}, `{"isDirectory":false,"isFile":true,"isSymlink":false,"createdAtMs":123,"modifiedAtMs":456}`)

	roundTrip(t, FsReadDirectoryParams{Path: "/d"}, `{"path":"/d"}`)
	roundTrip(t, FsReadDirectoryResponse{Entries: []FsReadDirectoryEntry{
		{FileName: "a.txt", IsDirectory: false, IsFile: true},
	}}, `{"entries":[{"fileName":"a.txt","isDirectory":false,"isFile":true}]}`)

	// recursive and force always emitted (null when absent).
	roundTrip(t, FsRemoveParams{Path: "/x"},
		`{"path":"/x","recursive":null,"force":null}`)
	roundTrip(t, FsRemoveResponse{}, `{}`)

	// recursive omitted when false (skip_serializing_if Not::not).
	roundTrip(t, FsCopyParams{SourcePath: "/a", DestinationPath: "/b"},
		`{"sourcePath":"/a","destinationPath":"/b"}`)
	roundTrip(t, FsCopyParams{SourcePath: "/a", DestinationPath: "/b", Recursive: true},
		`{"sourcePath":"/a","destinationPath":"/b","recursive":true}`)
	roundTrip(t, FsCopyResponse{}, `{}`)

	roundTrip(t, FsWatchParams{WatchID: "w1", Path: "/w"},
		`{"watchId":"w1","path":"/w"}`)
	roundTrip(t, FsWatchResponse{Path: "/w"}, `{"path":"/w"}`)
	roundTrip(t, FsUnwatchParams{WatchID: "w1"}, `{"watchId":"w1"}`)
	roundTrip(t, FsUnwatchResponse{}, `{}`)

	roundTrip(t, FsChangedNotification{
		WatchID:      "0195ec6b",
		ChangedPaths: []protocol.AbsolutePath{"/repo/.git/HEAD", "/repo/.git/FETCH_HEAD"},
	}, `{"watchId":"0195ec6b","changedPaths":["/repo/.git/HEAD","/repo/.git/FETCH_HEAD"]}`)
}

func TestProcessParamsRoundTrip(t *testing.T) {
	// All optional/false fields: tty/streamStdin/streamStdoutStderr omitted,
	// outputBytesCap/timeoutMs (double_option) omitted, env/size emitted null.
	roundTrip(t, ProcessSpawnParams{
		Command:       []string{"sleep", "30"},
		ProcessHandle: "sleep-1",
		Cwd:           "/readable",
	}, `{"command":["sleep","30"],"processHandle":"sleep-1","cwd":"/readable","env":null,"size":null}`)

	// double_option set to null disables the cap/timeout (key present, null).
	roundTrip(t, ProcessSpawnParams{
		Command:        []string{"sleep", "30"},
		ProcessHandle:  "sleep-1",
		Cwd:            "/readable",
		OutputBytesCap: DoubleOption[uint64]{Present: true, Set: false},
		TimeoutMs:      DoubleOption[int64]{Present: true, Set: false},
	}, `{"command":["sleep","30"],"processHandle":"sleep-1","cwd":"/readable","outputBytesCap":null,"timeoutMs":null,"env":null,"size":null}`)

	// double_option set to a value.
	roundTrip(t, ProcessSpawnParams{
		Command:        []string{"sleep", "30"},
		ProcessHandle:  "sleep-1",
		Cwd:            "/readable",
		OutputBytesCap: NewDoubleOptionValue[uint64](123),
		TimeoutMs:      NewDoubleOptionValue[int64](456),
	}, `{"command":["sleep","30"],"processHandle":"sleep-1","cwd":"/readable","outputBytesCap":123,"timeoutMs":456,"env":null,"size":null}`)

	roundTrip(t, ProcessSpawnResponse{}, `{}`)

	// deltaBase64 always emitted (null), closeStdin omitted when false.
	roundTrip(t, ProcessWriteStdinParams{ProcessHandle: "proc-7", CloseStdin: true},
		`{"processHandle":"proc-7","deltaBase64":null,"closeStdin":true}`)
	roundTrip(t, ProcessWriteStdinParams{ProcessHandle: "proc-7", DeltaBase64: strPtr("AAE=")},
		`{"processHandle":"proc-7","deltaBase64":"AAE="}`)
	roundTrip(t, ProcessWriteStdinResponse{}, `{}`)

	roundTrip(t, ProcessKillParams{ProcessHandle: "proc-7"}, `{"processHandle":"proc-7"}`)
	roundTrip(t, ProcessKillResponse{}, `{}`)

	roundTrip(t, ProcessResizePtyParams{
		ProcessHandle: "proc-7",
		Size:          ProcessTerminalSize{Rows: 50, Cols: 160},
	}, `{"processHandle":"proc-7","size":{"rows":50,"cols":160}}`)
	roundTrip(t, ProcessResizePtyResponse{}, `{}`)
}

func TestProcessNotificationsRoundTrip(t *testing.T) {
	roundTrip(t, ProcessOutputDeltaNotification{
		ProcessHandle: "proc-1", Stream: ProcessOutputStreamStdout,
		DeltaBase64: "AQI=", CapReached: false,
	}, `{"processHandle":"proc-1","stream":"stdout","deltaBase64":"AQI=","capReached":false}`)

	roundTrip(t, ProcessExitedNotification{
		ProcessHandle: "proc-1", ExitCode: 0,
		Stdout: "out", StdoutCapReached: false,
		Stderr: "err", StderrCapReached: true,
	}, `{"processHandle":"proc-1","exitCode":0,"stdout":"out","stdoutCapReached":false,"stderr":"err","stderrCapReached":true}`)
}

func TestCommandExecParamsRoundTrip(t *testing.T) {
	// disableOutputCap variant: optional Option fields emit null (no double_option).
	roundTrip(t, CommandExecParams{
		Command:            []string{"yes"},
		ProcessID:          strPtr("yes-1"),
		StreamStdoutStderr: true,
		DisableOutputCap:   true,
	}, `{"command":["yes"],"processId":"yes-1","streamStdoutStderr":true,"outputBytesCap":null,"disableOutputCap":true,"timeoutMs":null,"cwd":null,"env":null,"size":null,"sandboxPolicy":null,"permissionProfile":null}`)

	// disableTimeout variant.
	roundTrip(t, CommandExecParams{
		Command:        []string{"sleep", "30"},
		ProcessID:      strPtr("sleep-1"),
		DisableTimeout: true,
	}, `{"command":["sleep","30"],"processId":"sleep-1","disableTimeout":true,"timeoutMs":null,"cwd":null,"env":null,"size":null,"sandboxPolicy":null,"permissionProfile":null,"outputBytesCap":null}`)

	// tty + size variant.
	roundTrip(t, CommandExecParams{
		Command:   []string{"top"},
		ProcessID: strPtr("pty-1"),
		TTY:       true,
		Size:      &CommandExecTerminalSize{Rows: 40, Cols: 120},
	}, `{"command":["top"],"processId":"pty-1","tty":true,"outputBytesCap":null,"timeoutMs":null,"cwd":null,"env":null,"size":{"rows":40,"cols":120},"sandboxPolicy":null,"permissionProfile":null}`)

	// explicit timeoutMs + cwd value.
	roundTrip(t, CommandExecParams{
		Command:   []string{"ls", "-la"},
		TimeoutMs: i64Ptr(1000),
		Cwd:       strPtr("/tmp"),
	}, `{"command":["ls","-la"],"processId":null,"outputBytesCap":null,"timeoutMs":1000,"cwd":"/tmp","env":null,"size":null,"sandboxPolicy":null,"permissionProfile":null}`)

	// outputBytesCap value.
	roundTrip(t, CommandExecParams{
		Command:        []string{"cat"},
		OutputBytesCap: u64Ptr(2048),
	}, `{"command":["cat"],"processId":null,"outputBytesCap":2048,"timeoutMs":null,"cwd":null,"env":null,"size":null,"sandboxPolicy":null,"permissionProfile":null}`)
}

func TestCommandExecDeserializeDefaults(t *testing.T) {
	// A minimal payload should populate defaults (false bools, nil options).
	var p CommandExecParams
	if err := json.Unmarshal([]byte(`{"command":["ls","-la"],"timeoutMs":1000,"cwd":"/tmp"}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.TTY || p.StreamStdin || p.StreamStdoutStderr || p.DisableOutputCap || p.DisableTimeout {
		t.Fatalf("expected all bool flags false, got %+v", p)
	}
	if p.ProcessID != nil || p.OutputBytesCap != nil || p.Env != nil || p.Size != nil ||
		p.SandboxPolicy != nil || p.PermissionProfile != nil {
		t.Fatalf("expected nil optionals, got %+v", p)
	}
	if p.TimeoutMs == nil || *p.TimeoutMs != 1000 {
		t.Fatalf("timeoutMs: want 1000, got %v", p.TimeoutMs)
	}
	if p.Cwd == nil || *p.Cwd != "/tmp" {
		t.Fatalf("cwd: want /tmp, got %v", p.Cwd)
	}
}

func TestCommandExecControlRoundTrip(t *testing.T) {
	roundTrip(t, CommandExecResponse{ExitCode: 1, Stdout: "o", Stderr: "e"},
		`{"exitCode":1,"stdout":"o","stderr":"e"}`)

	roundTrip(t, CommandExecWriteParams{ProcessID: "p", CloseStdin: true},
		`{"processId":"p","deltaBase64":null,"closeStdin":true}`)
	roundTrip(t, CommandExecWriteResponse{}, `{}`)

	roundTrip(t, CommandExecTerminateParams{ProcessID: "p"}, `{"processId":"p"}`)
	roundTrip(t, CommandExecTerminateResponse{}, `{}`)

	roundTrip(t, CommandExecResizeParams{
		ProcessID: "proc-9", Size: CommandExecTerminalSize{Rows: 50, Cols: 160},
	}, `{"processId":"proc-9","size":{"rows":50,"cols":160}}`)
	roundTrip(t, CommandExecResizeResponse{}, `{}`)

	roundTrip(t, CommandExecOutputDeltaNotification{
		ProcessID: "proc-1", Stream: CommandExecOutputStreamStdout,
		DeltaBase64: "AQI=", CapReached: false,
	}, `{"processId":"proc-1","stream":"stdout","deltaBase64":"AQI=","capReached":false}`)
}

func TestRealtimeTransportRoundTrip(t *testing.T) {
	roundTrip(t, NewWebsocketTransport(), `{"type":"websocket"}`)
	roundTrip(t, NewWebrtcTransport("v=0\r\n"), `{"type":"webrtc","sdp":"v=0\r\n"}`)
}

func TestRealtimeStartParamsRoundTrip(t *testing.T) {
	// Minimal: prompt omitted (double_option), others emit null.
	roundTrip(t, ThreadRealtimeStartParams{
		ThreadID:       "thr_1",
		OutputModality: protocol.RealtimeOutputModalityAudio,
	}, `{"threadId":"thr_1","outputModality":"audio","realtimeSessionId":null,"transport":null,"voice":null}`)

	// Full: prompt value, transport, voice.
	transport := NewWebsocketTransport()
	voice := protocol.RealtimeVoiceMarin
	roundTrip(t, ThreadRealtimeStartParams{
		ThreadID:          "thr_1",
		OutputModality:    protocol.RealtimeOutputModalityText,
		Prompt:            NewDoubleOptionValue[string]("hi"),
		RealtimeSessionID: strPtr("sess_1"),
		Transport:         &transport,
		Voice:             &voice,
	}, `{"threadId":"thr_1","outputModality":"text","prompt":"hi","realtimeSessionId":"sess_1","transport":{"type":"websocket"},"voice":"marin"}`)

	// prompt explicit null (double_option present, not set).
	roundTrip(t, ThreadRealtimeStartParams{
		ThreadID:       "thr_1",
		OutputModality: protocol.RealtimeOutputModalityText,
		Prompt:         NewDoubleOptionNull[string](),
	}, `{"threadId":"thr_1","outputModality":"text","prompt":null,"realtimeSessionId":null,"transport":null,"voice":null}`)

	roundTrip(t, ThreadRealtimeStartResponse{}, `{}`)
}

func TestRealtimeAudioAndTextRoundTrip(t *testing.T) {
	// audio chunk: optional fields emitted null.
	roundTrip(t, ThreadRealtimeAudioChunk{
		Data: "AAE=", SampleRate: 24000, NumChannels: 1,
	}, `{"data":"AAE=","sampleRate":24000,"numChannels":1,"samplesPerChannel":null,"itemId":null}`)
	roundTrip(t, ThreadRealtimeAudioChunk{
		Data: "AAE=", SampleRate: 24000, NumChannels: 1,
		SamplesPerChannel: u32Ptr(480), ItemID: strPtr("it_1"),
	}, `{"data":"AAE=","sampleRate":24000,"numChannels":1,"samplesPerChannel":480,"itemId":"it_1"}`)

	roundTrip(t, ThreadRealtimeAppendAudioParams{
		ThreadID: "thr_1",
		Audio:    ThreadRealtimeAudioChunk{Data: "AAE=", SampleRate: 24000, NumChannels: 1},
	}, `{"threadId":"thr_1","audio":{"data":"AAE=","sampleRate":24000,"numChannels":1,"samplesPerChannel":null,"itemId":null}}`)
	roundTrip(t, ThreadRealtimeAppendAudioResponse{}, `{}`)

	roundTrip(t, ThreadRealtimeAppendTextParams{ThreadID: "thr_1", Text: "hi"},
		`{"threadId":"thr_1","text":"hi"}`)
	roundTrip(t, ThreadRealtimeAppendTextResponse{}, `{}`)

	roundTrip(t, ThreadRealtimeStopParams{ThreadID: "thr_1"}, `{"threadId":"thr_1"}`)
	roundTrip(t, ThreadRealtimeStopResponse{}, `{}`)

	roundTrip(t, ThreadRealtimeListVoicesParams{}, `{}`)
	roundTrip(t, ThreadRealtimeListVoicesResponse{Voices: RealtimeVoicesList{
		V1:        []protocol.RealtimeVoice{protocol.RealtimeVoiceCove},
		V2:        []protocol.RealtimeVoice{protocol.RealtimeVoiceMarin},
		DefaultV1: protocol.RealtimeVoiceCove,
		DefaultV2: protocol.RealtimeVoiceMarin,
	}}, `{"voices":{"v1":["cove"],"v2":["marin"],"defaultV1":"cove","defaultV2":"marin"}}`)
}

func TestRealtimeNotificationsRoundTrip(t *testing.T) {
	roundTrip(t, ThreadRealtimeStartedNotification{
		ThreadID: "thr_1", Version: RealtimeConversationVersionV2V2,
	}, `{"threadId":"thr_1","realtimeSessionId":null,"version":"v2"}`)
	roundTrip(t, ThreadRealtimeStartedNotification{
		ThreadID: "thr_1", RealtimeSessionID: strPtr("s"), Version: RealtimeConversationVersionV2V1,
	}, `{"threadId":"thr_1","realtimeSessionId":"s","version":"v1"}`)

	roundTrip(t, ThreadRealtimeItemAddedNotification{
		ThreadID: "thr_1", Item: json.RawMessage(`{"k":"v"}`),
	}, `{"threadId":"thr_1","item":{"k":"v"}}`)

	roundTrip(t, ThreadRealtimeTranscriptDeltaNotification{
		ThreadID: "thr_1", Role: "assistant", Delta: "he",
	}, `{"threadId":"thr_1","role":"assistant","delta":"he"}`)
	roundTrip(t, ThreadRealtimeTranscriptDoneNotification{
		ThreadID: "thr_1", Role: "assistant", Text: "hello",
	}, `{"threadId":"thr_1","role":"assistant","text":"hello"}`)

	roundTrip(t, ThreadRealtimeOutputAudioDeltaNotification{
		ThreadID: "thr_1",
		Audio:    ThreadRealtimeAudioChunk{Data: "AAE=", SampleRate: 24000, NumChannels: 1},
	}, `{"threadId":"thr_1","audio":{"data":"AAE=","sampleRate":24000,"numChannels":1,"samplesPerChannel":null,"itemId":null}}`)

	roundTrip(t, ThreadRealtimeSdpNotification{ThreadID: "thr_1", SDP: "v=0"},
		`{"threadId":"thr_1","sdp":"v=0"}`)
	roundTrip(t, ThreadRealtimeErrorNotification{ThreadID: "thr_1", Message: "boom"},
		`{"threadId":"thr_1","message":"boom"}`)
	roundTrip(t, ThreadRealtimeClosedNotification{ThreadID: "thr_1"},
		`{"threadId":"thr_1","reason":null}`)
	roundTrip(t, ThreadRealtimeClosedNotification{ThreadID: "thr_1", Reason: strPtr("done")},
		`{"threadId":"thr_1","reason":"done"}`)
}

func TestFsProcessRealtimeRegistration(t *testing.T) {
	requests := map[string]bool{
		"fs/readFile":                 false,
		"fs/writeFile":                false,
		"fs/createDirectory":          false,
		"fs/getMetadata":              false,
		"fs/readDirectory":            false,
		"fs/remove":                   false,
		"fs/copy":                     false,
		"fs/watch":                    false,
		"fs/unwatch":                  false,
		"command/exec":                false,
		"command/exec/write":          false,
		"command/exec/terminate":      false,
		"command/exec/resize":         false,
		"process/spawn":               true,
		"process/writeStdin":          true,
		"process/kill":                true,
		"process/resizePty":           true,
		"thread/realtime/start":       true,
		"thread/realtime/appendAudio": true,
		"thread/realtime/appendText":  true,
		"thread/realtime/stop":        true,
		"thread/realtime/listVoices":  true,
	}
	for method, wantExp := range requests {
		spec, ok := Lookup(method)
		if !ok {
			t.Errorf("client request %q not registered", method)
			continue
		}
		if spec.Experimental != wantExp {
			t.Errorf("%q experimental = %v, want %v", method, spec.Experimental, wantExp)
		}
		if p := spec.NewParams(); p == nil {
			t.Errorf("%q NewParams returned nil", method)
		}
		if r := spec.NewResult(); r == nil {
			t.Errorf("%q NewResult returned nil", method)
		}
	}

	notes := map[string]bool{
		"fs/changed":                        false,
		"command/exec/outputDelta":          false,
		"process/outputDelta":               true,
		"process/exited":                    true,
		"thread/realtime/started":           true,
		"thread/realtime/itemAdded":         true,
		"thread/realtime/transcript/delta":  true,
		"thread/realtime/transcript/done":   true,
		"thread/realtime/outputAudio/delta": true,
		"thread/realtime/sdp":               true,
		"thread/realtime/error":             true,
		"thread/realtime/closed":            true,
	}
	for method, wantExp := range notes {
		spec, ok := LookupNotification(method)
		if !ok {
			t.Errorf("notification %q not registered", method)
			continue
		}
		if spec.Experimental != wantExp {
			t.Errorf("%q experimental = %v, want %v", method, spec.Experimental, wantExp)
		}
		if p := spec.NewParams(); p == nil {
			t.Errorf("%q NewParams returned nil", method)
		}
	}
}

func TestDecodeClientRequestParamsFsCopy(t *testing.T) {
	req := JSONRPCRequest{Method: "fs/copy", Params: json.RawMessage(
		`{"sourcePath":"/a","destinationPath":"/b","recursive":true}`)}
	got, err := DecodeClientRequestParams(req)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	p, ok := got.(*FsCopyParams)
	if !ok {
		t.Fatalf("type = %T, want *FsCopyParams", got)
	}
	if p.SourcePath != "/a" || p.DestinationPath != "/b" || !p.Recursive {
		t.Fatalf("decoded = %+v", *p)
	}
}
