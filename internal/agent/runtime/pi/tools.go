package pi

import "github.com/austiecodes/solax/pkg/ir"

// toolClass maps pi tool names to IR tool classes. Names that start with
// "mcp__" map to ir.ToolMCP. Other unknown names map to ir.ToolOther.
var toolClass = map[string]ir.ToolClass{
	"bash":     ir.ToolShell,
	"read":     ir.ToolFileRead,
	"ls":       ir.ToolFileRead,
	"grep":     ir.ToolFileSearch,
	"find":     ir.ToolFileSearch,
	"edit":     ir.ToolFileChange,
	"write":    ir.ToolFileChange,
	"codemode": ir.ToolScript,
}
