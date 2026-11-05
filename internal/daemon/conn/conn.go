// Package conn keeps the one outbound WebSocket from the daemon to the
// server. It reconnects with backoff and sends Hello after each connect.
package conn

import (
	"context"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Handler handles a command from the server and returns its ack.
type Handler func(ctx context.Context, env protocol.Envelope) protocol.Ack

// Conn is the daemon side of the daemon protocol.
type Conn struct {
	server string
	token  string
	hello  func() protocol.Hello
	handle Handler
}

// New returns a Conn. hello builds the Hello record for each connect.
func New(server, token string, hello func() protocol.Hello, h Handler) *Conn {
	return &Conn{server: server, token: token, hello: hello, handle: h}
}

// Run connects and reconnects until ctx ends.
func (c *Conn) Run(ctx context.Context) error {
	return errs.ErrNotImplemented
}

// Send sends a record to the server. Reliable records wait for an ack.
func (c *Conn) Send(ctx context.Context, env protocol.Envelope) error {
	return errs.ErrNotImplemented
}

// Connected reports whether the connection is up.
func (c *Conn) Connected() bool { return false }
