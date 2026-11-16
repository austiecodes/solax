package core

import (
	"context"

	"github.com/austiecodes/solax/pkg/ir"
)

// Store keeps the domain state. internal/store/sqlite implements it.
type Store interface {
	// Atomic runs fn in one write transaction. Write transactions are
	// serialised. fn must not call Atomic or Read.
	Atomic(ctx context.Context, fn func(tx Tx) error) error
	// Read runs fn in one read-only transaction: a consistent snapshot that
	// does not block writers. Any write in fn fails.
	Read(ctx context.Context, fn func(tx Tx) error) error
}

// Tx reads and writes the domain state inside one transaction. A lookup
// that finds nothing returns errs.ErrNotFound. A Save inserts or updates
// by ID.
type Tx interface {
	SaveOwner(ctx context.Context, o Owner) error
	Owners(ctx context.Context) ([]Owner, error)

	Machine(ctx context.Context, id ID) (Machine, error)
	MachineByPairCode(ctx context.Context, code string) (Machine, error)
	MachineByTokenHash(ctx context.Context, hash string) (Machine, error)
	SaveMachine(ctx context.Context, m Machine) error
	Machines(ctx context.Context) ([]Machine, error)

	Agent(ctx context.Context, id ID) (Agent, error)
	// AgentByName returns the live (not deleted) agent with this name.
	AgentByName(ctx context.Context, name string) (Agent, error)
	// SaveAgent fails with errs.ErrConflict when another live agent has
	// the same name.
	SaveAgent(ctx context.Context, a Agent) error
	// Agents returns the live agents.
	Agents(ctx context.Context) ([]Agent, error)

	Message(ctx context.Context, id ID) (Message, error)
	// AppendMessage stores m with the next seq of its owner and returns it.
	AppendMessage(ctx context.Context, m Message) (Message, error)
	// Messages returns the messages of an owner with seq > since, oldest
	// first.
	Messages(ctx context.Context, ownerID ID, since int64, limit int) ([]Message, error)

	Session(ctx context.Context, id ID) (Session, error)
	// SessionFor returns the session of an agent for a job ("" for the
	// Super Agent).
	SessionFor(ctx context.Context, agentID, jobID ID) (Session, error)
	SaveSession(ctx context.Context, s Session) error
	SessionsOnMachine(ctx context.Context, machineID ID) ([]Session, error)

	Job(ctx context.Context, id ID) (Job, error)
	SaveJob(ctx context.Context, j Job) error
	// Jobs returns the newest jobs of an owner first.
	Jobs(ctx context.Context, ownerID ID, limit int) ([]Job, error)

	AddInput(ctx context.Context, in Input) error
	// PendingInputs returns the unsent inputs of a session, oldest first.
	PendingInputs(ctx context.Context, sessionID ID) ([]Input, error)
	MarkInputsSent(ctx context.Context, ids []ID) error
	// PendingSessions returns the sessions that have unsent inputs.
	PendingSessions(ctx context.Context) ([]ID, error)

	// AddSessionEvent stores a reliable IR event. It reports false for a
	// seq that is already stored (a resend after reconnect).
	AddSessionEvent(ctx context.Context, sessionID ID, e ir.Event) (bool, error)
	// SessionEvents returns events with seq > after, oldest first.
	SessionEvents(ctx context.Context, sessionID ID, after int64, limit int) ([]ir.Event, error)
}
