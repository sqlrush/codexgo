package execserver

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func TestByteChunkRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		want string
	}{
		{name: "empty", raw: []byte{}, want: `""`},
		{name: "ascii", raw: []byte("hello"), want: `"aGVsbG8="`},
		{name: "binary", raw: []byte{0x00, 0xff, 0x10}, want: `"AP8Q"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(ByteChunk(tt.raw))
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("marshal got %s want %s", got, tt.want)
			}
			var back ByteChunk
			if err := json.Unmarshal(got, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if string(back) != string(tt.raw) {
				t.Fatalf("round-trip got %q want %q", back, tt.raw)
			}
		})
	}
}

func TestProcessIdTransparent(t *testing.T) {
	id := NewProcessId("proc-1")
	got, err := json.Marshal(id)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(got) != `"proc-1"` {
		t.Fatalf("marshal got %s want \"proc-1\"", got)
	}
	var back ProcessId
	if err := json.Unmarshal([]byte(`"proc-2"`), &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.String() != "proc-2" {
		t.Fatalf("round-trip got %q want proc-2", back.String())
	}
}

func TestExecParamsSerde(t *testing.T) {
	arg0 := "login-shell"
	params := ExecParams{
		ProcessID: NewProcessId("p1"),
		Argv:      []string{"echo", "hi"},
		Cwd:       "/tmp",
		EnvPolicy: nil,
		Env:       map[string]string{"A": "1"},
		Tty:       true,
		PipeStdin: false,
		Arg0:      &arg0,
	}
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	for _, key := range []string{"processId", "argv", "cwd", "envPolicy", "env", "tty", "pipeStdin", "arg0"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("missing key %q in %s", key, data)
		}
	}
	if string(raw["envPolicy"]) != "null" {
		t.Fatalf("envPolicy should be null, got %s", raw["envPolicy"])
	}
	if string(raw["arg0"]) != `"login-shell"` {
		t.Fatalf("arg0 mismatch: %s", raw["arg0"])
	}

	var back ExecParams
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.ProcessID.String() != "p1" || !back.Tty || back.Cwd != "/tmp" {
		t.Fatalf("round-trip mismatch: %+v", back)
	}
}

func TestExecEnvPolicySerde(t *testing.T) {
	policy := ExecEnvPolicy{
		Inherit:               protocol.ShellEnvironmentPolicyInheritCore,
		IgnoreDefaultExcludes: true,
		Exclude:               []string{"*FOO*"},
		Set:                   map[string]string{"K": "V"},
		IncludeOnly:           []string{"PATH"},
	}
	data, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if string(raw["inherit"]) != `"core"` {
		t.Fatalf("inherit mismatch: %s", raw["inherit"])
	}
	for _, key := range []string{"inherit", "ignoreDefaultExcludes", "exclude", "set", "includeOnly"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("missing key %q", key)
		}
	}
}

func TestReadResponseNullFields(t *testing.T) {
	resp := ReadResponse{
		Chunks:  []ProcessOutputChunk{},
		NextSeq: 1,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if string(raw["exitCode"]) != "null" {
		t.Fatalf("exitCode should be null, got %s", raw["exitCode"])
	}
	if string(raw["failure"]) != "null" {
		t.Fatalf("failure should be null, got %s", raw["failure"])
	}
}

func TestWriteStatusValues(t *testing.T) {
	tests := []struct {
		status WriteStatus
		want   string
	}{
		{WriteStatusAccepted, `"accepted"`},
		{WriteStatusUnknownProcess, `"unknownProcess"`},
		{WriteStatusStdinClosed, `"stdinClosed"`},
		{WriteStatusStarting, `"starting"`},
	}
	for _, tt := range tests {
		got, err := json.Marshal(WriteResponse{Status: tt.status})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		want := `{"status":` + tt.want + `}`
		if string(got) != want {
			t.Fatalf("got %s want %s", got, want)
		}
	}
}

func TestExecOutputStreamValues(t *testing.T) {
	tests := []struct {
		stream ExecOutputStream
		want   string
	}{
		{ExecOutputStreamStdout, `"stdout"`},
		{ExecOutputStreamStderr, `"stderr"`},
		{ExecOutputStreamPty, `"pty"`},
	}
	for _, tt := range tests {
		got, err := json.Marshal(tt.stream)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(got) != tt.want {
			t.Fatalf("got %s want %s", got, tt.want)
		}
	}
}

func TestHTTPRequestParamsTimeoutOmitted(t *testing.T) {
	params := HTTPRequestParams{Method: "GET", URL: "https://example.test", RequestID: "r1"}
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["timeoutMs"]; ok {
		t.Fatalf("timeoutMs should be omitted, got %s", data)
	}
	// headers and bodyBase64 are always present (no skip).
	for _, key := range []string{"headers", "bodyBase64", "streamResponse"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("key %q should be present, got %s", key, data)
		}
	}
}
