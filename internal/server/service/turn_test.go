package service

import (
	"testing"

	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/pkg/ir"
)

const (
	human = model.AuthorHuman
	super = model.AuthorSuper
)

func TestInputMode(t *testing.T) {
	cases := []struct {
		state model.TaskState
		in    ir.InputMode
		want  ir.InputMode
	}{
		{model.TaskRunning, ir.ModeSteer, ir.ModeSteer},
		{model.TaskQueued, ir.ModeSteer, ir.ModeFollowUp},
		{model.TaskDone, ir.ModeSteer, ir.ModeFollowUp},
		{model.TaskFailed, ir.ModeSteer, ir.ModeFollowUp},
		{model.TaskRunning, ir.ModeFollowUp, ir.ModeFollowUp},
		{model.TaskRunning, "", ir.ModeFollowUp},
		{model.TaskDone, "", ir.ModeFollowUp},
	}
	for _, c := range cases {
		if got := inputMode(c.state, c.in); got != c.want {
			t.Errorf("inputMode(%s, %q) = %q, want %q", c.state, c.in, got, c.want)
		}
	}
}

// All (RequestedBy, TurnBy, State, Actor, Mode) cases of system.md §4.1.
func TestTurnAfterInput(t *testing.T) {
	cases := []struct {
		name          string
		requested, by model.AuthorKind
		state         model.TaskState
		actor         model.AuthorKind
		mode          ir.InputMode
		wantTurn      model.AuthorKind
		wantNote      bool
	}{
		// The Super Agent started the task.
		{"super task, user steers its running turn", super, super, model.TaskRunning, human, ir.ModeSteer, super, true},
		{"super task, user queues a follow-up while it runs", super, super, model.TaskRunning, human, ir.ModeFollowUp, human, true},
		{"super task, user follows up after the end", super, super, model.TaskDone, human, ir.ModeFollowUp, human, true},
		{"super task, user follows up after a failure", super, super, model.TaskFailed, human, ir.ModeFollowUp, human, true},
		{"super task, super follows up", super, super, model.TaskDone, super, ir.ModeFollowUp, super, false},
		{"super task, super steers", super, super, model.TaskRunning, super, ir.ModeSteer, super, false},
		{"super task now owned by user, user follows up", super, human, model.TaskDone, human, ir.ModeFollowUp, human, true},
		{"super task now owned by user, user steers", super, human, model.TaskRunning, human, ir.ModeSteer, human, true},
		{"super task now owned by user, super takes it back", super, human, model.TaskDone, super, ir.ModeFollowUp, super, false},
		// The user started the task.
		{"user task, user follows up", human, human, model.TaskDone, human, ir.ModeFollowUp, human, false},
		{"user task, user steers", human, human, model.TaskRunning, human, ir.ModeSteer, human, false},
		{"user task, super follows up", human, human, model.TaskDone, super, ir.ModeFollowUp, super, false},
		{"user task, super steers the user's turn", human, human, model.TaskRunning, super, ir.ModeSteer, human, false},
		{"user task owned by super, user steers", human, super, model.TaskRunning, human, ir.ModeSteer, super, true},
		{"user task owned by super, user follows up", human, super, model.TaskDone, human, ir.ModeFollowUp, human, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			j := model.Task{RequestedBy: c.requested, TurnBy: c.by, State: c.state}
			mode := inputMode(c.state, c.mode)
			turn, note := turnAfterInput(j, c.actor, mode)
			if turn != c.wantTurn || note != c.wantNote {
				t.Fatalf("turnAfterInput = (%s, %v), want (%s, %v)", turn, note, c.wantTurn, c.wantNote)
			}
		})
	}
}

func TestFinishAndCancelMode(t *testing.T) {
	cases := []struct {
		requested, by model.AuthorKind
		finish        ir.InputMode
		cancelByHuman ir.InputMode
		cancelBySuper ir.InputMode
	}{
		{super, super, ir.ModeFollowUp, ir.ModeNote, ""},
		{super, human, ir.ModeNote, ir.ModeNote, ""},
		{human, super, ir.ModeFollowUp, ir.ModeNote, ""},
		{human, human, "", "", ""},
	}
	for _, c := range cases {
		j := model.Task{RequestedBy: c.requested, TurnBy: c.by}
		if got := finishMode(j); got != c.finish {
			t.Errorf("finishMode(requested=%s, turn=%s) = %q, want %q", c.requested, c.by, got, c.finish)
		}
		if got := cancelMode(j, human); got != c.cancelByHuman {
			t.Errorf("cancelMode(requested=%s, turn=%s, human) = %q, want %q", c.requested, c.by, got, c.cancelByHuman)
		}
		if got := cancelMode(j, super); got != c.cancelBySuper {
			t.Errorf("cancelMode(requested=%s, turn=%s, super) = %q, want %q", c.requested, c.by, got, c.cancelBySuper)
		}
	}
}

func TestBatch(t *testing.T) {
	in := func(mode ir.InputMode, text string) model.Input { return model.Input{Mode: mode, Text: text} }
	cases := []struct {
		name     string
		pending  []model.Input
		wantText string
		wantMode ir.InputMode
		wantOK   bool
	}{
		{"only notes wait", []model.Input{in(ir.ModeNote, "a")}, "", "", false},
		{"one follow-up", []model.Input{in(ir.ModeFollowUp, "do it")}, "do it", ir.ModeFollowUp, true},
		{"steers stay a steer", []model.Input{in(ir.ModeSteer, "a"), in(ir.ModeSteer, "b")}, "a\n\n---\n\nb", ir.ModeSteer, true},
		{"a follow-up makes the batch a follow-up", []model.Input{in(ir.ModeSteer, "a"), in(ir.ModeFollowUp, "b")}, "a\n\n---\n\nb", ir.ModeFollowUp, true},
		{"notes go first as context",
			[]model.Input{in(ir.ModeNote, "user steered tsk_1"), in(ir.ModeFollowUp, "[user] hi"), in(ir.ModeNote, "line 1\nline 2")},
			"[context]\n- user steered tsk_1\n- line 1\n  line 2\n\n[user] hi", ir.ModeFollowUp, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, mode, ok := batch(c.pending)
			if text != c.wantText || mode != c.wantMode || ok != c.wantOK {
				t.Fatalf("batch = (%q, %q, %v), want (%q, %q, %v)", text, mode, ok, c.wantText, c.wantMode, c.wantOK)
			}
		})
	}
}

func TestTaskCompletionReplacesPreview(t *testing.T) {
	answer, empty := "final answer", ""
	for _, tc := range []struct {
		name string
		turn ir.Turn
		want string
	}{
		{"success", ir.Turn{Status: ir.TurnOK, Answer: &answer}, answer},
		{"empty answer", ir.Turn{Status: ir.TurnOK, Answer: &empty}, ""},
		{"failed", ir.Turn{Status: ir.TurnFailed}, ""},
		{"interrupted", ir.Turn{Status: ir.TurnInterrupted}, ""},
		{"older daemon", ir.Turn{Status: ir.TurnOK}, "preview"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := model.Task{State: model.TaskRunning, Result: "preview"}
			applyTaskEvent(&j, ir.Event{Type: ir.TurnCompleted, Turn: &tc.turn})
			if !j.State.Terminal() || j.Result != tc.want {
				t.Fatalf("state = %s, result = %q, want terminal with %q", j.State, j.Result, tc.want)
			}
		})
	}
}
