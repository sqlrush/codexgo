package hooks

import "github.com/sqlrush/codexgo/internal/protocol"

func errorEntry(text string) protocol.HookOutputEntry {
	return protocol.HookOutputEntry{Kind: protocol.HookOutputEntryKindError, Text: text}
}

func warningEntry(text string) protocol.HookOutputEntry {
	return protocol.HookOutputEntry{Kind: protocol.HookOutputEntryKindWarning, Text: text}
}

func feedbackEntry(text string) protocol.HookOutputEntry {
	return protocol.HookOutputEntry{Kind: protocol.HookOutputEntryKindFeedback, Text: text}
}

func stopEntry(text string) protocol.HookOutputEntry {
	return protocol.HookOutputEntry{Kind: protocol.HookOutputEntryKindStop, Text: text}
}
