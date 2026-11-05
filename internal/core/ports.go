package core

import (
	"context"
	"encoding/json"
	"time"

	"github.com/austiecodes/solax/pkg/protocol"
)

// Store is the storage port. internal/store/sqlite implements it.
type Store interface {
	// Atomic runs fn in one transaction. Effects enqueued in fn run only
	// after the transaction commits.
	Atomic(ctx context.Context, fn func(tx Tx) error) error
	Close() error
}

// Tx gives the repositories inside one transaction.
type Tx interface {
	Machines() MachineRepo
	Agents() AgentRepo
	Workspaces() WorkspaceRepo
	Tasks() TaskRepo
	Messages() MessageRepo
	Sessions() SessionRepo
	Cursors() CursorRepo
	Effects() EffectRepo
}

type MachineRepo interface {
	Get(ctx context.Context, id ID) (Machine, error)
	ByPairCode(ctx context.Context, code string) (Machine, error)
	Save(ctx context.Context, m Machine) error
	List(ctx context.Context) ([]Machine, error)
}

type AgentRepo interface {
	Get(ctx context.Context, id ID) (Agent, error)
	ByName(ctx context.Context, machineID ID, name string) (Agent, error)
	Save(ctx context.Context, a Agent) error
	List(ctx context.Context) ([]Agent, error)
}

type WorkspaceRepo interface {
	Get(ctx context.Context, id ID) (Workspace, error)
	Save(ctx context.Context, w Workspace) error
	List(ctx context.Context) ([]Workspace, error)
	Members(ctx context.Context, id ID) ([]Member, error)
	SaveMember(ctx context.Context, m Member) error
	DeleteMember(ctx context.Context, workspaceID, agentID ID) error
}

type TaskRepo interface {
	Get(ctx context.Context, id ID) (Task, error)
	General(ctx context.Context, workspaceID ID) (Task, error)
	Save(ctx context.Context, t Task) error
	Open(ctx context.Context, workspaceID ID) ([]Task, error)
}

type MessageRepo interface {
	Get(ctx context.Context, id ID) (Message, error)
	// Append stores m with the next workspace seq and returns it.
	Append(ctx context.Context, m Message) (Message, error)
	// Since returns messages of a task with seq > since, oldest first.
	Since(ctx context.Context, taskID ID, since int64, limit int) ([]Message, error)
}

type SessionRepo interface {
	Get(ctx context.Context, id ID) (Session, error)
	For(ctx context.Context, agentID, taskID ID) (Session, error)
	Save(ctx context.Context, s Session) error
	OnMachine(ctx context.Context, machineID ID) ([]Session, error)
}

type CursorRepo interface {
	Get(ctx context.Context, sessionID, taskID ID) (Cursor, error)
	Save(ctx context.Context, c Cursor) error
}

// EffectRepo stores durable side effects (the outbox).
type EffectRepo interface {
	Enqueue(ctx context.Context, kind EffectKind, key string, payload json.RawMessage) error
}

// EffectKind is the kind of a durable side effect.
type EffectKind string

const (
	EffectDeliver      EffectKind = "deliver"       // send a batch to a session
	EffectEnsure       EffectKind = "ensure"        // start or resume a session
	EffectNotifyClient EffectKind = "notify_client" // push a client stream event
)

// Dispatcher sends commands to daemons. internal/server/hub implements it.
type Dispatcher interface {
	// Send sends a command to the daemon of a machine and waits for its ack.
	Send(ctx context.Context, machineID ID, env protocol.Envelope) (protocol.Ack, error)
	Online(machineID ID) bool
}

// Notifier pushes events to connected clients. internal/server/stream
// implements it.
type Notifier interface {
	Publish(ctx context.Context, ownerID ID, event string, payload any) error
}

// Clock returns the current time. Tests replace it.
type Clock func() time.Time
