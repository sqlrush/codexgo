package tui

import (
	"testing"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

func TestNewTextTurnCommand(t *testing.T) {
	cmd := NewTextTurnCommand("hi there")
	if cmd.Kind != AppCommandUserTurn {
		t.Fatalf("kind = %v, want UserTurn", cmd.Kind)
	}
	if len(cmd.Items) != 1 {
		t.Fatalf("expected 1 input item, got %d", len(cmd.Items))
	}
	if cmd.Items[0].Kind != appserverproto.UserInputKindText {
		t.Fatalf("input kind = %v, want text", cmd.Items[0].Kind)
	}
	if cmd.Items[0].Text != "hi there" {
		t.Fatalf("input text = %q", cmd.Items[0].Text)
	}
}

func TestCommandConstructors(t *testing.T) {
	if NewInterruptCommand().Kind != AppCommandInterrupt {
		t.Fatal("interrupt kind")
	}
	if NewCompactCommand().Kind != AppCommandCompact {
		t.Fatal("compact kind")
	}
	if NewShutdownCommand().Kind != AppCommandShutdown {
		t.Fatal("shutdown kind")
	}
	rb := NewThreadRollbackCommand(3)
	if rb.Kind != AppCommandThreadRollback || rb.NumTurns != 3 {
		t.Fatalf("rollback = %+v", rb)
	}
	ex := NewExecApprovalCommand("id1", "turn1", "approved")
	if ex.ID != "id1" || ex.TurnID != "turn1" || ex.Decision != "approved" {
		t.Fatalf("exec approval = %+v", ex)
	}
}

func TestIsReview(t *testing.T) {
	if NewInterruptCommand().IsReview() {
		t.Fatal("interrupt is not a review")
	}
	review := AppCommand{Kind: AppCommandReview}
	if !review.IsReview() {
		t.Fatal("review command should report IsReview")
	}
}
