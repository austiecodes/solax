package codex

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/pkg/ir"
)

// Decoder maps app-server notifications to IR events.
//
//	turn/started, turn/completed       → turn.*
//	item/started, item/completed       → item.* (by ThreadItem type)
//	item/agentMessage/delta            → item.delta (text)
//	item/reasoning/*Delta              → item.delta (reasoning)
//	item/commandExecution/outputDelta  → item.delta (tool_output)
//	thread/tokenUsage/updated          → usage.updated
//	error                              → notice
//
// thread/started is dropped: the session emits session.started from the
// thread/start result. userMessage items are dropped: the session emits the
// user message itself. Server requests (approvals) are handled by the
// session, not here.
type Decoder struct {
	usage *ir.Usage // last total, attached to turn.completed
}

// NewDecoder returns a decoder for one session.
func NewDecoder() *Decoder { return &Decoder{} }

type envelope struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// Decode maps one raw record (a notification). Responses and requests give
// no events.
func (d *Decoder) Decode(rec []byte) ([]ir.Event, error) {
	var e envelope
	if err := json.Unmarshal(rec, &e); err != nil {
		return nil, fmt.Errorf("codex: decode: %w", err)
	}
	if e.Method == "" {
		return nil, nil
	}
	return d.Notification(e.Method, e.Params)
}

type params struct {
	TurnID     string          `json:"turnId"`
	ItemID     string          `json:"itemId"`
	Delta      string          `json:"delta"`
	Turn       *turn           `json:"turn"`
	Item       json.RawMessage `json:"item"`
	TokenUsage *struct {
		Total tokenCount `json:"total"`
	} `json:"tokenUsage"`
	Error     *turnError `json:"error"`
	WillRetry bool       `json:"willRetry"`
}

type turn struct {
	ID     string     `json:"id"`
	Status string     `json:"status"`
	Error  *turnError `json:"error"`
}

type turnError struct {
	Message string `json:"message"`
}

type tokenCount struct {
	InputTokens       int64 `json:"inputTokens"`
	CachedInputTokens int64 `json:"cachedInputTokens"`
	OutputTokens      int64 `json:"outputTokens"`
}

// Notification maps one notification.
func (d *Decoder) Notification(method string, raw json.RawMessage) ([]ir.Event, error) {
	var p params
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("codex: %s: %w", method, err)
		}
	}
	switch method {
	case "turn/started":
		if p.Turn == nil {
			return nil, nil
		}
		d.usage = nil
		return []ir.Event{agent.TurnStarted(p.Turn.ID, ir.CauseInput)}, nil
	case "turn/completed":
		if p.Turn == nil {
			return nil, nil
		}
		status, msg := ir.TurnOK, ""
		switch p.Turn.Status {
		case "interrupted":
			status = ir.TurnInterrupted
		case "failed":
			status = ir.TurnFailed
			msg = "turn failed"
			if p.Turn.Error != nil && p.Turn.Error.Message != "" {
				msg = p.Turn.Error.Message
			}
		}
		return []ir.Event{agent.TurnCompleted(p.Turn.ID, status, msg, d.usage)}, nil
	case "item/started", "item/completed":
		it, ok := mapItem(p.Item)
		if !ok {
			return nil, nil
		}
		if method == "item/started" {
			it.Status = ir.StatusInProgress
			return []ir.Event{agent.ItemStarted(p.TurnID, it)}, nil
		}
		return []ir.Event{agent.ItemCompleted(p.TurnID, it)}, nil
	case "item/agentMessage/delta":
		return []ir.Event{agent.ItemDelta(p.TurnID, p.ItemID, "text", p.Delta)}, nil
	case "item/reasoning/summaryTextDelta", "item/reasoning/textDelta":
		return []ir.Event{agent.ItemDelta(p.TurnID, p.ItemID, "reasoning", p.Delta)}, nil
	case "item/commandExecution/outputDelta":
		return []ir.Event{agent.ItemDelta(p.TurnID, p.ItemID, "tool_output", p.Delta)}, nil
	case "thread/tokenUsage/updated":
		if p.TokenUsage == nil {
			return nil, nil
		}
		t := p.TokenUsage.Total
		u := ir.Usage{Input: t.InputTokens - t.CachedInputTokens, Output: t.OutputTokens, CacheRead: t.CachedInputTokens}
		d.usage = &u
		return []ir.Event{agent.UsageUpdated(p.TurnID, u)}, nil
	case "error":
		if p.Error == nil {
			return nil, nil
		}
		level, code := "error", "error"
		if p.WillRetry {
			level, code = "warn", "retry"
		}
		return []ir.Event{agent.Notice(p.TurnID, level, code, p.Error.Message)}, nil
	}
	return nil, nil
}

// item is the union of the ThreadItem fields that Solax reads.
type item struct {
	Type             string          `json:"type"`
	ID               string          `json:"id"`
	Status           string          `json:"status"`
	Text             string          `json:"text"`
	Summary          []string        `json:"summary"`
	Content          json.RawMessage `json:"content"`
	Command          string          `json:"command"`
	Cwd              string          `json:"cwd"`
	AggregatedOutput *string         `json:"aggregatedOutput"`
	ExitCode         *int            `json:"exitCode"`
	Changes          []struct {
		Path string          `json:"path"`
		Kind json.RawMessage `json:"kind"`
		Diff string          `json:"diff"`
	} `json:"changes"`
	Server    string          `json:"server"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	Result    json.RawMessage `json:"result"`
	Error     json.RawMessage `json:"error"`
	Query     string          `json:"query"`
}

// mapItem maps a ThreadItem to an IR item. ok is false for items that the
// IR does not show (userMessage, review mode markers, unknown types).
func mapItem(raw json.RawMessage) (ir.Item, bool) {
	var it item
	if json.Unmarshal(raw, &it) != nil || it.ID == "" {
		return ir.Item{}, false
	}
	out := ir.Item{ID: it.ID, Native: it.Type, Status: itemStatus(it.Status)}
	switch it.Type {
	case "agentMessage":
		out.Kind, out.Text = ir.ItemAgentMessage, it.Text
	case "reasoning":
		out.Kind = ir.ItemReasoning
		out.Text = strings.Join(it.Summary, "\n")
		if out.Text == "" {
			out.Text = agent.TextOf(it.Content)
		}
	case "plan":
		out.Kind, out.Text = ir.ItemPlan, it.Text
	case "contextCompaction":
		out.Kind = ir.ItemCompaction
	case "commandExecution":
		out.Kind = ir.ItemToolCall
		in, _ := json.Marshal(map[string]string{"command": it.Command, "cwd": it.Cwd})
		tc := &ir.ToolCall{Name: it.Type, Class: ir.ToolShell, Input: in, ExitCode: it.ExitCode}
		if it.AggregatedOutput != nil {
			tc.Output = agent.CapOutput(*it.AggregatedOutput)
		}
		out.Tool = tc
	case "fileChange":
		out.Kind = ir.ItemToolCall
		tc := &ir.ToolCall{Name: it.Type, Class: ir.ToolFileChange}
		for _, c := range it.Changes {
			tc.Files = append(tc.Files, ir.FileChange{Path: c.Path, Op: changeOp(c.Kind), Diff: c.Diff})
		}
		out.Tool = tc
	case "mcpToolCall":
		out.Kind = ir.ItemToolCall
		tc := &ir.ToolCall{Name: "mcp__" + it.Server + "__" + it.Tool, Class: ir.ToolMCP, Input: it.Arguments}
		if len(it.Result) > 0 && string(it.Result) != "null" {
			tc.Output = agent.CapOutput(string(it.Result))
		}
		out.Tool = tc
	case "webSearch":
		out.Kind = ir.ItemToolCall
		in, _ := json.Marshal(map[string]string{"query": it.Query})
		out.Tool = &ir.ToolCall{Name: it.Type, Class: ir.ToolWeb, Input: in}
	default:
		class, ok := itemClass[it.Type]
		if !ok {
			return ir.Item{}, false
		}
		out.Kind = ir.ItemToolCall
		out.Tool = &ir.ToolCall{Name: it.Type, Class: class}
	}
	return out, true
}

func itemStatus(s string) ir.ItemStatus {
	switch s {
	case "failed":
		return ir.StatusFailed
	case "declined":
		return ir.StatusDeclined
	case "inProgress", "in_progress":
		return ir.StatusInProgress
	case "interrupted":
		return ir.StatusInterrupted
	}
	return ""
}

// changeOp reads a fileChange kind. Codex sends a string ("add") or an
// object ({"type":"update","move_path":...}).
func changeOp(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		var o struct {
			Type     string `json:"type"`
			MovePath string `json:"move_path"`
		}
		_ = json.Unmarshal(raw, &o)
		s = o.Type
		if o.MovePath != "" {
			return "rename"
		}
	}
	switch s {
	case "add", "create":
		return "add"
	case "delete", "remove":
		return "delete"
	}
	return "modify"
}

var _ agent.Decoder = (*Decoder)(nil)
