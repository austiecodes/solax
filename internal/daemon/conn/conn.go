// Package conn keeps the one outbound WebSocket from the daemon to the
// server. It reconnects with backoff and sends Hello after each connect.
//
// Records to the server go through one ordered queue. While the server is
// unreachable, reliable records wait in the queue (bounded) and live
// records (deltas) are dropped. The server stores session events by
// (session, seq), so a record sent twice is harmless.
package conn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/austiecodes/solax/pkg/protocol"
)

const (
	heartbeatEvery = 15 * time.Second
	writeTimeout   = 15 * time.Second
	maxQueue       = 20000
	maxBackoff     = 30 * time.Second
)

// Handler handles a command from the server and returns its ack.
type Handler func(ctx context.Context, env protocol.Envelope) protocol.Ack

// Conn is the daemon side of the daemon protocol.
type Conn struct {
	server    string
	token     string
	hello     func(ctx context.Context) protocol.Hello
	heartbeat func() protocol.Heartbeat
	handle    Handler
	log       *slog.Logger

	connected atomic.Bool

	mu    sync.Mutex
	queue []protocol.Envelope
	wake  chan struct{}
}

// New returns a Conn. hello builds the Hello record for each connect; it
// receives the connection context, so a probe cannot outlive the link.
func New(server, token string, hello func(ctx context.Context) protocol.Hello, heartbeat func() protocol.Heartbeat, h Handler, log *slog.Logger) *Conn {
	if log == nil {
		log = slog.Default()
	}
	return &Conn{server: server, token: token, hello: hello, heartbeat: heartbeat, handle: h, log: log, wake: make(chan struct{}, 1)}
}

// Run connects and reconnects until ctx ends.
func (c *Conn) Run(ctx context.Context) error {
	backoff := time.Second
	for {
		start := time.Now()
		err := c.session(ctx)
		c.connected.Store(false)
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(start) > time.Minute {
			backoff = time.Second
		}
		c.log.Warn("server connection lost", "err", err, "retry_in", backoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

// URL returns the WebSocket URL of the daemon endpoint.
func URL(server string) string {
	u := strings.TrimRight(server, "/")
	switch {
	case strings.HasPrefix(u, "https://"):
		u = "wss://" + strings.TrimPrefix(u, "https://")
	case strings.HasPrefix(u, "http://"):
		u = "ws://" + strings.TrimPrefix(u, "http://")
	}
	return u + protocol.DaemonPath
}

// session runs one connection until it fails.
func (c *Conn) session(ctx context.Context) error {
	ws, resp, err := websocket.DefaultDialer.DialContext(ctx, URL(c.server), http.Header{"Authorization": {"Bearer " + c.token}})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("machine is not approved yet (solax machine approve <code>): %w", err)
		}
		return err
	}
	ws.SetReadLimit(16 << 20)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Closing the connection is the only way to stop a blocked read.
	go func() {
		<-ctx.Done()
		ws.Close()
	}()

	var wmu sync.Mutex // gorilla allows one writer at a time
	write := func(env protocol.Envelope) error {
		wmu.Lock()
		defer wmu.Unlock()
		_ = ws.SetWriteDeadline(time.Now().Add(writeTimeout))
		return ws.WriteJSON(env)
	}
	if err := write(protocol.Envelope{Type: protocol.TypeHello, Payload: mustJSON(c.hello(ctx))}); err != nil {
		return err
	}
	c.connected.Store(true)
	c.log.Info("connected to server", "server", c.server)

	errc := make(chan error, 3)
	go func() { errc <- c.writeLoop(ctx, write) }()
	go func() { errc <- c.heartbeatLoop(ctx, write) }()
	go func() { errc <- c.readLoop(ctx, ws, write) }()
	return <-errc
}

func (c *Conn) readLoop(ctx context.Context, ws *websocket.Conn, write func(protocol.Envelope) error) error {
	for {
		var env protocol.Envelope
		if err := ws.ReadJSON(&env); err != nil {
			return err
		}
		if env.Type == protocol.TypeAck {
			continue // the daemon does not wait for acks
		}
		// Commands run concurrently: a slow session start must not hold up
		// the delivery to another session.
		go func() {
			ack := c.handle(ctx, env)
			if env.ID != "" {
				_ = write(protocol.Envelope{ID: env.ID, Type: protocol.TypeAck, Payload: mustJSON(ack)})
			}
		}()
	}
}

func (c *Conn) writeLoop(ctx context.Context, write func(protocol.Envelope) error) error {
	for {
		c.mu.Lock()
		var next *protocol.Envelope
		if len(c.queue) > 0 {
			next = &c.queue[0]
		}
		c.mu.Unlock()
		if next == nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-c.wake:
				continue
			}
		}
		if err := write(*next); err != nil {
			return err // the record stays queued for the next connection
		}
		c.mu.Lock()
		c.queue = c.queue[1:]
		c.mu.Unlock()
	}
}

func (c *Conn) heartbeatLoop(ctx context.Context, write func(protocol.Envelope) error) error {
	t := time.NewTicker(heartbeatEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if c.heartbeat == nil {
				continue
			}
			if err := write(protocol.Envelope{Type: protocol.TypeHeartbeat, Payload: mustJSON(c.heartbeat())}); err != nil {
				return err
			}
		}
	}
}

// ErrQueueFull is returned when the server was away for too long.
var ErrQueueFull = errors.New("conn: send queue full")

// Send queues a record for the server. Live session events are dropped
// while the server is not connected. It never blocks on the network.
func (c *Conn) Send(ctx context.Context, env protocol.Envelope) error {
	if !c.connected.Load() && isLive(env) {
		return nil
	}
	c.mu.Lock()
	if len(c.queue) >= maxQueue {
		c.mu.Unlock()
		return ErrQueueFull
	}
	c.queue = append(c.queue, env)
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return nil
}

func isLive(env protocol.Envelope) bool {
	if env.Type != protocol.TypeSessionEvent {
		return false
	}
	var se struct {
		Channel protocol.Channel `json:"channel"`
	}
	_ = json.Unmarshal(env.Payload, &se)
	return se.Channel == protocol.ChannelLive
}

// Connected reports whether the connection is up.
func (c *Conn) Connected() bool { return c.connected.Load() }

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return b
}
