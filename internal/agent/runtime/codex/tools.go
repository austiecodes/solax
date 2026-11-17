package codex

import "github.com/austiecodes/solax/pkg/ir"

// itemClass maps Codex ThreadItem types to IR tool classes.
var itemClass = map[string]ir.ToolClass{
	"commandExecution": ir.ToolShell,
	"fileChange":       ir.ToolFileChange,
	"mcpToolCall":      ir.ToolMCP,
	"webSearch":        ir.ToolWeb,
	"collabToolCall":   ir.ToolSubagent,
	"dynamicToolCall":  ir.ToolOther,
	"imageView":        ir.ToolFileRead,
}
