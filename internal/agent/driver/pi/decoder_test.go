package pi

import (
	"testing"

	"github.com/austiecodes/solax/internal/agent/agenttest"
	"github.com/austiecodes/solax/pkg/ir"
)

func TestGoldenToolAndText(t *testing.T) {
	evs := agenttest.Replay(t, NewDecoder(), agenttest.Transcript(ir.KindPi, "tool_and_text.jsonl"))
	agenttest.CheckTurns(t, evs)
	last := evs[len(evs)-1]
	if last.Type != ir.TurnCompleted || last.Turn.Status != ir.TurnOK {
		t.Fatalf("last event = %+v, want a completed turn", last)
	}
	var tool, text bool
	for _, e := range evs {
		if e.Type != ir.ItemCompleted {
			continue
		}
		switch e.Item.Kind {
		case ir.ItemToolCall:
			tool = e.Item.Tool.Class == ir.ToolShell && e.Item.Tool.Output != ""
		case ir.ItemAgentMessage:
			text = text || e.Item.Text == "hello"
		}
	}
	if !tool || !text {
		t.Fatalf("shell tool with output = %v, final text \"hello\" = %v", tool, text)
	}
}

// A provider failure after all retries fails the turn with the error.
func TestGoldenErrorRetry(t *testing.T) {
	evs := agenttest.Replay(t, NewDecoder(), agenttest.Transcript(ir.KindPi, "error_retry.jsonl"))
	agenttest.CheckTurns(t, evs)
	last := evs[len(evs)-1]
	if last.Type != ir.TurnCompleted || last.Turn.Status != ir.TurnFailed || last.Turn.Error.Message != "Connection error." {
		t.Fatalf("last event = %+v, want a failed turn", last.Turn)
	}
	var retries int
	for i, e := range evs {
		if e.Type == ir.TurnStarted && i != 0 {
			t.Fatal("a retry opened a second turn")
		}
		if e.Type == ir.NoticeEmitted && e.Notice.Code == "retry" {
			retries++
		}
	}
	if retries != 3 {
		t.Fatalf("retry notices = %d, want 3", retries)
	}
}
