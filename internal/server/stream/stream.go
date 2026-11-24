// Package stream pushes live events to connected clients over WebSocket
// (/v1/stream). Its Publish is service.Publish.
//
// A client that connects reads the state from the REST API once the
// connection is up; no push after that is lost. A client that reads too
// slowly is disconnected rather than left with a gap: it reconnects and
// reads the state again. Every push is a protocol.Push record.
package stream

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/austiecodes/solax/internal/server/model"
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
	subs map[model.ID]map[*sub]struct{}
}

type sub struct {
	ch   chan []byte
	lag  chan struct{} // closed when ch is full and a push is lost
	once sync.Once
}

func newSub() *sub { return &sub{ch: make(chan []byte, queueSize), lag: make(chan struct{})} }

// lost marks that the client missed a push.
func (sb *sub) lost() { sb.once.Do(func() { close(sb.lag) }) }

// New returns a Stream.
func New() *Stream { return &Stream{subs: map[model.ID]map[*sub]struct{}{}} }

// upgrader accepts client connections. Clients authenticate with a bearer
// token header, which a browser page cannot send cross-site, so the origin
// check adds nothing.
var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

// Serve upgrades a client connection of an owner and sends pushes until
// the client goes away. The caller has verified the token.
func (s *Stream) Serve(w http.ResponseWriter, r *http.Request, owner model.ID) {
	// Subscribe before the upgrade: the client reads the state when the
	// upgrade completes, so a push from then on must reach it.
	sb := newSub()
	s.add(owner, sb)
	defer s.remove(owner, sb)
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()

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
		case <-sb.lag:
			_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "client too slow"), time.Now().Add(time.Second))
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

// Publish sends an event to all clients of an owner. A client that reads
// too slowly to take the event is disconnected.
func (s *Stream) Publish(ctx context.Context, ownerID model.ID, event string, payload any) {
	push := protocol.Push{Event: event}
	if payload != nil {
		p, err := json.Marshal(payload)
		if err != nil {
			return
		}
		push.Payload = p
	}
	b, err := json.Marshal(push)
	if err != nil {
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for sb := range s.subs[ownerID] {
		select {
		case sb.ch <- b:
		default:
			sb.lost()
		}
	}
}

func (s *Stream) add(owner model.ID, sb *sub) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subs[owner] == nil {
		s.subs[owner] = map[*sub]struct{}{}
	}
	s.subs[owner][sb] = struct{}{}
}

func (s *Stream) remove(owner model.ID, sb *sub) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subs[owner], sb)
}
