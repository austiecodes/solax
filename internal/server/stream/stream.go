// Package stream pushes live events to connected clients over WebSocket
// (/v1/stream). It implements core.Notifier.
//
// The stream is best-effort: a client that reads too slowly loses pushes,
// and a client that connects late reads the state from the REST API. Every
// push is a protocol.Push record.
package stream

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/pkg/protocol"
)

const (
	// queueSize is the number of pushes a client may lag behind.
	queueSize = 1024
	pingEvery = 30 * time.Second
	pongWait  = 75 * time.Second
)

// Stream fans out events to the clients of each owner.
type Stream struct {
	mu   sync.RWMutex
	subs map[core.ID]map[*sub]struct{}
}

type sub struct {
	ch chan []byte
}

// New returns a Stream.
func New() *Stream { return &Stream{subs: map[core.ID]map[*sub]struct{}{}} }

// upgrader accepts client connections. Clients authenticate with a bearer
// token header, which a browser page cannot send cross-site, so the origin
// check adds nothing.
var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

// Serve upgrades a client connection of an owner and sends pushes until
// the client goes away. The caller has verified the token.
func (s *Stream) Serve(w http.ResponseWriter, r *http.Request, owner core.ID) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	sb := &sub{ch: make(chan []byte, queueSize)}
	s.add(owner, sb)
	defer s.remove(owner, sb)

	// The client sends nothing. Reading handles its close frame and its
	// pongs; a read error means the client is gone.
	gone := make(chan struct{})
	ws.SetReadLimit(4 << 10)
	_ = ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(pongWait)) })
	go func() {
		defer close(gone)
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case <-gone:
			return
		case <-r.Context().Done():
			return
		case b := <-sb.ch:
			_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := ws.WriteMessage(websocket.TextMessage, b); err != nil {
				return
			}
		case <-ping.C:
			if err := ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
				return
			}
		}
	}
}

// Publish sends an event to all clients of an owner. It drops the event
// for clients that read too slowly.
func (s *Stream) Publish(ctx context.Context, ownerID core.ID, event string, payload any) error {
	push := protocol.Push{Event: event}
	if payload != nil {
		p, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		push.Payload = p
	}
	b, err := json.Marshal(push)
	if err != nil {
		return err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for sb := range s.subs[ownerID] {
		select {
		case sb.ch <- b:
		default:
		}
	}
	return nil
}

func (s *Stream) add(owner core.ID, sb *sub) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subs[owner] == nil {
		s.subs[owner] = map[*sub]struct{}{}
	}
	s.subs[owner][sb] = struct{}{}
}

func (s *Stream) remove(owner core.ID, sb *sub) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subs[owner], sb)
}

var _ core.Notifier = (*Stream)(nil)
