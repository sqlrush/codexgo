package realtimeconv

import "testing"

func TestPrefixText(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		prefix string
		kind   SessionKind
		want   string
	}{
		{name: "v1 no prefix", text: "hello", prefix: UserTextPrefix, kind: SessionKindV1, want: "hello"},
		{name: "v2 adds prefix", text: "hello", prefix: UserTextPrefix, kind: SessionKindV2, want: "[USER] hello"},
		{name: "v2 empty unchanged", text: "", prefix: UserTextPrefix, kind: SessionKindV2, want: ""},
		{name: "v2 already prefixed", text: "[USER] hi", prefix: UserTextPrefix, kind: SessionKindV2, want: "[USER] hi"},
		{name: "v2 backend prefix", text: "out", prefix: BackendTextPrefix, kind: SessionKindV2, want: "[BACKEND] out"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := prefixText(tt.text, tt.prefix, tt.kind); got != tt.want {
				t.Fatalf("prefixText = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrefixV2Text(t *testing.T) {
	if got := PrefixV2Text("hi", BackendTextPrefix); got != "[BACKEND] hi" {
		t.Fatalf("PrefixV2Text = %q", got)
	}
}

func TestEscapeXMLText(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "a & b", want: "a &amp; b"},
		{in: "<tag>", want: "&lt;tag&gt;"},
		{in: "x & <y> & z", want: "x &amp; &lt;y&gt; &amp; z"},
		{in: "plain", want: "plain"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := escapeXMLText(tt.in); got != tt.want {
				t.Fatalf("escapeXMLText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDelegationFromHandoff(t *testing.T) {
	tests := []struct {
		name    string
		handoff *HandoffRequested
		wantOK  bool
		want    string
	}{
		{
			name:    "nil",
			handoff: nil,
			wantOK:  false,
		},
		{
			name:    "empty",
			handoff: &HandoffRequested{},
			wantOK:  false,
		},
		{
			name: "input only",
			handoff: &HandoffRequested{
				InputTranscript: "do <stuff> & more",
			},
			wantOK: true,
			want:   "<realtime_delegation>\n  <input>do &lt;stuff&gt; &amp; more</input>\n</realtime_delegation>",
		},
		{
			name: "input with transcript",
			handoff: &HandoffRequested{
				InputTranscript: "task",
				ActiveTranscript: []TranscriptEntry{
					{Role: "user", Text: "hi"},
					{Role: "assistant", Text: "yo"},
				},
			},
			wantOK: true,
			want:   "<realtime_delegation>\n  <input>task</input>\n  <transcript_delta>user: hi\nassistant: yo</transcript_delta>\n</realtime_delegation>",
		},
		{
			name: "transcript fallback when no input",
			handoff: &HandoffRequested{
				ActiveTranscript: []TranscriptEntry{
					{Role: "user", Text: "only"},
				},
			},
			wantOK: true,
			want:   "<realtime_delegation>\n  <input>user: only</input>\n  <transcript_delta>user: only</transcript_delta>\n</realtime_delegation>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := DelegationFromHandoff(tt.handoff)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Fatalf("got:\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}
