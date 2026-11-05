package pi

import (
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
)

// Decoder maps pi RPC records to IR events.
//
//	agent_start                      → turn.started
//	message_start/update/end         → item.* (agent_message, reasoning)
//	tool_execution_start/update/end  → item.* (tool_call)
//	compaction_start/end             → item.* (compaction)
//	auto_retry_start/end             → notice
//	extension_ui_request             → request.opened
//	agent_settled                    → turn.completed (not agent_end)
//	turn_start/turn_end              → dropped (a step inside an IR turn)
//	response                         → not an event; correlated by ID
type Decoder struct {
	sessionID string
	seq       uint64
	turn      string
}

// NewDecoder returns a decoder for one session.
func NewDecoder(sessionID string) *Decoder { return &Decoder{sessionID: sessionID} }

func (d *Decoder) Decode(record []byte) ([]ir.Event, error) {
	return nil, errs.ErrNotImplemented
}
