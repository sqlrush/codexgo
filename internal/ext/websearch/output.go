package websearch

import (
	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/tools"
)

// EncryptedSearchOutput is the encrypted server-side search result returned to
// the model. Rust: EncryptedSearchOutput.
type EncryptedSearchOutput struct {
	encryptedOutput string
}

// NewEncryptedSearchOutput wraps an encrypted search output string. Rust:
// EncryptedSearchOutput::new.
func NewEncryptedSearchOutput(encryptedOutput string) EncryptedSearchOutput {
	return EncryptedSearchOutput{encryptedOutput: encryptedOutput}
}

// LogPreview returns a non-sensitive telemetry preview. Rust:
// ToolOutput::log_preview.
func (o EncryptedSearchOutput) LogPreview() string {
	return "[encrypted standalone web search output]"
}

// SuccessForLogging reports the result as successful. Rust:
// ToolOutput::success_for_logging.
func (o EncryptedSearchOutput) SuccessForLogging() bool {
	return true
}

// ToResponseItem emits the encrypted function call output. Rust:
// ToolOutput::to_response_item (FunctionCallOutputPayload::from_content_items).
func (o EncryptedSearchOutput) ToResponseItem(callID string) tools.ResponseInputItem {
	payload := protocol.FunctionCallOutputFromContentItems([]protocol.FunctionCallOutputContentItem{
		{
			Type:             protocol.FunctionCallOutputContentItemKindEncryptedContent,
			EncryptedContent: o.encryptedOutput,
		},
	})
	return tools.FunctionCallOutputInput(callID, payload)
}
