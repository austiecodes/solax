package framing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
)

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

// ErrClosed is returned by Call after the peer stopped reading.
var ErrClosed = errors.New("framing: rpc peer closed")

// Handler handles requests and notifications from the peer. Codex
// app-server sends requests to the client (approvals), so the connection is
// bidirectional. Run calls the handler on its own goroutine, in record
// order; a handler must not block on Call.
type Handler interface {
	// HandleRequest answers a request from the peer.
	HandleRequest(ctx context.Context, method string, params json.RawMessage) (any, error)
	// HandleNotification handles a notification from the peer. raw is the
	// whole record, for decoders and transcripts.
	HandleNotification(ctx context.Context, method string, params json.RawMessage, raw []byte)
}

// Peer is one JSON-RPC 2.0 connection over JSONL. Codex omits the
// "jsonrpc" member; the peer neither sends nor requires it.
type Peer struct {
	r *Reader
	w *Writer
	h Handler

	next    atomic.Int64
	mu      sync.Mutex
	pending map[string]chan rpcMessage
	closed  bool
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

// NewPeer returns a Peer. Call Run to start reading.
func NewPeer(r io.Reader, w io.Writer, h Handler) *Peer {
	return &Peer{r: NewReader(r), w: NewWriter(w), h: h, pending: map[string]chan rpcMessage{}}
}

// Run reads records until the reader fails. It fails all open calls when
// it returns. A record that is not valid JSON is skipped.
func (p *Peer) Run(ctx context.Context) error {
	defer p.closeAll()
	for {
		rec, err := p.r.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		var m rpcMessage
		if json.Unmarshal(rec, &m) != nil {
			continue
		}
		switch {
		case m.Method != "" && len(m.ID) > 0:
			p.answer(ctx, m)
		case m.Method != "":
			p.h.HandleNotification(ctx, m.Method, m.Params, rec)
		case len(m.ID) > 0:
			p.mu.Lock()
			ch, ok := p.pending[idKey(m.ID)]
			delete(p.pending, idKey(m.ID))
			p.mu.Unlock()
			if ok {
				ch <- m
			}
		}
	}
}

func (p *Peer) answer(ctx context.Context, m rpcMessage) {
	res, err := p.h.HandleRequest(ctx, m.Method, m.Params)
	out := map[string]any{"id": m.ID}
	if err != nil {
		var re *RPCError
		if !errors.As(err, &re) {
			re = &RPCError{Code: -32603, Message: err.Error()}
		}
		out["error"] = re
	} else {
		out["result"] = res
	}
	_ = p.w.Write(out)
}

// Call sends a request and waits for its response. It decodes the result
// into out when out is not nil.
func (p *Peer) Call(ctx context.Context, method string, params, out any) error {
	id := strconv.FormatInt(p.next.Add(1), 10)
	ch := make(chan rpcMessage, 1)
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrClosed
	}
	p.pending[id] = ch
	p.mu.Unlock()

	n, _ := strconv.ParseInt(id, 10, 64)
	if err := p.w.Write(map[string]any{"id": n, "method": method, "params": params}); err != nil {
		p.drop(id)
		return err
	}
	select {
	case <-ctx.Done():
		p.drop(id)
		return ctx.Err()
	case m, ok := <-ch:
		if !ok {
			return ErrClosed
		}
		if m.Error != nil {
			return m.Error
		}
		if out != nil && len(m.Result) > 0 {
			return json.Unmarshal(m.Result, out)
		}
		return nil
	}
}

// Notify sends a notification.
func (p *Peer) Notify(ctx context.Context, method string, params any) error {
	return p.w.Write(map[string]any{"method": method, "params": params})
}

func (p *Peer) drop(id string) {
	p.mu.Lock()
	delete(p.pending, id)
	p.mu.Unlock()
}

func (p *Peer) closeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	for id, ch := range p.pending {
		close(ch)
		delete(p.pending, id)
	}
}

// idKey normalizes an ID: 7 and "7" are the same call.
func idKey(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}
