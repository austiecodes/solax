package agent

import (
	"encoding/json"
	"strings"

	"github.com/austiecodes/solax/pkg/ir"
)

// Constructors for IR events. Drivers use them so that every driver fills
// the same fields for the same event type. Seq, SessionID and At are set
// later by the emitter.

// SessionStarted is the first event of a session.
func SessionStarted(ref ir.SessionRef, model string, caps ir.Caps) ir.Event {
	return ir.Event{Type: ir.SessionStarted, Session: &ir.SessionInfo{Ref: ref, Model: model, Caps: &caps}}
}

// SessionExited is the last event of a session.
func SessionExited(ref ir.SessionRef, code int, reason string) ir.Event {
	return ir.Event{Type: ir.SessionExitedEvent, Session: &ir.SessionInfo{Ref: ref, ExitCode: &code, Reason: reason}}
}

// TurnStarted opens a turn.
func TurnStarted(turnID string, cause ir.TurnCause) ir.Event {
	return ir.Event{Type: ir.TurnStarted, TurnID: turnID, Turn: &ir.Turn{Cause: cause}}
}

// TurnCompleted closes a turn. errMsg is set for a failed turn.
func TurnCompleted(turnID string, status ir.TurnStatus, errMsg string, usage *ir.Usage) ir.Event {
	t := &ir.Turn{Status: status, Usage: usage}
	if errMsg != "" {
		t.Error = &ir.Error{Message: errMsg}
	}
	return ir.Event{Type: ir.TurnCompleted, TurnID: turnID, Turn: t}
}

// ItemStarted announces an item.
func ItemStarted(turnID string, it ir.Item) ir.Event {
	if it.Status == "" {
		it.Status = ir.StatusInProgress
	}
	return ir.Event{Type: ir.ItemStarted, TurnID: turnID, Item: &it}
}

// ItemCompleted is the final state of an item. It is the truth.
func ItemCompleted(turnID string, it ir.Item) ir.Event {
	if it.Status == "" {
		it.Status = ir.StatusCompleted
	}
	return ir.Event{Type: ir.ItemCompleted, TurnID: turnID, Item: &it}
}

// ItemDelta is a streamed part of an item. kind is text, reasoning or
// tool_output.
func ItemDelta(turnID, itemID, kind, text string) ir.Event {
	return ir.Event{Type: ir.ItemDelta, TurnID: turnID, Delta: &ir.Delta{ItemID: itemID, Kind: kind, Text: text}}
}

// UsageUpdated reports token usage.
func UsageUpdated(turnID string, u ir.Usage) ir.Event {
	return ir.Event{Type: ir.UsageUpdated, TurnID: turnID, Usage: &u}
}

// Notice reports a retry, a warning or an error.
func Notice(turnID, level, code, msg string) ir.Event {
	return ir.Event{Type: ir.NoticeEmitted, TurnID: turnID, Notice: &ir.Notice{Level: level, Code: code, Message: msg}}
}

// UserMessage is the input that the daemon sent to the agent. Sessions
// emit it themselves, so the stream looks the same for every driver: no
// driver depends on the agent to echo the input.
func UserMessage(id, text string) ir.Event {
	return ItemCompleted("", ir.Item{ID: id, Kind: ir.ItemUserMessage, Text: text})
}

// MaxToolOutput caps ToolCall.Output. The full text stays on the machine.
const MaxToolOutput = 16 << 10

// CapOutput shortens tool output to MaxToolOutput bytes on a rune boundary.
func CapOutput(s string) string {
	if len(s) <= MaxToolOutput {
		return s
	}
	cut := MaxToolOutput
	for cut > 0 && !utf8RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n…[truncated]"
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

// ClassOf returns the tool class for a native tool name. Names that start
// with "mcp__" are MCP tools; unknown names are ToolOther.
func ClassOf(table map[string]ir.ToolClass, name string) ir.ToolClass {
	if c, ok := table[name]; ok {
		return c
	}
	if strings.HasPrefix(name, "mcp__") {
		return ir.ToolMCP
	}
	return ir.ToolOther
}

// TextOf joins the text parts of a native content value. It accepts a
// plain string or an array of {"type":"text","text":...} blocks.
func TextOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b.Type == "text" || b.Type == "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}
