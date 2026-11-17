package core

import (
	"time"

	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Owner is the person who owns an installation. v1 has one owner; the
// schema keeps owner_id so that team support can come later.
type Owner struct {
	ID        ID
	CreatedAt time.Time
}

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
	// Runtimes are the coding agent CLIs that the daemon detected.
	Runtimes []protocol.RuntimeProbe
}

// HasRuntime reports whether the daemon detected a working runtime of kind k.
func (m Machine) HasRuntime(k ir.Kind) bool {
	for _, d := range m.Runtimes {
		if d.Kind == k && d.OK {
			return true
		}
	}
	return false
}

// Reports tells whether the daemon reported a runtime of kind k, working
// or not.
func (m Machine) Reports(k ir.Kind) bool {
	for _, d := range m.Runtimes {
		if d.Kind == k {
			return true
		}
	}
	return false
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

// Agent is a coding agent that a person set up on one machine. The server
// defines it; the daemon of the machine runs its sessions. The name is
// unique: the Super Agent routes jobs by name, with the description as the
// hint for what the agent is good at.
type Agent struct {
	ID          ID
	MachineID   ID
	Name        string
	Description string
	// Instructions are the system prompt of the sessions of the agent,
	// after the Solax role prompt. Empty: the role prompt only.
	Instructions string
	Runtime      ir.Kind
	Role         AgentRole
	Dir          string
	Model        string
	Options      map[string]any // runtime-specific, for example pi "thinking"
	Deleted      bool
	CreatedAt    time.Time
}

// AgentRole is the system role of an agent.
type AgentRole string

const (
	RoleWorker AgentRole = ""
	RoleSuper  AgentRole = "super"
)

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
	MessageChat   MessageKind = "chat"
	MessageSystem MessageKind = "system"
)

// Message is one entry in the Super Agent conversation of an owner.
type Message struct {
	ID        ID
	OwnerID   ID
	Seq       int64 // per owner
	Author    Author
	Kind      MessageKind
	Text      string
	CreatedAt time.Time
}

// Session is one agent process: the session of a job, or the Super Agent
// session. The daemon owns the
// process. The server keeps its state and its native ref.
type Session struct {
	ID        ID
	AgentID   ID
	JobID     ID // the job of a worker session; empty for the Super Agent session
	State     ir.SessionState
	Ref       *ir.SessionRef
	Live      bool  // the daemon reports the process as running
	LastSeq   int64 // last stored IR event
	UpdatedAt time.Time
}

// SessionStopped is the state of a session that has no process and never
// had one, or whose process ended. An input wakes it again.
const SessionStopped ir.SessionState = "stopped"

// JobState is the state of a job.
type JobState string

const (
	JobQueued    JobState = "queued"  // waits for its session to take the prompt
	JobRunning   JobState = "running" // the agent works on it
	JobDone      JobState = "done"    // the last turn completed; Result holds the answer
	JobFailed    JobState = "failed"
	JobCancelled JobState = "cancelled"
)

// Terminal reports whether no more work happens without a new input.
func (s JobState) Terminal() bool { return s == JobDone || s == JobFailed || s == JobCancelled }

// Job is one piece of work for one agent. It runs in its own session. The
// caller gets the job ID at once and does not wait: when the turn of the
// agent completes, the job is done and the result goes back to whoever
// asked (the Super Agent inbox, or the person).
type Job struct {
	ID          ID
	OwnerID     ID
	AgentID     ID
	SessionID   ID
	RequestedBy AuthorKind // human or super
	Title       string
	Prompt      string
	State       JobState
	Result      string // the last agent message of the last turn
	Error       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Input is one entry in the inbox of a session. The delivery loop sends
// pending inputs in order. The ID is the ir.Input ref, so a redelivery
// after a lost ack is dropped by the daemon.
type Input struct {
	ID        ID
	SessionID ID
	Text      string
	Sent      bool
	CreatedAt time.Time
}
