// Package core holds the domain model and the delivery engine. The Engine
// is the only writer of domain state. It reaches storage, daemons and
// clients only through the ports in ports.go.
package core

import (
	"time"

	"github.com/austiecodes/solax/pkg/ir"
)

// Machine is a host that runs "solax daemon".
type Machine struct {
	ID        ID
	OwnerID   ID
	Name      string
	State     MachineState
	TokenHash string    // SHA-256 of the token that the machine generated
	PairCode  string    // pending only: the code that a client approves
	PairUntil time.Time // pending only: the code expires
	LastSeen  time.Time
	Online    bool
	Slots     int // free session slots from the last heartbeat
}

// MachineState is the approval state of a machine.
type MachineState string

const (
	MachinePending  MachineState = "pending"  // enrolled, waits for a client to approve
	MachineApproved MachineState = "approved" // its token is valid
	MachineRevoked  MachineState = "revoked"
)

// PairTTL is how long a pairing code is valid.
const PairTTL = 24 * time.Hour

// Agent references an agent that the local config of a machine defines. The server does
// not define agents.
type Agent struct {
	ID        ID
	MachineID ID
	Name      string
	Driver    ir.Kind
	Role      AgentRole
	Dir       string
	Model     string
}

// AgentRole is the system role of an agent.
type AgentRole string

const (
	RoleWorker AgentRole = ""
	RoleSuper  AgentRole = "super"
)

// Workspace is a long-lived group of agents with a shared message log.
type Workspace struct {
	ID        ID
	OwnerID   ID
	Name      string
	LeaderID  ID // agent ID; exactly one leader
	NextSeq   int64
	CreatedAt time.Time
}

// MemberRole is the role of an agent in a workspace.
type MemberRole string

const (
	MemberLeader MemberRole = "leader"
	MemberPlain  MemberRole = "member"
)

// Member is an agent in a workspace.
type Member struct {
	WorkspaceID ID
	AgentID     ID
	Role        MemberRole
}

// TaskState is the state of a task.
type TaskState string

const (
	TaskOpen        TaskState = "open"
	TaskRunning     TaskState = "running"
	TaskSummarizing TaskState = "summarizing"
	TaskDone        TaskState = "done"
	TaskBlocked     TaskState = "blocked"
	TaskCancelled   TaskState = "cancelled"
)

// Task is one unit of work in a workspace, with its own message stream.
// Each workspace has a general task.
type Task struct {
	ID          ID
	WorkspaceID ID
	Title       string
	Brief       string
	General     bool
	State       TaskState
	Budget      Budget
	Spent       Spent
	CreatedAt   time.Time
}

// Budget holds the hard limits of a task. When a limit is reached, the
// leader must write the summary.
type Budget struct {
	MaxAgentTurns       int
	MaxWallTime         time.Duration
	MaxHopsWithoutHuman int
}

// DefaultBudget is the budget of a new task.
var DefaultBudget = Budget{MaxAgentTurns: 40, MaxWallTime: 2 * time.Hour, MaxHopsWithoutHuman: 15}

// Spent is the used part of a budget.
type Spent struct {
	AgentTurns       int
	HopsWithoutHuman int
}

// AuthorKind is the kind of a message author.
type AuthorKind string

const (
	AuthorHuman  AuthorKind = "human"
	AuthorAgent  AuthorKind = "agent"
	AuthorSuper  AuthorKind = "super"
	AuthorSystem AuthorKind = "system"
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
	MessageChat    MessageKind = "chat"
	MessageReport  MessageKind = "report"
	MessageSummary MessageKind = "summary"
	MessageSystem  MessageKind = "system"
)

// ReportStatus is the status of a report message.
type ReportStatus string

const (
	ReportProgress ReportStatus = "progress"
	ReportDone     ReportStatus = "done"
	ReportBlocked  ReportStatus = "blocked"
	ReportFailed   ReportStatus = "failed"
)

// Message is one append-only entry in a task stream.
type Message struct {
	ID          ID
	WorkspaceID ID
	TaskID      ID
	Seq         int64 // per workspace
	Author      Author
	Kind        MessageKind
	Status      ReportStatus // report only
	Checkpoint  bool         // summary only: keep the task running
	Mentions    []ID         // agent IDs
	Text        string
	CreatedAt   time.Time
}

// Session is one agent process for one (member, task). The daemon owns the
// process. The server keeps its state and its native ref.
type Session struct {
	ID        ID
	AgentID   ID
	TaskID    ID // empty for the Super Agent session
	State     ir.SessionState
	Ref       *ir.SessionRef
	UpdatedAt time.Time
}

// Cursor tracks delivery of one task stream to one session.
type Cursor struct {
	SessionID    ID
	TaskID       ID
	DeliveredSeq int64
	ReadSeq      int64
}
