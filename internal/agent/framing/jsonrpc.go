package framing

import (
	"context"
	"encoding/json"
	"io"

	"github.com/austiecodes/solax/internal/errs"
)

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return e.Message }

// Handler handles requests and notifications from the peer. Codex
// app-server sends requests to the client (approvals), so the connection is
// bidirectional.
type Handler interface {
	// HandleRequest answers a request from the peer.
	HandleRequest(ctx context.Context, method string, params json.RawMessage) (any, error)
	// HandleNotification handles a notification from the peer.
	HandleNotification(ctx context.Context, method string, params json.RawMessage)
}

// Peer is one JSON-RPC 2.0 connection over JSONL.
type Peer struct {
	r *Reader
	w *Writer
	h Handler
}

// NewPeer returns a Peer. Call Run to start reading.
func NewPeer(r io.Reader, w io.Writer, h Handler) *Peer {
	return &Peer{r: NewReader(r), w: NewWriter(w), h: h}
}

// Run reads records until ctx ends or the reader fails.
func (p *Peer) Run(ctx context.Context) error {
	return errs.ErrNotImplemented
}

// Call sends a request and waits for its response. It decodes the result
// into out when out is not nil.
func (p *Peer) Call(ctx context.Context, method string, params, out any) error {
	return errs.ErrNotImplemented
}

// Notify sends a notification.
func (p *Peer) Notify(ctx context.Context, method string, params any) error {
	return errs.ErrNotImplemented
}
