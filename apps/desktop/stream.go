package main

import (
	"context"
	"sync"
	"time"

	"github.com/egoist/mygo"

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

// Events that the page receives. They are package-level so that
// `mygo generate` finds them. Each one mirrors a /v1/stream event.
var (
	ConnectionChanged = mygo.NewEvent[Connection]("connection")
	MessageCreated    = mygo.NewEvent[protocol.Message]("messageCreated")
	TaskUpdated       = mygo.NewEvent[protocol.Task]("taskUpdated")
	MachineUpdated    = mygo.NewEvent[protocol.Machine]("machineUpdated")
	SuperReplied      = mygo.NewEvent[protocol.Message]("superReplied")
)

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
