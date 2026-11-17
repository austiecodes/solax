// Package engine holds the domain rules. The Engine is the only writer of
// domain state. It keeps the state in the model database, and reaches
// daemons and clients through small interfaces: Dispatcher, Notifier and
// TokenIssuer.
package engine

import (
	"context"
	"encoding/json"
	"time"

	"github.com/austiecodes/solax/internal/server/model"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Engine holds the domain rules. The API calls its commands and queries,
// and the hub reports daemon facts to it. Each command runs in one
// transaction.
type Engine struct {
	dispatch Dispatcher
	notify   Notifier
	tokens   TokenIssuer
	kick     func() // wakes the delivery loop; see OnWork
	now      Clock
}

// Config holds what an Engine needs. All fields are optional; the model
// database must be open.
type Config struct {
	Dispatcher Dispatcher
	Notifier   Notifier
	// Tokens issues the agent token of each session.ensure.
	Tokens TokenIssuer
	Clock  Clock // nil: time.Now
}

// New returns an Engine.
func New(cfg Config) *Engine {
	now := cfg.Clock
	if now == nil {
		now = time.Now
	}
	return &Engine{dispatch: cfg.Dispatcher, notify: cfg.Notifier, tokens: cfg.Tokens, now: now}
}

// Dispatcher sends commands to daemons. internal/server/hub implements it.
type Dispatcher interface {
	// Send sends a command to the daemon of a machine and waits for its ack.
	Send(ctx context.Context, machineID model.ID, env protocol.Envelope) (protocol.Ack, error)
	Online(machineID model.ID) bool
	// Disconnect closes the connection of a machine, if any (revocation).
	Disconnect(machineID model.ID)
}

// Notifier pushes events to connected clients. internal/server/stream
// implements it.
type Notifier interface {
	Publish(ctx context.Context, ownerID model.ID, event string, payload any) error
}

// TokenIssuer issues the agent tokens that session.ensure carries.
// internal/auth implements it; the Engine never signs a token itself.
type TokenIssuer interface {
	IssueToken(ctx context.Context, r model.TokenRequest) (string, error)
}

// Clock returns the current time. Tests replace it.
type Clock func() time.Time

// Helpers shared by the command files.

// requireManager rejects a plain worker agent. Only a person and the Super
// Agent dispatch, follow up and cancel jobs.
func requireManager(a model.Actor) error {
	if a.Kind != model.AuthorHuman && a.Kind != model.AuthorSuper {
		return errs.ErrForbidden
	}
	return nil
}

// requireHuman rejects everything but a person (a client token).
func requireHuman(a model.Actor) error {
	if a.Kind != model.AuthorHuman {
		return errs.ErrForbidden
	}
	return nil
}

// owned hides objects of other owners: they look absent, not forbidden.
func owned(a model.Actor, owner model.ID) error {
	if a.OwnerID == "" || a.OwnerID != owner {
		return errs.ErrNotFound
	}
	return nil
}

// zeroTime clears a time field.
var zeroTime time.Time

// clock returns the current time in the precision the database keeps, so a
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

// Client stream events (pushes). The payloads are protocol types.
const (
	PushAgents   = "agents.changed"   // no payload: reload the agent list
	PushMachines = "machines.changed" // no payload: reload the machine list
	PushJob      = "job"              // protocol.Job
	PushSession  = "session.event"    // protocol.SessionEventNote
	PushMessage  = "super.message"    // protocol.Message of the Super Agent conversation
)

// publish pushes a client event. The stream is best-effort: a client that
// missed an event reloads from the REST API.
func (e *Engine) publish(ctx context.Context, owner model.ID, event string, payload any) {
	if e.notify != nil {
		_ = e.notify.Publish(ctx, owner, event, payload)
	}
}

// OnWork sets the function that wakes the delivery loop. The server sets
// it; the engine calls it after a commit that added inputs or brought a
// machine online.
func (e *Engine) OnWork(fn func()) { e.kick = fn }

func (e *Engine) wake() {
	if e.kick != nil {
		e.kick()
	}
}
