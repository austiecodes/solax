// Package service holds the domain rules as plain functions, one file for
// each domain. It is the only writer of domain state: controllers call it
// for each request, and the hub calls it with what daemons report. A
// command runs in one model.Atomic transaction.
package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/pkg/protocol"
)

// The service reaches daemons, clients and the token signer through these
// functions. The server sets them once at start, before it serves; tests
// replace them. The defaults act as if no daemon and no client is
// connected.
var (
	// Now returns the current time.
	Now = time.Now
	// Send sends a command to the daemon of a machine and waits for its
	// ack.
	Send = func(ctx context.Context, machineID model.ID, env protocol.Envelope) (protocol.Ack, error) {
		return protocol.Ack{}, errs.ErrUnavailable
	}
	// Online reports whether the daemon of a machine is connected.
	Online = func(machineID model.ID) bool { return false }
	// Disconnect closes the connection of a machine, if any (revocation).
	Disconnect = func(machineID model.ID) {}
	// Publish pushes an event to the connected clients of an owner. The
	// stream is best-effort: a client that missed an event reloads from
	// the REST API.
	Publish = func(ctx context.Context, ownerID model.ID, event string, payload any) {}
	// IssueToken issues the agent token of a session.
	IssueToken = func(ctx context.Context, r model.TokenRequest) (string, error) { return "", nil }
	// Wake wakes the delivery loop after a commit that added inputs or
	// brought a machine online.
	Wake = func() {}
)

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
func clock() time.Time { return Now().UTC().Truncate(time.Millisecond) }

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
