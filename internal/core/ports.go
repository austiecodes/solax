package core

import (
	"context"
	"encoding/json"
	"time"

	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Store is the storage port. internal/store/sqlite implements it.
type Store interface {
	// Atomic runs fn in one write transaction. Write transactions are
	// serialised. Effects enqueued in fn run only after the transaction
	// commits. fn must not call Atomic or Read.
	Atomic(ctx context.Context, fn func(tx Tx) error) error
	// Read runs fn in one read-only transaction: a consistent snapshot that
	// does not block writers. Any write in fn fails.
	Read(ctx context.Context, fn func(tx Tx) error) error
	Close() error
}

// Tx gives the repositories inside one transaction.
type Tx interface {
	Owners() OwnerRepo
	Machines() MachineRepo
	Agents() AgentRepo
	Workspaces() WorkspaceRepo
	Tasks() TaskRepo
	Messages() MessageRepo
	TaskEvents() TaskEventRepo
	Sessions() SessionRepo
	Cursors() CursorRepo
	Effects() EffectRepo
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

type WorkspaceRepo interface {
	Get(ctx context.Context, id ID) (Workspace, error)
	ByName(ctx context.Context, ownerID ID, name string) (Workspace, error)
	Save(ctx context.Context, w Workspace) error
	List(ctx context.Context) ([]Workspace, error)
	Members(ctx context.Context, id ID) ([]Member, error)
	SaveMember(ctx context.Context, m Member) error
	DeleteMember(ctx context.Context, workspaceID, agentID ID) error
}

type TaskRepo interface {
	Get(ctx context.Context, id ID) (Task, error)
	General(ctx context.Context, workspaceID ID) (Task, error)
	// Create stores a new task. ErrConflict if the ID exists.
	Create(ctx context.Context, t Task) error
	// Update stores every mutable field of t except Spent, if the stored
	// rev is prevRev. Otherwise it returns ErrConflict. The caller sets
	// t.Rev (normally prevRev+1).
	Update(ctx context.Context, t Task, prevRev int64) error
	// SetSpent stores the spent budget. It does not change the rev: budget
	// charging is not a state transition.
	SetSpent(ctx context.Context, id ID, s Spent) error
	// List returns all tasks of a workspace, oldest first.
	List(ctx context.Context, workspaceID ID) ([]Task, error)
	// Open returns the tasks of a workspace that are not terminal.
	Open(ctx context.Context, workspaceID ID) ([]Task, error)
	// Children returns the child tasks of a task, oldest first.
	Children(ctx context.Context, parentID ID) ([]Task, error)
}

type MessageRepo interface {
	Get(ctx context.Context, id ID) (Message, error)
	// Append stores m with the next seq of its stream (the workspace, or
	// the Super Agent conversation of m.OwnerID when m.WorkspaceID is
	// empty) and returns it.
	Append(ctx context.Context, m Message) (Message, error)
	// Since returns messages of a task with seq > since, oldest first.
	Since(ctx context.Context, taskID ID, since int64, limit int) ([]Message, error)
	// SuperSince returns the Super Agent conversation of an owner with
	// seq > since, oldest first.
	SuperSince(ctx context.Context, ownerID ID, since int64, limit int) ([]Message, error)
}

// TaskEventRepo stores the append-only input log of each task.
type TaskEventRepo interface {
	// Append stores an event with the next seq of its task and returns it.
	Append(ctx context.Context, e TaskEvent) (TaskEvent, error)
	// Since returns events of a task with seq > after, oldest first: at
	// most limit events, and at most maxBytes of payload (always at least
	// one event if there is one). maxBytes <= 0 means no byte bound.
	Since(ctx context.Context, taskID ID, after int64, limit, maxBytes int) ([]TaskEvent, error)
	// Last returns the highest seq of a task, or 0.
	Last(ctx context.Context, taskID ID) (int64, error)
}

type SessionRepo interface {
	Get(ctx context.Context, id ID) (Session, error)
	For(ctx context.Context, agentID, taskID ID) (Session, error)
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

type CursorRepo interface {
	Get(ctx context.Context, sessionID, taskID ID) (Cursor, error)
	Save(ctx context.Context, c Cursor) error
}

// Effect is one pending durable side effect.
//
// An effect is a signal, not a message: there is at most one row per
// (kind, key), and a handler reads the current state from the store
// instead of trusting the payload. Enqueueing an existing effect bumps its
// Generation, so a run that started before the enqueue cannot complete
// (delete) it and the new signal is not lost.
type Effect struct {
	Kind       EffectKind
	Key        string
	Payload    json.RawMessage
	Generation int64     // increases on every Enqueue of (kind, key)
	ClaimToken string    // set by ClaimDue; identifies the current claim
	Attempts   int       // failed runs so far
	RunAt      time.Time // earliest time to run
}

// EffectRepo stores durable side effects (the outbox). The producer and the
// outbox both reach it through Tx.Effects, so neither needs the concrete
// store.
type EffectRepo interface {
	// Enqueue adds the effect, or signals an existing one: it replaces the
	// payload, bumps the generation and makes it due now. A claimed effect
	// stays claimed; Complete then sees the new generation and requeues it.
	Enqueue(ctx context.Context, kind EffectKind, key string, payload json.RawMessage) error
	// EnqueueAt is Enqueue with an explicit run time (timers).
	EnqueueAt(ctx context.Context, kind EffectKind, key string, payload json.RawMessage, runAt time.Time) error
	// Cancel removes an effect whatever its state (for example a timer that
	// was disarmed). It is not an error if there is none.
	Cancel(ctx context.Context, kind EffectKind, key string) error
	// ClaimDue returns up to limit effects of kind that are due at now and
	// not claimed, and claims them until ClaimTTL after now with a new
	// ClaimToken. Claiming keeps two outbox workers (or a process that
	// restarted mid-effect) from running one effect at the same time.
	ClaimDue(ctx context.Context, kind EffectKind, now time.Time, limit int) ([]Effect, error)
	// Holds reports whether e is still claimed by e.ClaimToken. A handler
	// that commits a computation checks it in the commit transaction.
	Holds(ctx context.Context, e Effect) (bool, error)
	// Complete finishes a claimed run. If the effect was not enqueued again
	// since the claim (same generation), it is removed. If it was, the
	// claim is released and the effect is due now. If the claim was lost
	// (another token), nothing changes. It reports whether the row was removed.
	Complete(ctx context.Context, e Effect) (removed bool, err error)
	// Reschedule releases a claimed run with its attempt count and the
	// next run time. It does nothing if the claim was lost.
	Reschedule(ctx context.Context, e Effect, runAt time.Time, attempts int, lastErr string) error
}

// ClaimTTL is how long a claimed effect stays claimed when its runner
// dies. It must exceed the longest handler run.
const ClaimTTL = 2 * time.Minute

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
	// Disconnect closes the connection of a machine, if any (revocation).
	Disconnect(machineID ID)
}

// Notifier pushes events to connected clients. internal/server/stream
// implements it.
type Notifier interface {
	Publish(ctx context.Context, ownerID ID, event string, payload any) error
}

// TokenRequest describes the agent token of one session.
type TokenRequest struct {
	OwnerID   ID
	AgentID   ID
	SessionID ID
	Workspace ID
	Task      ID // empty for the Super Agent
	Super     bool
	TTL       time.Duration
}

// AgentTokens issues the agent tokens that session.ensure carries.
// internal/auth implements it; the Engine never signs a token itself.
type AgentTokens interface {
	IssueToken(ctx context.Context, r TokenRequest) (string, error)
}

// Clock returns the current time. Tests replace it.
type Clock func() time.Time
