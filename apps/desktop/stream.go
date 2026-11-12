package main

import (
	"context"
	"sync"
	"time"

	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// ConnectionState is the state of the server stream.
type ConnectionState string

const (
	Disconnected ConnectionState = "disconnected"
	Connecting   ConnectionState = "connecting"
	Connected    ConnectionState = "connected"
)

// Connection is the payload of the connection event.
type Connection struct {
	State  ConnectionState `json:"state"`
	Server string          `json:"server,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// Native Go events mirror the server stream without a JavaScript bridge.
var (
	ConnectionChanged = newEvent[Connection]("connection")
	MessageCreated    = newEvent[protocol.Message]("messageCreated")
	TaskUpdated       = newEvent[protocol.Task]("taskUpdated")
	MachineUpdated    = newEvent[protocol.Machine]("machineUpdated")
	SuperReplied      = newEvent[protocol.Message]("superReplied")
	SessionEvent      = newEvent[SessionEventRecord]("sessionEvent")
)

// SessionEventRecord is the payload of session.event on /v1/stream: one IR
// event of one agent session, so native views can show live execution.
type SessionEventRecord struct {
	SessionID string   `json:"session_id"`
	Event     ir.Event `json:"event"`
}

// stream keeps one WebSocket to /v1/stream for the profile in use and
// broadcasts its events to all windows. It reconnects with backoff.
type stream struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

// Restart closes the current connection and connects to server.
func (s *stream) Restart(server, token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go s.run(ctx, server, token)
}

// Stop closes the connection.
func (s *stream) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

// run connects, decodes stream records and broadcasts them as events.
// TODO: dial the WebSocket, map protocol.Stream* records to the events,
// and show a notification when the Super Agent asks the user a question.
func (s *stream) run(ctx context.Context, server, token string) {
	ConnectionChanged.Broadcast(Connection{State: Connecting, Server: server})
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		ConnectionChanged.Broadcast(Connection{State: Disconnected, Server: server, Error: "stream: not implemented"})
	}
}

type event[T any] struct {
	mu        sync.RWMutex
	listeners []func(T)
}

func newEvent[T any](_ string) *event[T] { return &event[T]{} }
func (e *event[T]) listen(fn func(T)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.listeners = append(e.listeners, fn)
}
func (e *event[T]) Broadcast(value T) {
	e.mu.RLock()
	fns := append([]func(T){}, e.listeners...)
	e.mu.RUnlock()
	for _, fn := range fns {
		fn(value)
	}
}
