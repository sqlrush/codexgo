package multiagent

import (
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func TestRenderInputPreview(t *testing.T) {
	author := mustPath(t, "/root/a")
	recipient := mustPath(t, "/root")

	tests := []struct {
		name string
		op   protocol.Op
		want string
	}{
		{
			name: "user input text items joined with newline",
			op: protocol.Op{Type: protocol.OpUserInput, Items: []protocol.UserInput{
				{Type: protocol.UserInputKindText, Text: "line one"},
				{Type: protocol.UserInputKindText, Text: "line two"},
			}},
			want: "line one\nline two",
		},
		{
			name: "image placeholder",
			op: protocol.Op{Type: protocol.OpUserInput, Items: []protocol.UserInput{
				{Type: protocol.UserInputKindImage, ImageURL: "data:..."},
			}},
			want: "[image]",
		},
		{
			name: "local image path",
			op: protocol.Op{Type: protocol.OpUserInput, Items: []protocol.UserInput{
				{Type: protocol.UserInputKindLocalImage, Path: "/tmp/x.png"},
			}},
			want: "[local_image:/tmp/x.png]",
		},
		{
			name: "skill reference",
			op: protocol.Op{Type: protocol.OpUserInput, Items: []protocol.UserInput{
				{Type: protocol.UserInputKindSkill, Name: "deploy", Path: "/skills/deploy"},
			}},
			want: "[skill:$deploy](/skills/deploy)",
		},
		{
			name: "mention reference",
			op: protocol.Op{Type: protocol.OpUserInput, Items: []protocol.UserInput{
				{Type: protocol.UserInputKindMention, Name: "alice", MentionPath: "people/alice"},
			}},
			want: "[mention:$alice](people/alice)",
		},
		{
			name: "inter-agent communication content",
			op: protocol.Op{Type: protocol.OpInterAgentCommunication, Communication: &protocol.InterAgentCommunication{
				Author:    author,
				Recipient: recipient,
				Content:   "ping",
			}},
			want: "ping",
		},
		{
			name: "other op renders empty",
			op:   protocol.Op{Type: protocol.OpInterrupt},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderInputPreview(tt.op); got != tt.want {
				t.Fatalf("renderInputPreview = %q, want %q", got, tt.want)
			}
		})
	}
}
