package main

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

func shell(cmd string) *ir.ToolCall {
	return &ir.ToolCall{Name: "Bash", Class: ir.ToolShell, Input: json.RawMessage(`{"command":"` + cmd + `"}`), Output: "ok"}
}

func done(seq uint64, it ir.Item, at time.Time) ir.Event {
	if it.Status == "" {
		it.Status = ir.StatusCompleted
	}
	return ir.Event{Seq: seq, Type: ir.ItemCompleted, Item: &it, At: at}
}

func kinds(bs []block) []blockKind {
	out := make([]blockKind, len(bs))
	for i, b := range bs {
		out[i] = b.kind
	}
	return out
}

// Steps fold into one group before the reply they led to; a tool call
// that started replaces its step as it completes; reasoning has its time.
func TestEventBlocks(t *testing.T) {
	t0 := time.Unix(1000, 0)
	evs := []ir.Event{
		done(1, ir.Item{ID: "u1", Kind: ir.ItemUserMessage, Text: "fix it"}, t0),
		{Seq: 2, Type: ir.ItemStarted, Item: &ir.Item{ID: "r1", Kind: ir.ItemReasoning}, At: t0},
		done(3, ir.Item{ID: "r1", Kind: ir.ItemReasoning, Text: "Let me look."}, t0.Add(2*time.Second)),
		{Seq: 4, Type: ir.ItemStarted, Item: &ir.Item{ID: "t1", Kind: ir.ItemToolCall, Status: ir.StatusInProgress, Tool: shell("ls")}, At: t0},
		done(5, ir.Item{ID: "t1", Kind: ir.ItemToolCall, Tool: shell("ls")}, t0.Add(time.Second)),
		done(6, ir.Item{ID: "r2", Kind: ir.ItemReasoning}, t0), // hidden reasoning: no text, no time
		done(7, ir.Item{ID: "a1", Kind: ir.ItemAgentMessage, Text: "Done."}, t0),
		{Seq: 8, Type: ir.ItemStarted, Item: &ir.Item{ID: "t2", Kind: ir.ItemToolCall, Status: ir.StatusInProgress, Tool: shell("go test")}, At: t0},
	}
	bs := eventBlocks(evs, nil, true)
	want := []blockKind{blockUser, blockSteps, blockReply, blockSteps}
	if got := kinds(bs); !slices.Equal(got, want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	g := bs[1]
	if len(g.steps) != 2 || g.live || g.key != "steps-r1" {
		t.Fatalf("first group = %+v", g)
	}
	if g.steps[0].took != 2*time.Second || stepsTitle(g) != "1 tool used" {
		t.Fatalf("took = %v, title = %q", g.steps[0].took, stepsTitle(g))
	}
	if last := bs[3]; !last.live || stepsTitle(last) != "Running go test" {
		t.Fatalf("live group = %+v, title %q", last, stepsTitle(last))
	}

	// Streaming text: reasoning joins the steps, agent text is a reply.
	live := map[string]*liveText{"r9": {kind: "reasoning", text: "hmm"}, "z9": {kind: "text", text: "Almost"}}
	bs = eventBlocks(evs[:7], live, true)
	if got := kinds(bs); !slices.Equal(got, []blockKind{blockUser, blockSteps, blockReply, blockSteps, blockReply}) {
		t.Fatalf("live kinds = %v", got)
	}
	if !bs[3].live || stepsTitle(bs[3]) != "Thinking" || !bs[4].live {
		t.Fatalf("live blocks = %+v", bs[3:])
	}
}

// A tool call that never got its completion stops when its turn ends: it
// must not spin for ever. After a good turn it counts as done; after an
// interrupted one, as stopped.
func TestEndTurnStopsOpenSteps(t *testing.T) {
	t0 := time.Unix(1000, 0)
	open := func(seq uint64, id string) ir.Event {
		return ir.Event{Seq: seq, Type: ir.ItemStarted, Item: &ir.Item{ID: id, Kind: ir.ItemToolCall, Status: ir.StatusInProgress, Tool: shell("cat x")}, At: t0}
	}
	end := func(seq uint64, st ir.TurnStatus) ir.Event {
		return ir.Event{Seq: seq, Type: ir.TurnCompleted, TurnID: "turn-1", Turn: &ir.Turn{Status: st}, At: t0}
	}
	for _, tc := range []struct {
		turn ir.TurnStatus
		want ir.ItemStatus
	}{
		{ir.TurnOK, ir.StatusCompleted},
		{ir.TurnInterrupted, ir.StatusInterrupted},
		{ir.TurnFailed, ir.StatusInterrupted},
	} {
		bs := eventBlocks([]ir.Event{open(1, "t1"), end(2, tc.turn)}, nil, false)
		if len(bs) != 1 || bs[0].live || bs[0].steps[0].status != tc.want {
			t.Errorf("turn %s: blocks %+v; want one stopped step with status %s", tc.turn, bs, tc.want)
		}
	}
	// Before the turn ends, the step runs.
	if bs := eventBlocks([]ir.Event{open(1, "t1")}, nil, true); !bs[0].steps[0].running() {
		t.Error("open turn: the step does not run")
	}
}

// The chat takes the messages as they are and puts the steps of the
// Super Agent above the reply each led to. Steps with no reply yet come
// last; a turn that only dispatched joins the steps of the next reply.
func TestChatBlocks(t *testing.T) {
	t0 := time.Unix(1000, 0)
	msg := func(id, kind, text string) protocol.Message {
		return protocol.Message{ID: id, Author: protocol.Author{Kind: kind}, Text: text}
	}
	msgs := []protocol.Message{
		msg("m1", "human", "add a README"),
		msg("m2", "super", "Sent it to coder."),
		msg("m3", "system", "[job job_1] agent coder: done"),
		msg("m4", "super", "The README is in."),
	}
	evs := []ir.Event{
		done(1, ir.Item{ID: "u1", Kind: ir.ItemUserMessage, Text: "[user] add a README"}, t0),
		done(2, ir.Item{ID: "t1", Kind: ir.ItemToolCall, Tool: shell("solax job send coder")}, t0),
		done(3, ir.Item{ID: "a1", Kind: ir.ItemAgentMessage, Text: "Sent it to coder.\n"}, t0),
		done(4, ir.Item{ID: "t2", Kind: ir.ItemToolCall, Tool: shell("solax job get job_1")}, t0),
		done(5, ir.Item{ID: "a2", Kind: ir.ItemAgentMessage, Text: "The README is in."}, t0),
		done(6, ir.Item{ID: "t3", Kind: ir.ItemToolCall, Tool: shell("solax jobs")}, t0),
	}
	bs := chatBlocks(msgs, evs, nil, true)
	want := []blockKind{blockUser, blockSteps, blockReply, blockNote, blockSteps, blockReply, blockSteps}
	if got := kinds(bs); !slices.Equal(got, want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	if bs[1].steps[0].id != "t1" || bs[4].steps[0].id != "t2" || bs[3].job != "job_1" || bs[3].text != "agent coder: done" {
		t.Fatalf("blocks = %+v", bs)
	}
	if !bs[6].live || bs[6].key != "steps-t3" {
		t.Fatalf("tail = %+v", bs[6])
	}
	// Without events (an old server), the chat is the messages alone.
	if got := kinds(chatBlocks(msgs, nil, nil, false)); !slices.Equal(got, []blockKind{blockUser, blockReply, blockNote, blockReply}) {
		t.Fatalf("no events: %v", got)
	}
}

// A group opens while the agent works and closes when it is done, unless
// the user opened or closed it.
func TestFolds(t *testing.T) {
	d := testDesktop(t)
	if f := d.foldFor("g", true); !f.open {
		t.Fatal("a live group is closed")
	}
	if f := d.foldFor("g", false); f.open {
		t.Fatal("a done group stays open")
	}
	d.folds["g"].open, d.folds["g"].set = true, true
	if f := d.foldFor("g", false); !f.open {
		t.Fatal("the user's choice did not hold")
	}
}

// Return sends the draft; Shift+Return does not.
func TestComposer(t *testing.T) {
	var sent []string
	draft := ""
	view := func(c *ui.Context) {
		composer{key: "c", placeholder: "Write", draft: &draft, focus: true, send: func(s string) { sent = append(sent, s) }}.view(c, paletteFor(c))
	}
	tt := ui.NewTester(view, 800, 300)
	tt.Type("hello")
	tt.Key(ui.Shift, ui.KeyEnter)
	if len(sent) != 0 {
		t.Fatalf("Shift+Return sent %q", sent)
	}
	tt.Key(0, ui.KeyEnter)
	if len(sent) != 1 || sent[0] != "hello" {
		t.Fatalf("sent = %q, draft %q", sent, draft)
	}
}
