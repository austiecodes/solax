package claude

import (
	"encoding/json"
	"fmt"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/pkg/ir"
)

// Decoder maps stream-json records to IR events.
//
//	system/init                       → notice when the permission mode is not Mode
//	                                    (else dropped: the session emits session.started)
//	first output after input          → turn.started (synthesized)
//	output with no input              → turn.started, cause auto
//	stream_event content_block_start  → item.started (text, thinking)
//	stream_event content_block_delta  → item.delta (text, reasoning)
//	assistant (text, thinking blocks) → item.completed
//	assistant (tool_use block)        → item.started (tool_call)
//	user (tool_result block)          → item.completed (by tool_use_id)
//	user (isReplay)                   → item.completed (user_message), when
//	                                    the agent reads the input
//	system/compact_boundary           → item.completed (compaction)
//	system/api_retry                  → notice
//	system/permission_denied          → dropped (the tool_result carries the denial)
//	result                            → usage.updated, turn.completed
//
// Item IDs of text and thinking blocks are "<message.id>:<block index>".
// Tool items use the tool_use ID.
//
// With --replay-user-messages, Claude Code echoes each input when the agent
// reads it: before the first output of an idle session, or between two
// model calls of a running turn (a steer). The user message item comes at
// that point, so the transcript shows where the agent took the input.
//
// The decoder is not safe for concurrent use. The session calls NoteInput
// and Interrupted under its own lock.
type Decoder struct {
	// Mode is the permission mode the session asked for. When system/init
	// reports another one, the decoder gives one warning: settings, the
	// account or the model made Claude Code fall back. "" skips the check.
	Mode string

	modeSeen    bool
	turns       int
	turn        string  // open turn, "" when idle
	inputs      []input // inputs written and not yet read, oldest first
	read        bool    // an input was read while no turn was open
	interrupted bool

	block map[string]int          // message ID → index of the open block
	seen  map[string]int          // message ID → blocks completed, when no stream events
	tools map[string]*ir.ToolCall // tool_use ID → call, until its result
	paren map[string]string       // tool_use ID → parent tool_use ID
}

// NewDecoder returns a decoder for one session.
func NewDecoder() *Decoder {
	return &Decoder{block: map[string]int{}, seen: map[string]int{}, tools: map[string]*ir.ToolCall{}, paren: map[string]string{}}
}

// input is an input written to Claude Code: the ID and the text of its
// user message item.
type input struct{ id, text string }

// NoteInput tells the decoder that an input was written. Its replay gives
// the user message item, and opens the next turn with cause "input".
func (d *Decoder) NoteInput(id, text string) { d.inputs = append(d.inputs, input{id, text}) }

// Interrupted marks the open turn as interrupted by the user.
func (d *Decoder) Interrupted() { d.interrupted = true }

// Busy reports whether a turn is open.
func (d *Decoder) Busy() bool { return d.turn != "" }

// TurnID returns the open turn, or "" when idle.
func (d *Decoder) TurnID() string { return d.turn }

type record struct {
	Type            string          `json:"type"`
	Subtype         string          `json:"subtype"`
	SessionID       string          `json:"session_id"`
	ParentToolUseID string          `json:"parent_tool_use_id"`
	Event           *streamEvent    `json:"event"`
	Message         json.RawMessage `json:"message"` // object in assistant and user, string in system/permission_denied
	StopReason      string          `json:"stop_reason"`
	Result          string          `json:"result"`
	IsError         bool            `json:"is_error"`
	Usage           *usage          `json:"usage"`
	TotalCostUSD    float64         `json:"total_cost_usd"`
	Errors          []string        `json:"errors"`
	Attempt         int             `json:"attempt"`
	MaxRetries      int             `json:"max_retries"`
	Error           json.RawMessage `json:"error"`
	APIMessageID    string          `json:"api_message_id"`
	PermissionMode  string          `json:"permissionMode"`
	IsReplay        bool            `json:"isReplay"`
}

type streamEvent struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	Message      *message
	ContentBlock *struct {
		Type string `json:"type"`
	} `json:"content_block"`
	Delta *struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Thinking string `json:"thinking"`
	} `json:"delta"`
}

type message struct {
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type block struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

type usage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// Decode maps one record. Unknown records give no events.
func (d *Decoder) Decode(rec []byte) ([]ir.Event, error) {
	var r record
	if err := json.Unmarshal(rec, &r); err != nil {
		return nil, fmt.Errorf("claude: decode: %w", err)
	}
	switch r.Type {
	case "stream_event":
		return d.stream(r), nil
	case "assistant":
		return d.assistant(r), nil
	case "user":
		return d.user(r), nil
	case "result":
		return d.result(r), nil
	case "system":
		return d.system(r), nil
	}
	return nil, nil
}

// open returns the turn.started event when no turn is open.
func (d *Decoder) open() []ir.Event {
	if d.turn != "" {
		return nil
	}
	d.turns++
	d.turn = fmt.Sprintf("turn-%d", d.turns)
	cause := ir.CauseAuto
	if d.read {
		cause = ir.CauseInput
	}
	d.read = false
	return []ir.Event{agent.TurnStarted(d.turn, cause)}
}

func (d *Decoder) stream(r record) []ir.Event {
	e := r.Event
	if e == nil {
		return nil
	}
	out := d.open()
	msgID := r.APIMessageID
	switch e.Type {
	case "content_block_start":
		if e.ContentBlock == nil {
			return out
		}
		d.block[msgID] = e.Index
		id := fmt.Sprintf("%s:%d", msgID, e.Index)
		switch e.ContentBlock.Type {
		case "text":
			out = append(out, agent.ItemStarted(d.turn, ir.Item{ID: id, ParentID: r.ParentToolUseID, Kind: ir.ItemAgentMessage, Native: "text"}))
		case "thinking":
			out = append(out, agent.ItemStarted(d.turn, ir.Item{ID: id, ParentID: r.ParentToolUseID, Kind: ir.ItemReasoning, Native: "thinking"}))
		}
	case "content_block_delta":
		if e.Delta == nil {
			return out
		}
		id := fmt.Sprintf("%s:%d", msgID, e.Index)
		switch e.Delta.Type {
		case "text_delta":
			out = append(out, agent.ItemDelta(d.turn, id, "text", e.Delta.Text))
		case "thinking_delta":
			if e.Delta.Thinking != "" {
				out = append(out, agent.ItemDelta(d.turn, id, "reasoning", e.Delta.Thinking))
			}
		}
	}
	return out
}

// assistant handles a complete assistant message. With partial messages,
// Claude Code sends one record per content block, before its
// content_block_stop, so the open block index names the item.
func (d *Decoder) assistant(r record) []ir.Event {
	msgID, blocks, ok := content(r)
	if !ok {
		return nil
	}
	out := d.open()
	for i, b := range blocks {
		idx, ok := d.block[msgID]
		if !ok || len(blocks) > 1 {
			idx = d.seen[msgID] + i
		}
		id := fmt.Sprintf("%s:%d", msgID, idx)
		switch b.Type {
		case "text":
			out = append(out, agent.ItemCompleted(d.turn, ir.Item{ID: id, ParentID: r.ParentToolUseID, Kind: ir.ItemAgentMessage, Text: b.Text, Native: "text"}))
		case "thinking", "redacted_thinking":
			out = append(out, agent.ItemCompleted(d.turn, ir.Item{ID: id, ParentID: r.ParentToolUseID, Kind: ir.ItemReasoning, Text: b.Thinking, Native: b.Type}))
		case "tool_use":
			call := &ir.ToolCall{Name: b.Name, Class: agent.ClassOf(toolClass, b.Name), Input: b.Input}
			d.tools[b.ID] = call
			d.paren[b.ID] = r.ParentToolUseID
			out = append(out, agent.ItemStarted(d.turn, ir.Item{ID: b.ID, ParentID: r.ParentToolUseID, Kind: ir.ItemToolCall, Tool: call, Native: "tool_use"}))
		}
	}
	if !d.hasStream(msgID) {
		d.seen[msgID] += len(blocks)
	}
	return out
}

// content decodes the message of an assistant or user record. Echoed user
// text has a string content, which gives ok false.
func content(r record) (id string, blocks []block, ok bool) {
	var m message
	if json.Unmarshal(r.Message, &m) != nil || json.Unmarshal(m.Content, &blocks) != nil {
		return "", nil, false
	}
	return m.ID, blocks, true
}

func (d *Decoder) hasStream(msgID string) bool {
	_, ok := d.block[msgID]
	return ok
}

// user handles tool results, and the replay of an input.
func (d *Decoder) user(r record) []ir.Event {
	if r.IsReplay {
		return d.replay()
	}
	_, blocks, ok := content(r)
	if !ok {
		return nil
	}
	var out []ir.Event
	for _, b := range blocks {
		if b.Type != "tool_result" {
			continue
		}
		call, ok := d.tools[b.ToolUseID]
		if !ok {
			call = &ir.ToolCall{Class: ir.ToolOther}
		}
		delete(d.tools, b.ToolUseID)
		res := *call
		res.Output = agent.CapOutput(agent.TextOf(b.Content))
		status := ir.StatusCompleted
		if b.IsError {
			status = ir.StatusFailed
		}
		parent := d.paren[b.ToolUseID]
		delete(d.paren, b.ToolUseID)
		out = append(out, agent.ItemCompleted(d.turn, ir.Item{ID: b.ToolUseID, ParentID: parent, Kind: ir.ItemToolCall, Status: status, Tool: &res, Native: "tool_result"}))
	}
	return out
}

func (d *Decoder) result(r record) []ir.Event {
	out := d.open()
	var u *ir.Usage
	if r.Usage != nil {
		u = &ir.Usage{
			Input: r.Usage.InputTokens, Output: r.Usage.OutputTokens,
			CacheRead: r.Usage.CacheReadInputTokens, CacheWrite: r.Usage.CacheCreationInputTokens,
			CostUSD: r.TotalCostUSD,
		}
		out = append(out, agent.UsageUpdated(d.turn, *u))
	}
	status, msg := ir.TurnOK, ""
	switch {
	case d.interrupted:
		status = ir.TurnInterrupted
	case r.IsError || r.Subtype != "success":
		status = ir.TurnFailed
		msg = r.Result
		if msg == "" && len(r.Errors) > 0 {
			msg = r.Errors[0]
		}
		if msg == "" {
			msg = r.Subtype
		}
	}
	// Tools without a result die with the turn.
	for id, call := range d.tools {
		out = append(out, agent.ItemCompleted(d.turn, ir.Item{ID: id, ParentID: d.paren[id], Kind: ir.ItemToolCall, Status: ir.StatusInterrupted, Tool: call}))
	}
	clear(d.tools)
	clear(d.paren)
	if status == ir.TurnOK && r.StopReason != "tool_use" {
		msg = r.Result
	}
	out = append(out, agent.TurnCompleted(d.turn, status, msg, u))
	d.turn, d.interrupted = "", false
	clear(d.block)
	clear(d.seen)
	return out
}

// replay gives the user message item of the oldest input not yet read.
// Claude Code reads the inputs in the order they were written.
func (d *Decoder) replay() []ir.Event {
	if len(d.inputs) == 0 {
		return nil
	}
	in := d.inputs[0]
	d.inputs = d.inputs[1:]
	if d.turn == "" {
		d.read = true
	}
	return []ir.Event{agent.ItemCompleted(d.turn, ir.Item{ID: in.id, Kind: ir.ItemUserMessage, Text: in.text})}
}

func (d *Decoder) system(r record) []ir.Event {
	switch r.Subtype {
	case "init":
		if d.Mode == "" || d.modeSeen || r.PermissionMode == "" {
			return nil
		}
		d.modeSeen = true
		if r.PermissionMode == d.Mode {
			return nil
		}
		return []ir.Event{agent.Notice(d.turn, "warn", "permission_mode", fmt.Sprintf(
			"Claude Code runs in %s mode, not %s. Its settings, account or model can cause this. Solax approves each permission request.",
			r.PermissionMode, d.Mode))}
	case "compact_boundary":
		out := d.open()
		return append(out, agent.ItemCompleted(d.turn, ir.Item{ID: fmt.Sprintf("compact-%d", d.turns), Kind: ir.ItemCompaction, Native: "compact_boundary"}))
	case "api_retry":
		return []ir.Event{agent.Notice(d.turn, "warn", "retry", fmt.Sprintf("API retry %d/%d", r.Attempt, r.MaxRetries))}
	}
	return nil
}
