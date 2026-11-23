package service

import (
	"fmt"
	"strings"

	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/pkg/ir"
)

// Fast Path and Slow Path (system.md §4). A task has a creator
// (RequestedBy) and an owner of its current turn (TurnBy). The result of a
// turn goes to its owner. A person can send input to any task directly;
// the Super Agent then gets a passive note (ir.ModeNote) instead of a wake,
// and reads it with its next waking input.
//
// The functions in this file are the rules. They take plain values and
// return plain values.

// noteClip bounds the text of one passive note.
const noteClip = 1000

// involvesSuper reports whether the Super Agent has a stake in a task: it
// created the task or started its current turn.
func involvesSuper(j model.Task) bool {
	return j.RequestedBy == model.AuthorSuper || j.TurnBy == model.AuthorSuper
}

// inputMode returns the mode that an input to a task really gets. A steer
// reaches only a running turn; for a task that does not run, it starts a
// turn as a follow-up.
func inputMode(state model.TaskState, mode ir.InputMode) ir.InputMode {
	if mode == ir.ModeSteer && state == model.TaskRunning {
		return ir.ModeSteer
	}
	return ir.ModeFollowUp
}

// turnAfterInput returns the owner of the turn after actor sends input in
// mode (from inputMode), and whether the Super Agent gets a note about the
// input. A steer guides the running turn, so its owner keeps it. A
// follow-up starts a new turn that the sender owns.
func turnAfterInput(j model.Task, actor model.AuthorKind, mode ir.InputMode) (turnBy model.AuthorKind, note bool) {
	turnBy = actor
	if mode == ir.ModeSteer {
		turnBy = j.TurnBy
	}
	return turnBy, actor != model.AuthorSuper && involvesSuper(j)
}

// finishMode returns how the Super Agent hears of a finished turn: a
// waking follow-up when it owns the turn, a note when it only created the
// task, and nothing ("") otherwise.
func finishMode(j model.Task) ir.InputMode {
	switch {
	case j.TurnBy == model.AuthorSuper:
		return ir.ModeFollowUp
	case j.RequestedBy == model.AuthorSuper:
		return ir.ModeNote
	}
	return ""
}

// cancelMode returns how the Super Agent hears that actor cancelled a
// task. It knows when it cancelled the task itself; a cancel by a person
// is a note, and never a wake.
func cancelMode(j model.Task, actor model.AuthorKind) ir.InputMode {
	if actor != model.AuthorSuper && involvesSuper(j) {
		return ir.ModeNote
	}
	return ""
}

// inputNote is the note text about input that a person sent to a task.
func inputNote(j model.Task, agent string, mode ir.InputMode, text string) string {
	verb := "followed up"
	if mode == ir.ModeSteer {
		verb = "steered"
	}
	return fmt.Sprintf("the user %s task %s (agent %s): %s", verb, j.ID, agent, clip(text, noteClip))
}

// batch joins the pending inputs of a session into one input. Notes go
// first, under a [context] header; the waking inputs follow in order. The
// batch is a steer only when every waking input is a steer. ok is false
// when no input wakes the session.
func batch(pending []model.Input) (text string, mode ir.InputMode, ok bool) {
	var notes, wake []string
	mode = ir.ModeSteer
	for _, in := range pending {
		switch in.Mode {
		case ir.ModeNote:
			notes = append(notes, "- "+strings.ReplaceAll(in.Text, "\n", "\n  "))
		case ir.ModeSteer:
			wake = append(wake, in.Text)
		default:
			wake = append(wake, in.Text)
			mode = ir.ModeFollowUp
		}
	}
	if len(wake) == 0 {
		return "", "", false
	}
	text = strings.Join(wake, "\n\n---\n\n")
	if len(notes) > 0 {
		text = "[context]\n" + strings.Join(notes, "\n") + "\n\n" + text
	}
	return text, mode, true
}
