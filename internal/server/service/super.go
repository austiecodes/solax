package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/austiecodes/solax/internal/server/model"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
)

// The Super Agent conversation.
//
// The user talks only to the Super Agent. Its session has an inbox like
// every session: user messages and the results of the jobs that it
// dispatched go there, and the delivery loop sends them as input. The
// Super Agent never waits for a job: it dispatches, ends its turn, and the
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
// first use. It has no job.
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

// jobFinished reports a finished job of the Super Agent back to it: a
// system note in the conversation, and an input in its inbox.
func jobFinished(ctx context.Context, tx *sql.Tx, j model.Job, ag model.Agent, post *afterCommit) error {
	if j.RequestedBy != model.AuthorSuper {
		return nil
	}
	ses, err := superSession(ctx, tx)
	if errors.Is(err, errs.ErrNotFound) {
		return nil // the Super Agent was removed; the job result stays on the job
	}
	if err != nil {
		return err
	}
	head := fmt.Sprintf("[job %s] agent %s: %s", j.ID, ag.Name, j.State)
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
	note, err := appendSuper(ctx, tx, j.OwnerID, model.Author{Kind: model.AuthorSystem}, model.MessageSystem, head)
	if err != nil {
		return err
	}
	post.add(func() { Publish(context.WithoutCancel(ctx), j.OwnerID, PushMessage, model.WireMessage(note)) })
	now := clock()
	return model.AddInput(ctx, tx, model.Input{ID: model.NewIDAt(model.PrefixInput, now), SessionID: ses.ID, Text: b.String(), CreatedAt: now})
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
