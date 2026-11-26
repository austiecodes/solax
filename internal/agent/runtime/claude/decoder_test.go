package claude

import (
	"testing"

	"github.com/austiecodes/solax/internal/agent/agenttest"
	"github.com/austiecodes/solax/pkg/ir"
)

func TestGoldenToolAndText(t *testing.T) {
	evs := agenttest.Replay(t, NewDecoder(), agenttest.Transcript(ir.KindClaude, "tool_and_text.jsonl"))
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

// A denied tool call gives a system/permission_denied record whose message
// is a string, not an object. It must not break the decode.
func TestPermissionDenied(t *testing.T) {
	d := NewDecoder()
	recs := []string{
		`{"type":"assistant","message":{"id":"msg_1","role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"touch a"}}]},"session_id":"s"}`,
		`{"type":"system","subtype":"permission_denied","tool_name":"Bash","tool_use_id":"toolu_1","decision_reason_type":"asyncAgent","decision_reason_code":null,"decision_reason":"no approval surface in this session; permission request denied automatically","message":"Permission for this tool use was denied.","uuid":"u","session_id":"s"}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"Permission for this tool use was denied.","is_error":true}]},"session_id":"s"}`,
	}
	var last ir.Event
	for _, rec := range recs {
		evs, err := d.Decode([]byte(rec))
		if err != nil {
			t.Fatalf("decode %s: %v", rec, err)
		}
		if len(evs) > 0 {
			last = evs[len(evs)-1]
		}
	}
	if last.Type != ir.ItemCompleted || last.Item.ID != "toolu_1" || last.Item.Status != ir.StatusFailed {
		t.Fatalf("last event = %+v, want a failed toolu_1", last)
	}
}

// system/init with another permission mode than the session asked for
// gives one warning; the same mode gives none.
func TestPermissionModeWarning(t *testing.T) {
	tests := []struct {
		mode, init string
		want       int
	}{
		{"bypassPermissions", "default", 1},
		{"bypassPermissions", "bypassPermissions", 0},
		{"", "default", 0},
	}
	for _, tt := range tests {
		d := NewDecoder()
		d.Mode = tt.mode
		rec := `{"type":"system","subtype":"init","permissionMode":"` + tt.init + `","session_id":"s"}`
		var got int
		for range 2 { // one warning per session, not per init
			evs, err := d.Decode([]byte(rec))
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range evs {
				if e.Type == ir.NoticeEmitted && e.Notice.Code == "permission_mode" {
					got++
				}
			}
		}
		if got != tt.want {
			t.Errorf("mode %q, init %q: %d warnings, want %d", tt.mode, tt.init, got, tt.want)
		}
	}
}
