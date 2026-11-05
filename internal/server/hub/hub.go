// Package hub keeps the WebSocket connections of the daemons. It
// implements core.Dispatcher for commands to daemons, and reports what
// daemons say to core.DaemonFacts.
package hub

import (
	"context"
	"net/http"
	"sync"

	"github.com/austiecodes/solax/internal/auth"
	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Hub holds one connection for each online machine.
type Hub struct {
	auth  auth.Verifier
	facts core.DaemonFacts

	mu    sync.RWMutex
	conns map[core.ID]*conn
}

// conn is one daemon connection. It correlates commands with acks by ID.
type conn struct {
	machineID core.ID
}

// New returns a Hub. Call SetFacts before serving, because the Engine and
// the Hub depend on each other.
func New(v auth.Verifier) *Hub {
	return &Hub{auth: v, conns: map[core.ID]*conn{}}
}

// SetFacts sets the receiver of daemon facts.
func (h *Hub) SetFacts(f core.DaemonFacts) { h.facts = f }

// ServeHTTP verifies the machine token (the daemon generated it; the server
// has its hash), upgrades to WebSocket, waits for Hello, then reads records
// until the connection closes. A pending machine gets 403 with
// "pending approval", and the daemon retries with backoff.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	http.Error(w, errs.ErrNotImplemented.Error(), http.StatusNotImplemented)
}

// Send sends a command to a machine and waits for its ack.
func (h *Hub) Send(ctx context.Context, machineID core.ID, env protocol.Envelope) (protocol.Ack, error) {
	return protocol.Ack{}, errs.ErrNotImplemented
}

// Online reports whether a machine has a live connection.
func (h *Hub) Online(machineID core.ID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.conns[machineID]
	return ok
}

var _ core.Dispatcher = (*Hub)(nil)
