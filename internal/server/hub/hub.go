// Package hub keeps the WebSocket connections of the daemons. It
// implements core.Dispatcher for commands to daemons, and reports what
// daemons say to core.DaemonFacts.
package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/austiecodes/solax/internal/auth"
	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/protocol"
)

const (
	helloTimeout = 30 * time.Second
	writeTimeout = 15 * time.Second
	// readTimeout closes a silent connection. The daemon sends a heartbeat
	// every 15 seconds.
	readTimeout = 60 * time.Second
	maxRecord   = 16 << 20
)

// upgrader accepts daemon connections. Daemons are not browsers, so the
// origin check does not apply; the machine token authenticates them.
var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

// Hub holds one connection for each online machine.
type Hub struct {
	auth  auth.Verifier
	facts core.DaemonFacts
	log   *slog.Logger

	mu    sync.RWMutex
	conns map[core.ID]*conn
}

// conn is one daemon connection. It correlates commands with acks by ID.
type conn struct {
	machineID core.ID
	ws        *websocket.Conn
	done      chan struct{}

	wmu sync.Mutex // one writer at a time

	next    atomic.Int64
	pmu     sync.Mutex
	pending map[string]chan protocol.Ack
}

// New returns a Hub. Call SetFacts before serving, because the Engine and
// the Hub depend on each other.
func New(v auth.Verifier, log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	return &Hub{auth: v, log: log, conns: map[core.ID]*conn{}}
}

// SetFacts sets the receiver of daemon facts.
func (h *Hub) SetFacts(f core.DaemonFacts) { h.facts = f }

// ServeHTTP verifies the machine token (the daemon generated it; the server
// has its hash), upgrades to WebSocket, waits for Hello, then reads records
// until the connection closes. A pending machine gets 403 with
// "pending approval", and the daemon retries with backoff.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	claims, err := h.auth.Verify(r.Context(), token)
	switch {
	case err == nil && claims.Kind == auth.KindMachine:
	case errors.Is(err, errs.ErrForbidden):
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	default:
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(maxRecord)
	c := &conn{machineID: claims.Subject, ws: ws, done: make(chan struct{}), pending: map[string]chan protocol.Ack{}}
	defer close(c.done)
	// The request context ends with the handler; the connection lives on it.
	ctx := r.Context()
	if err := h.handshake(ctx, c); err != nil {
		h.log.Warn("daemon handshake failed", "machine", c.machineID, "err", err)
		c.close(websocket.ClosePolicyViolation, "handshake failed")
		return
	}
	h.log.Info("daemon connected", "machine", c.machineID)
	err = h.readLoop(ctx, c)
	h.log.Info("daemon disconnected", "machine", c.machineID, "err", err)
	if h.drop(c) {
		_ = h.facts.MachineDisconnected(context.WithoutCancel(ctx), c.machineID)
	}
	ws.Close()
}

func (h *Hub) handshake(ctx context.Context, c *conn) error {
	var env protocol.Envelope
	_ = c.ws.SetReadDeadline(time.Now().Add(helloTimeout))
	if err := c.ws.ReadJSON(&env); err != nil {
		return err
	}
	if env.Type != protocol.TypeHello {
		return fmt.Errorf("first record is %q, want hello", env.Type)
	}
	var hello protocol.Hello
	if err := json.Unmarshal(env.Payload, &hello); err != nil {
		return err
	}
	// Register first, so the delivery loop that MachineConnected wakes
	// finds the connection.
	h.mu.Lock()
	old := h.conns[c.machineID]
	h.conns[c.machineID] = c
	h.mu.Unlock()
	if old != nil {
		old.close(websocket.CloseGoingAway, "replaced by a new connection")
	}
	if err := h.facts.MachineConnected(ctx, c.machineID, hello); err != nil {
		h.drop(c)
		return err
	}
	return nil
}

// drop removes c if it is still the connection of its machine.
func (h *Hub) drop(c *conn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[c.machineID] != c {
		return false
	}
	delete(h.conns, c.machineID)
	return true
}

func (h *Hub) readLoop(ctx context.Context, c *conn) error {
	for {
		var env protocol.Envelope
		_ = c.ws.SetReadDeadline(time.Now().Add(readTimeout))
		if err := c.ws.ReadJSON(&env); err != nil {
			c.failPending()
			return err
		}
		if env.Type == protocol.TypeAck {
			c.resolve(env)
			continue
		}
		err := h.fact(ctx, c, env)
		if err != nil {
			h.log.Warn("daemon record failed", "machine", c.machineID, "type", env.Type, "err", err)
		}
		if env.ID != "" {
			ack := protocol.Ack{OK: err == nil}
			if err != nil {
				ack.Error = err.Error()
			}
			_ = c.write(ctx, protocol.Envelope{ID: env.ID, Type: protocol.TypeAck, Payload: mustJSON(ack)})
		}
	}
}

func (h *Hub) fact(ctx context.Context, c *conn, env protocol.Envelope) error {
	switch env.Type {
	case protocol.TypeHello:
		var v protocol.Hello
		if err := json.Unmarshal(env.Payload, &v); err != nil {
			return err
		}
		return h.facts.MachineConnected(ctx, c.machineID, v)
	case protocol.TypeHeartbeat:
		var v protocol.Heartbeat
		if err := json.Unmarshal(env.Payload, &v); err != nil {
			return err
		}
		return h.facts.Heartbeat(ctx, c.machineID, v)
	case protocol.TypeSessionEvent:
		var v protocol.SessionEvent
		if err := json.Unmarshal(env.Payload, &v); err != nil {
			return err
		}
		return h.facts.SessionEvent(ctx, c.machineID, v)
	case protocol.TypeSessionState:
		var v protocol.SessionStateReport
		if err := json.Unmarshal(env.Payload, &v); err != nil {
			return err
		}
		return h.facts.SessionStateChanged(ctx, c.machineID, v)
	}
	return fmt.Errorf("%w: unknown record %q", errs.ErrInvalid, env.Type)
}

// Send sends a command to a machine and waits for its ack. A command that
// the daemon rejects gives an Ack with OK false and no error.
func (h *Hub) Send(ctx context.Context, machineID core.ID, env protocol.Envelope) (protocol.Ack, error) {
	h.mu.RLock()
	c := h.conns[machineID]
	h.mu.RUnlock()
	if c == nil {
		return protocol.Ack{}, fmt.Errorf("machine %s: %w", machineID, errs.ErrUnavailable)
	}
	env.ID = "s" + strconv.FormatInt(c.next.Add(1), 10)
	ch := make(chan protocol.Ack, 1)
	c.pmu.Lock()
	c.pending[env.ID] = ch
	c.pmu.Unlock()
	defer func() {
		c.pmu.Lock()
		delete(c.pending, env.ID)
		c.pmu.Unlock()
	}()
	if err := c.write(ctx, env); err != nil {
		return protocol.Ack{}, fmt.Errorf("machine %s: %w: %v", machineID, errs.ErrUnavailable, err)
	}
	select {
	case ack, ok := <-ch:
		if !ok {
			return protocol.Ack{}, fmt.Errorf("machine %s: %w: connection closed", machineID, errs.ErrUnavailable)
		}
		return ack, nil
	case <-ctx.Done():
		return protocol.Ack{}, ctx.Err()
	}
}

// Online reports whether a machine has a live connection.
func (h *Hub) Online(machineID core.ID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.conns[machineID]
	return ok
}

// Disconnect closes the connection of a machine, if any.
func (h *Hub) Disconnect(machineID core.ID) {
	h.mu.RLock()
	c := h.conns[machineID]
	h.mu.RUnlock()
	if c != nil {
		c.close(websocket.ClosePolicyViolation, "machine revoked")
	}
}

// write sends one record. Gorilla allows one writer at a time.
func (c *conn) write(_ context.Context, env protocol.Envelope) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(writeTimeout))
	return c.ws.WriteJSON(env)
}

// close sends a close frame and closes the connection. The read loop of
// the connection then ends.
func (c *conn) close(code int, reason string) {
	c.wmu.Lock()
	_ = c.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
	c.wmu.Unlock()
	_ = c.ws.Close()
}

func (c *conn) resolve(env protocol.Envelope) {
	var ack protocol.Ack
	if err := json.Unmarshal(env.Payload, &ack); err != nil {
		ack = protocol.Ack{Error: "bad ack: " + err.Error()}
	}
	c.pmu.Lock()
	ch, ok := c.pending[env.ID]
	delete(c.pending, env.ID)
	c.pmu.Unlock()
	if ok {
		ch <- ack
	}
}

func (c *conn) failPending() {
	c.pmu.Lock()
	defer c.pmu.Unlock()
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return b
}

var _ core.Dispatcher = (*Hub)(nil)
