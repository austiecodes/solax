package core

import (
	"encoding/json"
	"time"
)

// TaskEvent is one entry of the append-only input log of a task
// (task_events). Every orchestrator input is an event: messages, child task
// transitions, timers, action outcomes. The producer appends it in the same
// transaction as the change it records, so the log is the durable source of
// truth for orchestration.
type TaskEvent struct {
	TaskID  ID
	Seq     int64 // strictly increasing per task, from 1
	Kind    TaskEventKind
	Payload json.RawMessage // a JSON object; see the kind
	At      time.Time
}

// TaskEventKind is the kind of a task event. The payloads are specified in
// docs/design/plugin.md §5.4.
type TaskEventKind string

const (
	EventTaskStarted   TaskEventKind = "task.started"
	EventTaskResumed   TaskEventKind = "task.resumed"
	EventTaskCancelled TaskEventKind = "task.cancelled" // audit only; never delivered
	EventMessage       TaskEventKind = "message"
	EventReport        TaskEventKind = "report"
	EventSummary       TaskEventKind = "summary"
	EventAskResolved   TaskEventKind = "ask.resolved"
	EventChildBlocked  TaskEventKind = "child.blocked"
	EventChildResumed  TaskEventKind = "child.resumed"
	EventChildDone     TaskEventKind = "child.done"
	EventSessionState  TaskEventKind = "session.state"
	EventBudget        TaskEventKind = "budget"
	EventTimer         TaskEventKind = "timer"
	EventActionDone    TaskEventKind = "action.done"
)

// Payloads of the events that core writes. The JSON names are the plugin
// contract, so they are camelCase.

// TaskStartedPayload is the payload of task.started.
type TaskStartedPayload struct {
	Brief string `json:"brief"`
	By    Author `json:"by"`
}

// TaskCancelledPayload is the payload of task.cancelled.
type TaskCancelledPayload struct {
	By     Author `json:"by"`
	Reason string `json:"reason,omitempty"`
}

// MessagePayload is the payload of message, report and summary events.
type MessagePayload struct {
	MsgID      ID           `json:"msgId"`
	MsgSeq     int64        `json:"msgSeq"`
	Author     Author       `json:"author"`
	Text       string       `json:"text"`
	Mentions   []ID         `json:"mentions,omitempty"`
	Status     ReportStatus `json:"status,omitempty"`     // report only
	Checkpoint bool         `json:"checkpoint,omitempty"` // summary only
}

// MarshalJSON writes an author as {"kind":..., "id":...}.
func (a Author) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind AuthorKind `json:"kind"`
		ID   ID         `json:"id,omitempty"`
	}{a.Kind, a.ID})
}

// UnmarshalJSON reads an author written by MarshalJSON.
func (a *Author) UnmarshalJSON(b []byte) error {
	var v struct {
		Kind AuthorKind `json:"kind"`
		ID   ID         `json:"id"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	a.Kind, a.ID = v.Kind, v.ID
	return nil
}

// EventForMessage returns the task event kind that records message m, or
// "" when m is not an orchestrator input (system messages are written by
// the orchestrator itself).
func EventForMessage(m Message) TaskEventKind {
	switch m.Kind {
	case MessageChat:
		return EventMessage
	case MessageReport:
		return EventReport
	case MessageSummary:
		return EventSummary
	default:
		return ""
	}
}

// MessageEventPayload builds the payload of the event that records m.
func MessageEventPayload(m Message) MessagePayload {
	p := MessagePayload{MsgID: m.ID, MsgSeq: m.Seq, Author: m.Author, Text: m.Text, Mentions: m.Mentions}
	switch m.Kind {
	case MessageReport:
		p.Status = m.Status
	case MessageSummary:
		p.Checkpoint = m.Checkpoint
	}
	return p
}
