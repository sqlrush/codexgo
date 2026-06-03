package exec

import (
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// Command executions are not modeled as engine v1 TurnItems; the engine reports
// them through the exec_command_begin / exec_command_end event pair (keyed by
// call_id). This file synthesizes the exec command_execution item lifecycle from
// that pair so the JSONL stream matches the v2 ItemStarted/ItemCompleted shape
// the Rust exec emits.

// collectExecCommandBegin maps an exec_command_begin to a command_execution
// item.started event, allocating an exec item id keyed by the call id.
func (p *JSONLProcessor) collectExecCommandBegin(n *protocol.ExecCommandBeginEvent) CollectedThreadEvents {
	if n == nil {
		return CollectedThreadEvents{Status: StatusRunning}
	}
	id := p.startedItemID(n.CallID)
	item := ThreadItem{
		ID: id,
		Details: ThreadItemDetails{
			Kind: ThreadItemDetailKindCommandExecution,
			CommandExecution: &CommandExecutionItem{
				Command:          joinCommand(n.Command),
				AggregatedOutput: "",
				ExitCode:         nil,
				Status:           CommandExecutionStatusInProgress,
			},
		},
	}
	return CollectedThreadEvents{
		Events: []ThreadEvent{{Kind: ThreadEventKindItemStarted, ItemStarted: &ItemEvent{Item: item}}},
		Status: StatusRunning,
	}
}

// collectExecCommandEnd maps an exec_command_end to a command_execution
// item.completed event, reusing the id allocated at begin and translating the
// completion status.
func (p *JSONLProcessor) collectExecCommandEnd(n *protocol.ExecCommandEndEvent) CollectedThreadEvents {
	if n == nil {
		return CollectedThreadEvents{Status: StatusRunning}
	}
	id := p.completedItemID(n.CallID)
	exit := n.ExitCode
	item := ThreadItem{
		ID: id,
		Details: ThreadItemDetails{
			Kind: ThreadItemDetailKindCommandExecution,
			CommandExecution: &CommandExecutionItem{
				Command:          joinCommand(n.Command),
				AggregatedOutput: n.AggregatedOutput,
				ExitCode:         &exit,
				Status:           commandStatus(n.Status),
			},
		},
	}
	return CollectedThreadEvents{
		Events: []ThreadEvent{{Kind: ThreadEventKindItemCompleted, ItemCompleted: &ItemEvent{Item: item}}},
		Status: StatusRunning,
	}
}

// joinCommand renders an argv slice as a single space-joined command string,
// matching the command field the v2 CommandExecution item exposes.
func joinCommand(argv []string) string {
	return strings.Join(argv, " ")
}

// commandStatus maps the engine exec status to the exec command status.
func commandStatus(s protocol.ExecCommandStatus) CommandExecutionStatus {
	switch s {
	case protocol.ExecCommandStatusCompleted:
		return CommandExecutionStatusCompleted
	case protocol.ExecCommandStatusFailed:
		return CommandExecutionStatusFailed
	case protocol.ExecCommandStatusDeclined:
		return CommandExecutionStatusDeclined
	default:
		return CommandExecutionStatusInProgress
	}
}
