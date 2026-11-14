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

// structuredContent has a different shape per tool. The read tool sends a
// string; that once failed the whole tool_execution_end record, and the
// read call never completed.
func TestToolEndAnyStructuredContent(t *testing.T) {
	d := NewDecoder()
	recs := []string{
		`{"type":"agent_start"}`,
		`{"type":"tool_execution_start","toolCallId":"r1","toolName":"read","args":{"path":"README.md"}}`,
		`{"type":"tool_execution_start","toolCallId":"b1","toolName":"bash","args":{"command":"false"}}`,
		`{"type":"tool_execution_end","toolCallId":"r1","toolName":"read","result":{"content":[{"type":"text","text":"# Title"}],"structuredContent":"# Title"},"isError":false}`,
		`{"type":"tool_execution_end","toolCallId":"b1","toolName":"bash","result":{"content":[{"type":"text","text":""}],"structuredContent":{"exit_code":1}},"isError":true}`,
	}
	done := map[string]ir.Item{}
	for _, rec := range recs {
		evs, err := d.Decode([]byte(rec))
		if err != nil {
			t.Fatalf("Decode(%s): %v", rec, err)
		}
		for _, e := range evs {
			if e.Type == ir.ItemCompleted {
				done[e.Item.ID] = *e.Item
			}
		}
	}
	read, ok := done["r1"]
	if !ok || read.Tool.Output != "# Title" || read.Tool.ExitCode != nil {
		t.Errorf("read: completed=%t item=%+v; want output \"# Title\" and no exit code", ok, read.Tool)
	}
	bash, ok := done["b1"]
	if !ok || bash.Tool.ExitCode == nil || *bash.Tool.ExitCode != 1 || bash.Status != ir.StatusFailed {
		t.Errorf("bash: completed=%t item=%+v; want exit code 1, failed", ok, bash)
	}
}
