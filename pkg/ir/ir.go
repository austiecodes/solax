// Package ir defines the intermediate representation of coding agent
// activity. The daemon translates the native stream of each agent (pi,
// Codex, Claude Code) into these types. The server and the client use only
// these types.
//
// Rules:
//   - Completed events are the truth. Deltas are only a preview.
//   - TurnCompleted means that the agent will not continue without new input.
//   - The IR is not lossless. Raw carries the native record for debugging.
package ir

import (
	"encoding/json"
	"time"
)

// Kind identifies a coding agent driver.
type Kind string

const (
	KindPi     Kind = "pi"
	KindCodex  Kind = "codex"
	KindClaude Kind = "claude"
)

// SessionRef is the stable native reference of a session. The daemon uses
// it to resume the session after a restart.
type SessionRef struct {
	Kind     Kind   `json:"kind"`
	NativeID string `json:"native_id"`      // pi session file, codex thread ID, claude session_id
	Path     string `json:"path,omitempty"` // transcript path on the machine
}

// SessionState is derived only from IR events. It does not depend on a driver.
type SessionState string

const (
	SessionStarting SessionState = "starting"
	SessionIdle     SessionState = "idle"
	SessionBusy     SessionState = "busy"
	SessionWaiting  SessionState = "waiting" // a request is open
	SessionExited   SessionState = "exited"
)

// EventType is the type of an IR event.
type EventType string

const (
	SessionStarted EventType = "session.started"
	// SessionExitedEvent is the event type. The state ir.SessionExited has
	// the same name in the docs; one Go package cannot hold both names.
	SessionExitedEvent EventType = "session.exited"
	TurnStarted        EventType = "turn.started"
	TurnCompleted      EventType = "turn.completed"
	ItemStarted        EventType = "item.started"
	ItemDelta          EventType = "item.delta"
	ItemCompleted      EventType = "item.completed"
	RequestOpened      EventType = "request.opened"
	RequestResolved    EventType = "request.resolved"
	UsageUpdated       EventType = "usage.updated"
	NoticeEmitted      EventType = "notice"
)

// Reliable reports whether events of this type go on the reliable channel.
// Deltas go on the live channel and can be merged or dropped.
func (t EventType) Reliable() bool { return t != ItemDelta }

// Event is one IR event. Exactly one payload field is set, as the type
// requires.
type Event struct {
	Seq       uint64          `json:"seq"`        // per session, monotonic, set by the daemon
	SessionID string          `json:"session_id"` // Solax ses_ ID
	TurnID    string          `json:"turn_id,omitempty"`
	At        time.Time       `json:"at"`
	Type      EventType       `json:"type"`
	Session   *SessionInfo    `json:"session,omitempty"`
	Item      *Item           `json:"item,omitempty"`
	Delta     *Delta          `json:"delta,omitempty"`
	Turn      *Turn           `json:"turn,omitempty"`
	Request   *Request        `json:"request,omitempty"`
	Usage     *Usage          `json:"usage,omitempty"`
	Notice    *Notice         `json:"notice,omitempty"`
	Raw       json.RawMessage `json:"raw,omitempty"` // native record, debug only
}

// SessionInfo is the payload of SessionStarted and SessionExitedEvent.
type SessionInfo struct {
	Ref      SessionRef `json:"ref"`
	Model    string     `json:"model,omitempty"`
	Tools    []string   `json:"tools,omitempty"`
	Caps     *Caps      `json:"caps,omitempty"`
	ExitCode *int       `json:"exit_code,omitempty"`
	Reason   string     `json:"reason,omitempty"`
}

// ItemKind is the kind of an item in a turn.
type ItemKind string

const (
	ItemUserMessage  ItemKind = "user_message"
	ItemAgentMessage ItemKind = "agent_message"
	ItemReasoning    ItemKind = "reasoning"
	ItemToolCall     ItemKind = "tool_call"
	ItemPlan         ItemKind = "plan"
	ItemCompaction   ItemKind = "compaction"
)

// ItemStatus is the status of an item.
type ItemStatus string

const (
	StatusInProgress  ItemStatus = "in_progress"
	StatusCompleted   ItemStatus = "completed"
	StatusFailed      ItemStatus = "failed"
	StatusDeclined    ItemStatus = "declined"
	StatusInterrupted ItemStatus = "interrupted"
)

// Item is one unit of work in a turn.
type Item struct {
	ID       string     `json:"id"`
	ParentID string     `json:"parent_id,omitempty"` // tool_call item that started a nested run
	Kind     ItemKind   `json:"kind"`
	Status   ItemStatus `json:"status"`
	Text     string     `json:"text,omitempty"`
	Tool     *ToolCall  `json:"tool,omitempty"`
	Native   string     `json:"native,omitempty"` // native type name
}

// ToolClass is the normalized class of a tool call.
type ToolClass string

const (
	ToolShell      ToolClass = "shell"
	ToolFileRead   ToolClass = "file_read"
	ToolFileSearch ToolClass = "file_search"
	ToolFileChange ToolClass = "file_change"
	ToolMCP        ToolClass = "mcp"
	ToolWeb        ToolClass = "web"
	ToolSubagent   ToolClass = "subagent"
	ToolScript     ToolClass = "script"
	ToolOther      ToolClass = "other"
)

// ToolCall describes one tool call.
type ToolCall struct {
	Name      string          `json:"name"` // native name
	Class     ToolClass       `json:"class"`
	Input     json.RawMessage `json:"input,omitempty"`
	Output    string          `json:"output,omitempty"`     // capped; the full text is spilled
	OutputRef string          `json:"output_ref,omitempty"` // location of the full output on the machine
	ExitCode  *int            `json:"exit_code,omitempty"`
	Files     []FileChange    `json:"files,omitempty"`
}

// FileChange is one changed file of a file_change tool call.
type FileChange struct {
	Path string `json:"path"`
	Op   string `json:"op"` // add | modify | delete | rename
	Diff string `json:"diff,omitempty"`
}

// Delta is a streamed part of an item. It is best-effort.
type Delta struct {
	ItemID string `json:"item_id"`
	Kind   string `json:"kind"` // text | reasoning | tool_output
	Text   string `json:"text"`
}

// TurnCause tells what started a turn.
type TurnCause string

const (
	CauseInput TurnCause = "input"
	CauseAuto  TurnCause = "auto" // background work started it
)

// TurnStatus is the final status of a turn.
type TurnStatus string

const (
	TurnOK          TurnStatus = "completed"
	TurnInterrupted TurnStatus = "interrupted"
	TurnFailed      TurnStatus = "failed"
)

// Turn is the payload of TurnStarted and TurnCompleted.
type Turn struct {
	Cause  TurnCause  `json:"cause,omitempty"`
	Status TurnStatus `json:"status,omitempty"`
	Error  *Error     `json:"error,omitempty"`
	Usage  *Usage     `json:"usage,omitempty"`
}

// RequestKind is the kind of a request from an agent.
type RequestKind string

const (
	RequestApproval RequestKind = "approval"
	RequestQuestion RequestKind = "question"
)

// Request is an approval or a question that waits for a decision.
type Request struct {
	ID      string      `json:"id"`
	ItemID  string      `json:"item_id,omitempty"`
	Kind    RequestKind `json:"kind"`
	Tool    *ToolCall   `json:"tool,omitempty"`
	Prompt  string      `json:"prompt,omitempty"`
	Options []string    `json:"options,omitempty"`
}

// Decision answers a Request.
type Decision struct {
	Choice string `json:"choice"` // one of Request.Options, or "allow" / "deny"
	Reason string `json:"reason,omitempty"`
}

// Usage is token and cost usage.
type Usage struct {
	Input      int64   `json:"input"`
	Output     int64   `json:"output"`
	CacheRead  int64   `json:"cache_read,omitempty"`
	CacheWrite int64   `json:"cache_write,omitempty"`
	CostUSD    float64 `json:"cost_usd,omitempty"`
}

// Notice is an informational event: retry, compaction, warning or error.
type Notice struct {
	Level   string `json:"level"` // info | warn | error
	Code    string `json:"code"`  // for example "retry", "compaction"
	Message string `json:"message"`
}

// Error describes a failure.
type Error struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}
