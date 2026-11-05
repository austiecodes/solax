package codex

import (
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
)

// Decoder maps app-server notifications and server requests to IR events.
//
//	thread/started                     → session.started
//	turn/started, turn/completed       → turn.*
//	item/started, item/completed       → item.* (by ThreadItem type)
//	item/agentMessage/delta            → item.delta (text)
//	item/reasoning/*Delta              → item.delta (reasoning)
//	item/commandExecution/outputDelta  → item.delta (tool_output)
//	item/*/requestApproval (request)   → request.opened
//	thread/tokenUsage/updated          → usage.updated
//	error, model/rerouted              → notice
type Decoder struct {
	sessionID string
	seq       uint64
}

// NewDecoder returns a decoder for one session.
func NewDecoder(sessionID string) *Decoder { return &Decoder{sessionID: sessionID} }

func (d *Decoder) Decode(record []byte) ([]ir.Event, error) {
	return nil, errs.ErrNotImplemented
}
