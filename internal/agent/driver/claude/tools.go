package claude

import "github.com/austiecodes/solax/pkg/ir"

// toolClass maps Claude Code tool names to IR tool classes. Names that
// start with "mcp__" map to ir.ToolMCP. Other unknown names map to
// ir.ToolOther.
var toolClass = map[string]ir.ToolClass{
	"Bash":         ir.ToolShell,
	"Read":         ir.ToolFileRead,
	"Grep":         ir.ToolFileSearch,
	"Glob":         ir.ToolFileSearch,
	"Edit":         ir.ToolFileChange,
	"Write":        ir.ToolFileChange,
	"NotebookEdit": ir.ToolFileChange,
	"WebFetch":     ir.ToolWeb,
	"WebSearch":    ir.ToolWeb,
	"Agent":        ir.ToolSubagent,
	"Task":         ir.ToolSubagent,
}
