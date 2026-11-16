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
	Close() error
}

// Tx gives the tables inside one transaction.
type Tx interface {
	Owners() OwnerRepo
	Machines() MachineRepo
	Agents() AgentRepo
	Messages() MessageRepo
	Sessions() SessionRepo
	Jobs() JobRepo
	Inputs() InputRepo
	SessionEvents() SessionEventRepo
}

type OwnerRepo interface {
	Get(ctx context.Context, id ID) (Owner, error)
	Save(ctx context.Context, o Owner) error
	List(ctx context.Context) ([]Owner, error)
}

type MachineRepo interface {
	Get(ctx context.Context, id ID) (Machine, error)
	ByPairCode(ctx context.Context, code string) (Machine, error)
	ByTokenHash(ctx context.Context, hash string) (Machine, error)
	Save(ctx context.Context, m Machine) error
	List(ctx context.Context) ([]Machine, error)
}

type AgentRepo interface {
	Get(ctx context.Context, id ID) (Agent, error)
	ByName(ctx context.Context, machineID ID, name string) (Agent, error)
	// Named returns the live (not deleted) agent with this name.
	Named(ctx context.Context, name string) (Agent, error)
	Save(ctx context.Context, a Agent) error
	// List returns the live agents.
	List(ctx context.Context) ([]Agent, error)
}

type MessageRepo interface {
	Get(ctx context.Context, id ID) (Message, error)
	// Append stores m with the next seq of its owner and returns it.
	Append(ctx context.Context, m Message) (Message, error)
	// Since returns the messages of an owner with seq > since, oldest first.
	Since(ctx context.Context, ownerID ID, since int64, limit int) ([]Message, error)
}

type SessionRepo interface {
	Get(ctx context.Context, id ID) (Session, error)
	// For returns the session of an agent for a job ("" for the Super Agent).
	For(ctx context.Context, agentID, jobID ID) (Session, error)
	Save(ctx context.Context, s Session) error
	OnMachine(ctx context.Context, machineID ID) ([]Session, error)
}

type JobRepo interface {
	Get(ctx context.Context, id ID) (Job, error)
	BySession(ctx context.Context, sessionID ID) (Job, error)
	Save(ctx context.Context, j Job) error
	// List returns the newest jobs of an owner first.
	List(ctx context.Context, ownerID ID, limit int) ([]Job, error)
}

type InputRepo interface {
	Add(ctx context.Context, in Input) error
	// Pending returns the unsent inputs of a session, oldest first.
	Pending(ctx context.Context, sessionID ID) ([]Input, error)
	MarkSent(ctx context.Context, ids []ID) error
	// PendingSessions returns the sessions that have unsent inputs.
	PendingSessions(ctx context.Context) ([]ID, error)
}

type SessionEventRepo interface {
	// Add stores a reliable IR event. It reports false for a seq that is
	// already stored (a resend after reconnect).
	Add(ctx context.Context, sessionID ID, e ir.Event) (bool, error)
	// Since returns events with seq > after, oldest first.
	Since(ctx context.Context, sessionID ID, after int64, limit int) ([]ir.Event, error)
}
