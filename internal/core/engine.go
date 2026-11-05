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
	Task(ctx context.Context, a Actor, task ID) (Task, error)
	Messages(ctx context.Context, a Actor, task ID, since int64, limit int) ([]Message, error)
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
	now      Clock
}

// Deps are the ports of the Engine.
type Deps struct {
	Store      Store
	Dispatcher Dispatcher
	Notifier   Notifier
	Clock      Clock // nil: time.Now
}

// NewEngine returns an Engine.
func NewEngine(d Deps) *Engine {
	now := d.Clock
	if now == nil {
		now = time.Now
	}
	return &Engine{store: d.Store, dispatch: d.Dispatcher, notify: d.Notifier, now: now}
}

var (
	_ Commands    = (*Engine)(nil)
	_ Enrollment  = (*Engine)(nil)
	_ Queries     = (*Engine)(nil)
	_ DaemonFacts = (*Engine)(nil)
)

// Commands.

func (e *Engine) RegisterMachine(ctx context.Context, a Actor, name, tokenHash string) (Machine, error) {
	return Machine{}, errs.ErrNotImplemented
}

// EnrollMachine creates a pending machine with a pairing code that is
// valid for PairTTL. The same token hash enrolls once.
func (e *Engine) EnrollMachine(ctx context.Context, name, tokenHash string) (Machine, error) {
	return Machine{}, errs.ErrNotImplemented
}

func (e *Engine) ApproveMachine(ctx context.Context, a Actor, code string) (Machine, error) {
	return Machine{}, errs.ErrNotImplemented
}

func (e *Engine) RevokeMachine(ctx context.Context, a Actor, machineID ID) error {
	return errs.ErrNotImplemented
}

func (e *Engine) CreateWorkspace(ctx context.Context, a Actor, name, leader string) (Workspace, error) {
	return Workspace{}, errs.ErrNotImplemented
}

func (e *Engine) AddMember(ctx context.Context, a Actor, ws ID, agentRef string, leader bool) error {
	return errs.ErrNotImplemented
}

func (e *Engine) RemoveMember(ctx context.Context, a Actor, ws ID, agentRef string) error {
	return errs.ErrNotImplemented
}

// CreateTask creates a task with DefaultBudget, appends the brief as the
// first message and wakes the leader.
func (e *Engine) CreateTask(ctx context.Context, a Actor, ws ID, brief string) (Task, error) {
	return Task{}, errs.ErrNotImplemented
}

func (e *Engine) CancelTask(ctx context.Context, a Actor, task ID) error {
	return errs.ErrNotImplemented
}

// RequestSummary appends a system message that asks the leader for a summary.
func (e *Engine) RequestSummary(ctx context.Context, a Actor, task ID) error {
	return errs.ErrNotImplemented
}

// PostMessage appends a message, computes the wake set (Wakes), charges
// the budget (CheckBudget) and enqueues one deliver effect for each
// session to wake. It also updates the task state for reports and summaries.
func (e *Engine) PostMessage(ctx context.Context, a Actor, ws ID, in PostInput) (Message, error) {
	return Message{}, errs.ErrNotImplemented
}

func (e *Engine) SendToSuper(ctx context.Context, a Actor, text string) (Message, error) {
	return Message{}, errs.ErrNotImplemented
}

func (e *Engine) SuperReply(ctx context.Context, a Actor, text string) (Message, error) {
	return Message{}, errs.ErrNotImplemented
}

// Queries.

func (e *Engine) Machines(ctx context.Context, a Actor) ([]Machine, error) {
	return nil, errs.ErrNotImplemented
}

func (e *Engine) Agents(ctx context.Context, a Actor) ([]Agent, error) {
	return nil, errs.ErrNotImplemented
}

func (e *Engine) Workspaces(ctx context.Context, a Actor) ([]Workspace, error) {
	return nil, errs.ErrNotImplemented
}

func (e *Engine) Workspace(ctx context.Context, a Actor, ws ID) (Workspace, []Member, error) {
	return Workspace{}, nil, errs.ErrNotImplemented
}

func (e *Engine) Task(ctx context.Context, a Actor, task ID) (Task, error) {
	return Task{}, errs.ErrNotImplemented
}

func (e *Engine) Messages(ctx context.Context, a Actor, task ID, since int64, limit int) ([]Message, error) {
	return nil, errs.ErrNotImplemented
}

func (e *Engine) Recall(ctx context.Context, a Actor, id ID) (protocol.Object, error) {
	return protocol.Object{}, errs.ErrNotImplemented
}

// Daemon facts.

// MachineConnected registers the machine and its agents, and redelivers
// from the cursors of its sessions.
func (e *Engine) MachineConnected(ctx context.Context, machineID ID, h protocol.Hello) error {
	return errs.ErrNotImplemented
}

func (e *Engine) MachineDisconnected(ctx context.Context, machineID ID) error {
	return errs.ErrNotImplemented
}

func (e *Engine) Heartbeat(ctx context.Context, machineID ID, hb protocol.Heartbeat) error {
	return errs.ErrNotImplemented
}

// SessionStateChanged records the state and the native ref. When a session
// becomes idle, it flushes the pending batch of the session.
func (e *Engine) SessionStateChanged(ctx context.Context, machineID ID, r protocol.SessionStateReport) error {
	return errs.ErrNotImplemented
}

// SessionEvent handles one IR event. Reliable events update read cursors
// and wake rules (for example request.opened). All events go to clients.
func (e *Engine) SessionEvent(ctx context.Context, machineID ID, ev protocol.SessionEvent) error {
	return errs.ErrNotImplemented
}

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
