package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
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
func (e *Engine) SendToSuper(ctx context.Context, a Actor, text string) (Message, error) {
	if err := requireHuman(a); err != nil {
		return Message{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Message{}, fmt.Errorf("%w: empty message", errs.ErrInvalid)
	}
	var m Message
	err := e.store.Atomic(ctx, func(tx Tx) error {
		ses, err := e.superSession(ctx, tx)
		if err != nil {
			return err
		}
		now := e.clock()
		m, err = tx.Messages().Append(ctx, Message{
			ID: NewIDAt(PrefixMessage, now), OwnerID: a.OwnerID, Author: a.Author(), Kind: MessageChat, Text: text, CreatedAt: now,
		})
		if err != nil {
			return err
		}
		return tx.Inputs().Add(ctx, Input{ID: NewIDAt(PrefixInput, now), SessionID: ses.ID, Text: "[user] " + text, CreatedAt: now})
	})
	if err != nil {
		return Message{}, err
	}
	e.publish(ctx, a.OwnerID, PushMessage, MessageDTO(m))
	e.wake()
	return m, nil
}

// SuperReply appends a reply of the Super Agent. Plain agent messages of
// its session become replies too; this is for an explicit reply command.
func (e *Engine) SuperReply(ctx context.Context, a Actor, text string) (Message, error) {
	if a.Kind != AuthorSuper {
		return Message{}, errs.ErrForbidden
	}
	var m Message
	err := e.store.Atomic(ctx, func(tx Tx) error {
		var err error
		m, err = e.appendSuper(ctx, tx, a.OwnerID, Author{Kind: AuthorSuper, ID: a.ID}, MessageChat, text)
		return err
	})
	if err == nil {
		e.publish(ctx, a.OwnerID, PushMessage, MessageDTO(m))
	}
	return m, err
}

// superSession returns the session of the Super Agent, and creates it on
// first use. It has no job.
func (e *Engine) superSession(ctx context.Context, tx Tx) (Session, error) {
	ag, err := superAgent(ctx, tx)
	if err != nil {
		return Session{}, err
	}
	ses, err := tx.Sessions().For(ctx, ag.ID, "")
	if err == nil {
		return ses, nil
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return Session{}, err
	}
	now := e.clock()
	ses = Session{ID: NewIDAt(PrefixSession, now), AgentID: ag.ID, State: SessionStopped, UpdatedAt: now}
	return ses, tx.Sessions().Save(ctx, ses)
}

func (e *Engine) appendSuper(ctx context.Context, tx Tx, owner ID, author Author, kind MessageKind, text string) (Message, error) {
	now := e.clock()
	return tx.Messages().Append(ctx, Message{
		ID: NewIDAt(PrefixMessage, now), OwnerID: owner, Author: author, Kind: kind, Text: text, CreatedAt: now,
	})
}

// superEvent turns agent messages of the Super Agent session into replies.
func (e *Engine) superEvent(ctx context.Context, tx Tx, ses Session, ev ir.Event, post *afterCommit) error {
	if ev.Type != ir.ItemCompleted || ev.Item == nil || ev.Item.Kind != ir.ItemAgentMessage {
		return nil
	}
	text := strings.TrimSpace(ev.Item.Text)
	if text == "" {
		return nil
	}
	ag, err := tx.Agents().Get(ctx, ses.AgentID)
	if err != nil {
		return err
	}
	m, err := tx.Machines().Get(ctx, ag.MachineID)
	if err != nil {
		return err
	}
	msg, err := e.appendSuper(ctx, tx, m.OwnerID, Author{Kind: AuthorSuper, ID: ag.ID}, MessageChat, text)
	if err != nil {
		return err
	}
	post.add(func() { e.publish(context.WithoutCancel(ctx), m.OwnerID, PushMessage, MessageDTO(msg)) })
	return nil
}

// jobFinished reports a finished job of the Super Agent back to it: a
// system note in the conversation, and an input in its inbox.
func (e *Engine) jobFinished(ctx context.Context, tx Tx, j Job, ag Agent, post *afterCommit) error {
	if j.RequestedBy != AuthorSuper {
		return nil
	}
	ses, err := e.superSession(ctx, tx)
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
		b.WriteString("\nerror: " + j.Error)
	}
	if j.Result != "" {
		b.WriteString("\nresult:\n" + j.Result)
	}
	note, err := e.appendSuper(ctx, tx, j.OwnerID, Author{Kind: AuthorSystem}, MessageSystem, head)
	if err != nil {
		return err
	}
	post.add(func() { e.publish(context.WithoutCancel(ctx), j.OwnerID, PushMessage, MessageDTO(note)) })
	now := e.clock()
	return tx.Inputs().Add(ctx, Input{ID: NewIDAt(PrefixInput, now), SessionID: ses.ID, Text: b.String(), CreatedAt: now})
}

// MessageDTO converts a conversation message to its wire form.
func MessageDTO(m Message) protocol.Message {
	return protocol.Message{
		ID: string(m.ID), Seq: m.Seq, Author: protocol.Author{Kind: string(m.Author.Kind), ID: string(m.Author.ID)},
		Kind: string(m.Kind), Text: m.Text, CreatedAt: m.CreatedAt,
	}
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
