package core

import (
	"context"
	"time"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Actor is the identity behind a call. The API layer builds it from the
// token. An agent actor is scoped to one workspace and one task.
type Actor struct {
	Kind      AuthorKind
	ID        ID
	OwnerID   ID
	Workspace ID // agent tokens only
	Task      ID // agent tokens only
}

// Author returns the message author of the actor.
func (a Actor) Author() Author { return Author{Kind: a.Kind, ID: a.ID} }

// Commands are the write operations for people, agents (through the CLI)
// and the Super Agent (through its extension tools).
type Commands interface {
	// RegisterMachine adds an approved machine with the hash of the token
	// that the machine generated. A client calls it from the machine itself.
	RegisterMachine(ctx context.Context, a Actor, name, tokenHash string) (Machine, error)
	// ApproveMachine approves a pending machine by its pairing code.
	ApproveMachine(ctx context.Context, a Actor, code string) (Machine, error)
	// RevokeMachine makes the token of a machine invalid and stops its sessions.
	RevokeMachine(ctx context.Context, a Actor, machineID ID) error
	CreateWorkspace(ctx context.Context, a Actor, name string, leader string) (Workspace, error)
	AddMember(ctx context.Context, a Actor, ws ID, agentRef string, leader bool) error
	RemoveMember(ctx context.Context, a Actor, ws ID, agentRef string) error
	CreateTask(ctx context.Context, a Actor, ws ID, brief string) (Task, error)
	CancelTask(ctx context.Context, a Actor, task ID) error
	RequestSummary(ctx context.Context, a Actor, task ID) error
	PostMessage(ctx context.Context, a Actor, ws ID, in PostInput) (Message, error)
	// SendToSuper appends a user message to the Super Agent conversation
	// and wakes the Super Agent.
	SendToSuper(ctx context.Context, a Actor, text string) (Message, error)
	// SuperReply appends a Super Agent reply and pushes it to the clients.
	SuperReply(ctx context.Context, a Actor, text string) (Message, error)
}

// Enrollment is the only unauthenticated command. A machine without a
// client token enrolls itself with the hash of its generated token. The
// machine stays pending until a client approves its pairing code.
type Enrollment interface {
	EnrollMachine(ctx context.Context, name, tokenHash string) (Machine, error)
}

// Queries are the read operations. They never write.
type Queries interface {
	Machines(ctx context.Context, a Actor) ([]Machine, error)
	Agents(ctx context.Context, a Actor) ([]Agent, error)
	Workspaces(ctx context.Context, a Actor) ([]Workspace, error)
	Workspace(ctx context.Context, a Actor, ws ID) (Workspace, []Member, error)
	// Tasks returns the tasks of a workspace, oldest first. It includes
	// the general task.
	Tasks(ctx context.Context, a Actor, ws ID) ([]Task, error)
	Task(ctx context.Context, a Actor, task ID) (Task, error)
	Messages(ctx context.Context, a Actor, task ID, since int64, limit int) ([]Message, error)
	// SuperMessages returns the conversation of the user with the Super
	// Agent, with seq > since.
	SuperMessages(ctx context.Context, a Actor, since int64, limit int) ([]Message, error)
	// Recall returns any object by ID with the IDs of related objects
	// (Chain of Recall).
	Recall(ctx context.Context, a Actor, id ID) (protocol.Object, error)
}

// DaemonFacts are the facts that the hub reports. They use past-tense names.
type DaemonFacts interface {
	MachineConnected(ctx context.Context, machineID ID, h protocol.Hello) error
	MachineDisconnected(ctx context.Context, machineID ID) error
	Heartbeat(ctx context.Context, machineID ID, hb protocol.Heartbeat) error
	SessionStateChanged(ctx context.Context, machineID ID, r protocol.SessionStateReport) error
	SessionEvent(ctx context.Context, machineID ID, e protocol.SessionEvent) error
}

// PostInput is a message to append. It has no author: the actor is the author.
type PostInput struct {
	Task       ID // empty: the general task
	Mentions   []string
	Kind       MessageKind
	Status     ReportStatus
	Checkpoint bool
	Text       string
}

// Engine implements Commands, Queries and DaemonFacts. Each command runs in
// one transaction. Side effects go to the effect queue inside the
// transaction and run after commit.
type Engine struct {
	store    Store
	dispatch Dispatcher
	notify   Notifier
	tokens   AgentTokens
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

var (
	_ Commands    = (*Engine)(nil)
	_ Enrollment  = (*Engine)(nil)
	_ Queries     = (*Engine)(nil)
	_ DaemonFacts = (*Engine)(nil)
)

// Helpers shared by the command files.

// requireManager rejects a plain worker agent. Only a person and the Super
// Agent create workspaces, change members and create or cancel tasks.
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

// checkScope rejects a worker agent that acts outside the (workspace, task)
// pair of its token. People and the Super Agent are not scoped.
func checkScope(a Actor, ws, task ID) error {
	if a.Kind != AuthorAgent {
		return nil
	}
	if a.Workspace != ws {
		return errs.ErrForbidden
	}
	if a.Task != "" && task != "" && a.Task != task {
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

// selectMode selects the input mode for a batch: prompt when the session
// is idle, steer for urgent items, follow_up for other items.
func selectMode(state ir.SessionState, urgent bool) ir.InputMode {
	switch {
	case state == ir.SessionIdle:
		return ir.ModePrompt
	case urgent:
		return ir.ModeSteer
	default:
		return ir.ModeFollowUp
	}
}
