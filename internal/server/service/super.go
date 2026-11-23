package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/pkg/ir"
)

// The Super Agent conversation.
//
// The user talks only to the Super Agent. Its session has an inbox like
// every session: user messages and the results of the tasks that it
// dispatched go there, and the delivery loop sends them as input. The
// Super Agent never waits for a task: it dispatches, ends its turn, and the
// result wakes it later.
//
// Every agent message of the Super Agent session becomes a reply in the
// conversation, so the user sees its progress notes too.

// SendToSuper appends a user message to the Super Agent conversation and
// puts it into the Super Agent inbox.
func SendToSuper(ctx context.Context, a model.Actor, text string) (model.Message, error) {
	if err := requireHuman(a); err != nil {
		return model.Message{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return model.Message{}, fmt.Errorf("%w: empty message", errs.ErrInvalid)
	}
	var m model.Message
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		ses, err := superSession(ctx, tx)
		if err != nil {
			return err
		}
		now := clock()
		m, err = model.AppendMessage(ctx, tx, model.Message{
			ID: model.NewIDAt(model.PrefixMessage, now), OwnerID: a.OwnerID, Author: a.Author(), Kind: model.MessageChat, Text: text, CreatedAt: now,
		})
		if err != nil {
			return err
		}
		return model.AddInput(ctx, tx, model.Input{ID: model.NewIDAt(model.PrefixInput, now), SessionID: ses.ID, Text: "[user] " + text, CreatedAt: now})
	})
	if err != nil {
		return model.Message{}, err
	}
	Publish(ctx, a.OwnerID, PushMessage, model.WireMessage(m))
	Wake()
	return m, nil
}

// SuperReply appends a reply of the Super Agent. Plain agent messages of
// its session become replies too; this is for an explicit reply command.
func SuperReply(ctx context.Context, a model.Actor, text string) (model.Message, error) {
	if a.Kind != model.AuthorSuper {
		return model.Message{}, errs.ErrForbidden
	}
	var m model.Message
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		var err error
		m, err = appendSuper(ctx, tx, a.OwnerID, model.Author{Kind: model.AuthorSuper, ID: a.ID}, model.MessageChat, text)
		return err
	})
	if err == nil {
		Publish(ctx, a.OwnerID, PushMessage, model.WireMessage(m))
	}
	return m, err
}

// superSession returns the session of the Super Agent, and creates it on
// first use. It has no task.
func superSession(ctx context.Context, tx *sql.Tx) (model.Session, error) {
	ag, err := superAgent(ctx, tx)
	if err != nil {
		return model.Session{}, err
	}
	ses, err := model.GetSessionFor(ctx, tx, ag.ID, "")
	if err == nil {
		return ses, nil
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return model.Session{}, err
	}
	now := clock()
	ses = model.Session{ID: model.NewIDAt(model.PrefixSession, now), AgentID: ag.ID, State: model.SessionStopped, UpdatedAt: now}
	return ses, model.SaveSession(ctx, tx, ses)
}

func appendSuper(ctx context.Context, tx *sql.Tx, owner model.ID, author model.Author, kind model.MessageKind, text string) (model.Message, error) {
	now := clock()
	return model.AppendMessage(ctx, tx, model.Message{
		ID: model.NewIDAt(model.PrefixMessage, now), OwnerID: owner, Author: author, Kind: kind, Text: text, CreatedAt: now,
	})
}

// superEvent turns agent messages of the Super Agent session into replies.
func superEvent(ctx context.Context, tx *sql.Tx, ses model.Session, ev ir.Event, post *afterCommit) error {
	if ev.Type != ir.ItemCompleted || ev.Item == nil || ev.Item.Kind != ir.ItemAgentMessage {
		return nil
	}
	text := strings.TrimSpace(ev.Item.Text)
	if text == "" {
		return nil
	}
	ag, err := model.GetAgent(ctx, tx, ses.AgentID)
	if err != nil {
		return err
	}
	m, err := model.GetMachine(ctx, tx, ag.MachineID)
	if err != nil {
		return err
	}
	msg, err := appendSuper(ctx, tx, m.OwnerID, model.Author{Kind: model.AuthorSuper, ID: ag.ID}, model.MessageChat, text)
	if err != nil {
		return err
	}
	post.add(func() { Publish(context.WithoutCancel(ctx), m.OwnerID, PushMessage, model.WireMessage(msg)) })
	return nil
}

// taskFinished reports a finished turn to the Super Agent when it has a
// stake in the task (finishMode): a system line in the conversation, and an
// input in its inbox that wakes it when it owns the turn, or waits as a
// note when it only created the task.
func taskFinished(ctx context.Context, tx *sql.Tx, j model.Task, ag model.Agent, post *afterCommit) error {
	if j.WorkflowID != "" {
		post.add(func() { WakeWorkflow(j.WorkflowID) })
	}
	mode := finishMode(j)
	if mode == "" {
		return nil
	}
	head := fmt.Sprintf("[task %s] agent %s: %s", j.ID, ag.Name, j.State)
	var b strings.Builder
	b.WriteString(head)
	if j.Error != "" {
		b.WriteString("\nerror: ")
		b.WriteString(j.Error)
	}
	if j.Result != "" {
		b.WriteString("\nresult:\n")
		b.WriteString(j.Result)
	}
	text := b.String()
	if mode == ir.ModeNote {
		text = clip(text, noteClip)
	}
	return toSuper(ctx, tx, j.OwnerID, head, text, mode, post)
}

// toSuper writes head as a system line in the conversation, and text (or
// head, when text is empty) as an input of mode in the Super Agent inbox.
func toSuper(ctx context.Context, tx *sql.Tx, owner model.ID, head, text string, mode ir.InputMode, post *afterCommit) error {
	if text == "" {
		text = head
	}
	if err := superInput(ctx, tx, mode, text); err != nil {
		return err
	}
	msg, err := appendSuper(ctx, tx, owner, model.Author{Kind: model.AuthorSystem}, model.MessageSystem, head)
	if err != nil {
		return err
	}
	post.add(func() { Publish(context.WithoutCancel(ctx), owner, PushMessage, model.WireMessage(msg)) })
	return nil
}

// superInput adds an input to the Super Agent inbox. Without a Super Agent
// it does nothing: the task keeps its result.
func superInput(ctx context.Context, tx *sql.Tx, mode ir.InputMode, text string) error {
	ses, err := superSession(ctx, tx)
	if errors.Is(err, errs.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	now := clock()
	return model.AddInput(ctx, tx, model.Input{ID: model.NewIDAt(model.PrefixInput, now), SessionID: ses.ID, Mode: mode, Text: text, CreatedAt: now})
}

// afterCommit collects actions that must run only after the transaction
// commits, such as client pushes. A transaction may retry; it is reset
// at the start of each attempt.
type afterCommit []func()

func (p *afterCommit) add(fn func()) { *p = append(*p, fn) }

func (p *afterCommit) run() {
	for _, fn := range *p {
		fn()
	}
}

// SuperMessages returns the conversation of the user with the Super
// Agent, with seq > since.
func SuperMessages(ctx context.Context, a model.Actor, since int64, limit int) ([]model.Message, error) {
	if err := requireManager(a); err != nil {
		return nil, err
	}
	return model.ListMessages(ctx, model.ReadDB, a.OwnerID, since, limit)
}
