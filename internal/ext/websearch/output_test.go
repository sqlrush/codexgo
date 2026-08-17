package websearch

import (
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/tools"
)

func TestEncryptedSearchOutputEmitsEncryptedFunctionCallOutput(t *testing.T) {
	output := NewEncryptedSearchOutput("encrypted-search-output")
	item := output.ToResponseItem("call-1")

	if item.Kind != tools.ResponseInputItemKindFunctionCallOutput {
		t.Fatalf("kind = %d, want function call output", item.Kind)
	}
	if item.CallID != "call-1" {
		t.Errorf("call id = %q, want call-1", item.CallID)
	}
	want := protocol.FunctionCallOutputFromContentItems([]protocol.FunctionCallOutputContentItem{
		{
			Type:             protocol.FunctionCallOutputContentItemKindEncryptedContent,
			EncryptedContent: "encrypted-search-output",
		},
	})
	if !reflect.DeepEqual(item.Output, want) {
		t.Errorf("output = %#v, want %#v", item.Output, want)
	}
}

func TestEncryptedSearchOutputPreviewAndSuccess(t *testing.T) {
	output := NewEncryptedSearchOutput("x")
	if output.LogPreview() != "[encrypted standalone web search output]" {
		t.Errorf("log preview = %q", output.LogPreview())
	}
	if !output.SuccessForLogging() {
		t.Errorf("success should be true")
	}
}
