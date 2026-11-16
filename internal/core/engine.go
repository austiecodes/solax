package core

import (
	"encoding/json"
	"time"

	"github.com/austiecodes/solax/internal/errs"
)

// Actor is the identity behind a call. The API layer builds it from the
// token.
type Actor struct {
	Kind    AuthorKind
	ID      ID
	OwnerID ID
}

// Author returns the message author of the actor.
func (a Actor) Author() Author { return Author{Kind: a.Kind, ID: a.ID} }

// Engine holds the domain rules. The API calls its commands and queries,
// and the hub reports daemon facts to it. Each command runs in one
// transaction.
type Engine struct {
	store    Store
	dispatch Dispatcher
	notify   Notifier
	tokens   AgentTokens
	kick     func() // wakes the delivery loop; see OnWork
	now      Clock
}

// Deps are the ports of the Engine.
type Deps struct {
	Store      Store
	Dispatcher Dispatcher
	Notifier   Notifier
	// Tokens issues the agent token of each session.ensure.
	Tokens AgentTokens
	Clock  Clock // nil: time.Now
}

// NewEngine returns an Engine.
func NewEngine(d Deps) *Engine {
	now := d.Clock
	if now == nil {
		now = time.Now
	}
	return &Engine{store: d.Store, dispatch: d.Dispatcher, notify: d.Notifier, tokens: d.Tokens, now: now}
}

// Helpers shared by the command files.

// requireManager rejects a plain worker agent. Only a person and the Super
// Agent dispatch, follow up and cancel jobs.
func requireManager(a Actor) error {
	if a.Kind != AuthorHuman && a.Kind != AuthorSuper {
		return errs.ErrForbidden
	}
	return nil
}

// requireHuman rejects everything but a person (a client token).
func requireHuman(a Actor) error {
	if a.Kind != AuthorHuman {
		return errs.ErrForbidden
	}
	return nil
}

// owned hides objects of other owners: they look absent, not forbidden.
func owned(a Actor, owner ID) error {
	if a.OwnerID == "" || a.OwnerID != owner {
		return errs.ErrNotFound
	}
	return nil
}

// zeroTime clears a time field.
var zeroTime time.Time

// clock returns the current time in the precision the store keeps, so a
// value read back equals the value written.
func (e *Engine) clock() time.Time { return e.now().UTC().Truncate(time.Millisecond) }

// mustJSON encodes a wire value for an Envelope payload.
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return b
}
