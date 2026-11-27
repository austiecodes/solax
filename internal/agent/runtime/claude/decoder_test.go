package claude

import (
	"slices"
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

// An input written during a turn steers it: Claude Code reads it between
// two model calls and replays it there, and one result ends the turn. The
// user message item comes at the replay, inside the turn, and a replay
// while idle opens the next turn with cause "input".
func TestReplaySteer(t *testing.T) {
	d := NewDecoder()
	replay := func(text string) string {
		return `{"type":"user","isReplay":true,"message":{"role":"user","content":[{"type":"text","text":"` + text + `"}]},"session_id":"s"}`
	}
	d.NoteInput("in_1", "run sleep")
	var evs []ir.Event
	decode := func(recs ...string) {
		for _, rec := range recs {
			out, err := d.Decode([]byte(rec))
			if err != nil {
				t.Fatalf("decode %s: %v", rec, err)
			}
			evs = append(evs, out...)
		}
	}
	decode(replay("run sleep"),
		`{"type":"assistant","message":{"id":"msg_1","role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"sleep 6"}}]},"session_id":"s"}`)
	d.NoteInput("in_2", "echo second too")
	decode(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"first"}]},"session_id":"s"}`,
		replay("echo second too"),
		`{"type":"assistant","message":{"id":"msg_2","role":"assistant","content":[{"type":"text","text":"first, second"}]},"session_id":"s"}`,
		`{"type":"result","subtype":"success","result":"first, second","session_id":"s"}`)
	agenttest.CheckTurns(t, evs)

	var got []string
	for _, e := range evs {
		switch {
		case e.Type == ir.TurnStarted:
			got = append(got, "turn:"+string(e.Turn.Cause))
		case e.Type == ir.ItemCompleted:
			got = append(got, e.Item.ID+"@"+e.TurnID)
		case e.Type == ir.TurnCompleted:
			got = append(got, "end")
		}
	}
	want := []string{"in_1@", "turn:input", "toolu_1@turn-1", "in_2@turn-1", "msg_2:0@turn-1", "end"}
	if !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}

	// Output with no input read opens a turn with cause "auto".
	evs = nil
	decode(`{"type":"assistant","message":{"id":"msg_3","role":"assistant","content":[{"type":"text","text":"more"}]},"session_id":"s"}`)
	if evs[0].Type != ir.TurnStarted || evs[0].Turn.Cause != ir.CauseAuto {
		t.Fatalf("first event = %+v, want an auto turn", evs[0])
	}
}
