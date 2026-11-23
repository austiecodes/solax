package model

import (
	"context"
	"time"

	"github.com/austiecodes/solax/internal/errs"
)

// AuthorKind is the kind of a message author.
type AuthorKind string

const (
	AuthorHuman  AuthorKind = "human"
	AuthorAgent  AuthorKind = "agent"
	AuthorSuper  AuthorKind = "super"
	AuthorSystem AuthorKind = "system"
	// AuthorWorkflow is a workflow script. It starts and follows up tasks.
	AuthorWorkflow AuthorKind = "workflow"
)

// Author is the author of a message. It comes from the token, never from
// a request field.
type Author struct {
	Kind AuthorKind
	ID   ID
}

// MessageKind is the kind of a message.
type MessageKind string

const (
	MessageChat   MessageKind = "chat"
	MessageSystem MessageKind = "system"
)

// Message is one entry in the Super Agent conversation of an owner.
type Message struct {
	ID        ID
	OwnerID   ID
	Seq       int64 // per owner
	Author    Author
	Kind      MessageKind
	Text      string
	CreatedAt time.Time
}

// Messages: the Super Agent conversation of each owner.

const messageCols = `id, owner_id, seq, author_kind, author_id, kind, text, created_at`

// GetMessage returns the message with this ID.
func GetMessage(ctx context.Context, db Querier, id ID) (Message, error) {
	return scanMessage(db.QueryRowContext(ctx, `SELECT `+messageCols+` FROM messages WHERE id = ?`, id))
}

// AppendMessage stores m with the next seq of its owner and returns it.
// Run it in Atomic: write transactions are serialised, so the
// read-increment-write of the seq is safe.
func AppendMessage(ctx context.Context, db QueryExecutor, m Message) (Message, error) {
	if m.OwnerID == "" {
		return m, errs.ErrInvalid
	}
	err := db.QueryRowContext(ctx, `UPDATE owners SET super_seq = super_seq + 1 WHERE id = ? RETURNING super_seq`,
		m.OwnerID).Scan(&m.Seq)
	if err != nil {
		return m, mapErr(err)
	}
	_, err = exec(ctx, db, `INSERT INTO messages(`+messageCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.OwnerID, m.Seq, m.Author.Kind, m.Author.ID, m.Kind, m.Text, ms(m.CreatedAt))
	return m, err
}

// ListMessages returns the messages of an owner with seq > since, oldest
// first.
func ListMessages(ctx context.Context, db Querier, ownerID ID, since int64, limit int) ([]Message, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+messageCols+` FROM messages WHERE owner_id = ? AND seq > ?
		ORDER BY seq LIMIT ?`, ownerID, since, limitOr(limit))
	return collect(rows, err, scanMessage)
}

func scanMessage(s scanner) (Message, error) {
	var m Message
	var authorKind, kind string
	var created int64
	if err := s.Scan(&m.ID, &m.OwnerID, &m.Seq, &authorKind, &m.Author.ID, &kind, &m.Text, &created); err != nil {
		return m, mapErr(err)
	}
	m.Author.Kind, m.Kind, m.CreatedAt = AuthorKind(authorKind), MessageKind(kind), tm(created)
	return m, nil
}
