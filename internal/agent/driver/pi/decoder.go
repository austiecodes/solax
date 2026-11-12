package pi

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/pkg/ir"
)

// Decoder maps pi RPC records to IR events. See pi docs/json.md.
//
//	agent_start (no open turn)       → turn.started
//	message_update text/thinking     → item.started / item.delta
//	message_end (assistant)          → item.completed (text, thinking), usage.updated
//	tool_execution_start/update/end  → item.* (tool_call)
//	compaction_start/end             → item.* (compaction)
//	auto_retry_start/end             → notice
//	agent_settled                    → turn.completed (not agent_end)
//	turn_start/turn_end, agent_end   → dropped (steps inside an IR turn)
//	response, extension_ui_request   → handled by the session
//
// Item IDs of text and thinking blocks are "<turn>:m<message>:<index>".
// The decoder is not safe for concurrent use.
type Decoder struct {
	turns   int
	turn    string
	pending int
	msgs    int    // assistant messages in the turn
	lastErr string // error of the last assistant message
	usage   ir.Usage
	partial map[string]string          // tool call ID → output seen so far
	args    map[string]json.RawMessage // tool call ID → arguments, until the end event
	compact int
}

// NewDecoder returns a decoder for one session.
func NewDecoder() *Decoder {
	return &Decoder{partial: map[string]string{}, args: map[string]json.RawMessage{}}
}

// NoteInput tells the decoder that a prompt was sent.
func (d *Decoder) NoteInput() { d.pending++ }

// Busy reports whether a turn is open.
func (d *Decoder) Busy() bool { return d.turn != "" }

// TurnID returns the open turn, or "".
func (d *Decoder) TurnID() string { return d.turn }

type record struct {
	Type    string   `json:"type"`
	Message *message `json:"message"`
	Event   *struct {
		Type         string `json:"type"`
		ContentIndex int    `json:"contentIndex"`
		Delta        string `json:"delta"`
	} `json:"assistantMessageEvent"`
	ToolCallID    string          `json:"toolCallId"`
	ToolName      string          `json:"toolName"`
	Args          json.RawMessage `json:"args"`
	PartialResult *toolResult     `json:"partialResult"`
	Result        *toolResult     `json:"result"`
	IsError       bool            `json:"isError"`
	Aborted       bool            `json:"aborted"`
	Attempt       int             `json:"attempt"`
	MaxAttempts   int             `json:"maxAttempts"`
	ErrorMessage  string          `json:"errorMessage"`
	Success       bool            `json:"success"`
	FinalError    string          `json:"finalError"`
	Error         string          `json:"error"`
}

type message struct {
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content"`
	StopReason   string          `json:"stopReason"`
	ErrorMessage string          `json:"errorMessage"`
	Usage        *struct {
		Input      int64 `json:"input"`
		Output     int64 `json:"output"`
		CacheRead  int64 `json:"cacheRead"`
		CacheWrite int64 `json:"cacheWrite"`
		Cost       struct {
			Total float64 `json:"total"`
		} `json:"cost"`
	} `json:"usage"`
}

type toolResult struct {
	Content           json.RawMessage `json:"content"`
	StructuredContent *struct {
		ExitCode *int `json:"exit_code"`
	} `json:"structuredContent"`
}

// Decode maps one record.
func (d *Decoder) Decode(rec []byte) ([]ir.Event, error) {
	var r record
	if err := json.Unmarshal(rec, &r); err != nil {
		return nil, fmt.Errorf("pi: decode: %w", err)
	}
	switch r.Type {
	case "agent_start":
		return d.open(), nil
	case "message_start":
		if r.Message != nil && r.Message.Role == "assistant" {
			d.msgs++
		}
	case "message_update":
		return d.update(r), nil
	case "message_end":
		return d.messageEnd(r), nil
	case "tool_execution_start":
		out := d.open()
		d.args[r.ToolCallID] = r.Args
		return append(out, agent.ItemStarted(d.turn, ir.Item{
			ID: r.ToolCallID, Kind: ir.ItemToolCall, Native: r.ToolName,
			Tool: &ir.ToolCall{Name: r.ToolName, Class: agent.ClassOf(toolClass, r.ToolName), Input: r.Args},
		})), nil
	case "tool_execution_update":
		return d.toolUpdate(r), nil
	case "tool_execution_end":
		return d.toolEnd(r), nil
	case "compaction_start":
		out := d.open()
		d.compact++
		return append(out, agent.ItemStarted(d.turn, ir.Item{ID: d.compactID(), Kind: ir.ItemCompaction, Native: "compaction"})), nil
	case "compaction_end":
		it := ir.Item{ID: d.compactID(), Kind: ir.ItemCompaction, Native: "compaction"}
		switch {
		case r.Aborted:
			it.Status = ir.StatusInterrupted
		case r.ErrorMessage != "":
			it.Status, it.Text = ir.StatusFailed, r.ErrorMessage
		}
		return []ir.Event{agent.ItemCompleted(d.turn, it)}, nil
	case "auto_retry_start":
		return []ir.Event{agent.Notice(d.turn, "warn", "retry",
			fmt.Sprintf("retry %d/%d: %s", r.Attempt, r.MaxAttempts, r.ErrorMessage))}, nil
	case "auto_retry_end":
		if !r.Success {
			return []ir.Event{agent.Notice(d.turn, "error", "retry_failed", r.FinalError)}, nil
		}
	case "extension_error":
		return []ir.Event{agent.Notice(d.turn, "error", "extension_error", r.Error)}, nil
	case "agent_settled":
		return d.settled(r), nil
	}
	return nil, nil
}

func (d *Decoder) open() []ir.Event {
	if d.turn != "" {
		return nil
	}
	d.turns++
	d.turn = fmt.Sprintf("turn-%d", d.turns)
	d.msgs, d.lastErr, d.usage = 0, "", ir.Usage{}
	cause := ir.CauseAuto
	if d.pending > 0 {
		cause = ir.CauseInput
	}
	return []ir.Event{agent.TurnStarted(d.turn, cause)}
}

func (d *Decoder) blockID(idx int) string { return fmt.Sprintf("%s:m%d:%d", d.turn, d.msgs, idx) }
func (d *Decoder) compactID() string      { return fmt.Sprintf("%s:compact-%d", d.turn, d.compact) }

func (d *Decoder) update(r record) []ir.Event {
	if r.Event == nil {
		return nil
	}
	out := d.open()
	id := d.blockID(r.Event.ContentIndex)
	switch r.Event.Type {
	case "text_start":
		out = append(out, agent.ItemStarted(d.turn, ir.Item{ID: id, Kind: ir.ItemAgentMessage, Native: "text"}))
	case "thinking_start":
		out = append(out, agent.ItemStarted(d.turn, ir.Item{ID: id, Kind: ir.ItemReasoning, Native: "thinking"}))
	case "text_delta":
		out = append(out, agent.ItemDelta(d.turn, id, "text", r.Event.Delta))
	case "thinking_delta":
		out = append(out, agent.ItemDelta(d.turn, id, "reasoning", r.Event.Delta))
	}
	return out
}

func (d *Decoder) messageEnd(r record) []ir.Event {
	m := r.Message
	if m == nil || m.Role != "assistant" {
		return nil
	}
	out := d.open()
	var blocks []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Thinking string `json:"thinking"`
	}
	_ = json.Unmarshal(m.Content, &blocks)
	for i, b := range blocks {
		switch b.Type {
		case "text":
			out = append(out, agent.ItemCompleted(d.turn, ir.Item{ID: d.blockID(i), Kind: ir.ItemAgentMessage, Text: b.Text, Native: "text"}))
		case "thinking":
			out = append(out, agent.ItemCompleted(d.turn, ir.Item{ID: d.blockID(i), Kind: ir.ItemReasoning, Text: b.Thinking, Native: "thinking"}))
		}
	}
	d.lastErr = ""
	if m.StopReason == "error" {
		d.lastErr = m.ErrorMessage
		if d.lastErr == "" {
			d.lastErr = "provider error"
		}
	}
	if u := m.Usage; u != nil && (u.Input|u.Output|u.CacheRead|u.CacheWrite) != 0 {
		d.usage.Input += u.Input
		d.usage.Output += u.Output
		d.usage.CacheRead += u.CacheRead
		d.usage.CacheWrite += u.CacheWrite
		d.usage.CostUSD += u.Cost.Total
		out = append(out, agent.UsageUpdated(d.turn, d.usage))
	}
	return out
}

// toolUpdate turns the latest partial result into an append-only delta.
// pi sends the whole output so far, so the delta is the new suffix.
func (d *Decoder) toolUpdate(r record) []ir.Event {
	if r.PartialResult == nil {
		return nil
	}
	text := agent.TextOf(r.PartialResult.Content)
	prev := d.partial[r.ToolCallID]
	d.partial[r.ToolCallID] = text
	delta := text
	if strings.HasPrefix(text, prev) {
		delta = text[len(prev):]
	}
	if delta == "" {
		return nil
	}
	return []ir.Event{agent.ItemDelta(d.turn, r.ToolCallID, "tool_output", delta)}
}

func (d *Decoder) toolEnd(r record) []ir.Event {
	delete(d.partial, r.ToolCallID)
	in := r.Args
	if len(in) == 0 {
		in = d.args[r.ToolCallID]
	}
	delete(d.args, r.ToolCallID)
	tc := &ir.ToolCall{Name: r.ToolName, Class: agent.ClassOf(toolClass, r.ToolName), Input: in}
	if r.Result != nil {
		tc.Output = agent.CapOutput(agent.TextOf(r.Result.Content))
		if r.Result.StructuredContent != nil {
			tc.ExitCode = r.Result.StructuredContent.ExitCode
		}
	}
	it := ir.Item{ID: r.ToolCallID, Kind: ir.ItemToolCall, Native: r.ToolName, Tool: tc}
	if r.IsError {
		it.Status = ir.StatusFailed
	}
	return []ir.Event{agent.ItemCompleted(d.turn, it)}
}

func (d *Decoder) settled(r record) []ir.Event {
	if d.turn == "" {
		return nil
	}
	status, msg := ir.TurnOK, ""
	switch {
	case r.Aborted:
		status = ir.TurnInterrupted
	case d.lastErr != "":
		status, msg = ir.TurnFailed, d.lastErr
	}
	var u *ir.Usage
	if d.usage != (ir.Usage{}) {
		cp := d.usage
		u = &cp
	}
	out := []ir.Event{agent.TurnCompleted(d.turn, status, msg, u)}
	d.turn = ""
	clear(d.partial)
	clear(d.args)
	if d.pending > 0 {
		d.pending--
	}
	return out
}

var _ agent.Decoder = (*Decoder)(nil)
