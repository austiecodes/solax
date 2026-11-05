package claude

import (
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
)

// Decoder maps stream-json records to IR events.
//
//	system/init                       → session.started
//	first output after input          → turn.started (synthesized)
//	output with no input              → turn.started, cause auto
//	stream_event content_block_*      → item.started / item.delta
//	assistant (text, thinking blocks) → item.completed
//	assistant (tool_use block)        → item.started (tool_call)
//	user (tool_result block)          → item.completed (by tool_use_id)
//	system/compact_boundary           → item.* (compaction)
//	system/api_retry                  → notice
//	control_request can_use_tool      → request.opened
//	result                            → turn.completed, usage.updated
//
// Item IDs of assistant blocks are "<message.id>:<block index>".
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
