package multiagent

import (
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// renderInputPreview renders a short text preview of an initial operation, used
// as the agent's last-task-message. It is a faithful port of the Rust
// `agent::control::render_input_preview`.
func renderInputPreview(op protocol.Op) string {
	switch op.Type {
	case protocol.OpUserInput:
		lines := make([]string, 0, len(op.Items))
		for _, item := range op.Items {
			lines = append(lines, renderUserInputItem(item))
		}
		return strings.Join(lines, "\n")
	case protocol.OpInterAgentCommunication:
		if op.Communication != nil {
			return op.Communication.Content
		}
		return ""
	default:
		return ""
	}
}

// renderUserInputItem renders a single user-input item, mirroring the Rust match
// over the `UserInput` variants.
func renderUserInputItem(item protocol.UserInput) string {
	switch item.Type {
	case protocol.UserInputKindText:
		return item.Text
	case protocol.UserInputKindImage:
		return "[image]"
	case protocol.UserInputKindLocalImage:
		return fmt.Sprintf("[local_image:%s]", item.Path)
	case protocol.UserInputKindSkill:
		return fmt.Sprintf("[skill:$%s](%s)", item.Name, item.Path)
	case protocol.UserInputKindMention:
		return fmt.Sprintf("[mention:$%s](%s)", item.Name, item.MentionPath)
	default:
		return "[input]"
	}
}
