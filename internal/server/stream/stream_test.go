package stream

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/austiecodes/solax/internal/server/model"
)

const owner = model.ID("own_1")

func serve(t *testing.T, s *Stream) *websocket.Conn {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.Serve(w, r, owner) }))
	t.Cleanup(srv.Close)
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	return ws
}

func subs(s *Stream) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.subs[owner])
}

// The client is subscribed when its dial returns, so a push that follows
// reaches it.
func TestSubscribedOnUpgrade(t *testing.T) {
	s := New()
	ws := serve(t, s)
	if n := subs(s); n != 1 {
		t.Fatalf("subscribers after dial = %d, want 1", n)
	}
	s.Publish(context.Background(), owner, "task", map[string]string{"id": "t1"})
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, b, err := ws.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"t1"`) {
		t.Fatalf("push = %s", b)
	}
}

// A client that cannot take a push is closed with "try again later",
// so it reconnects and reads the state again instead of keeping a gap.
func TestLaggingClientClosed(t *testing.T) {
	s := New()
	sb := newSub()
	s.add(owner, sb)
	for range queueSize {
		s.Publish(context.Background(), owner, "session.event", nil)
	}
	select {
	case <-sb.lag:
		t.Fatal("lag before the queue is full")
	default:
	}
	s.Publish(context.Background(), owner, "task", nil)
	s.Publish(context.Background(), owner, "task", nil) // a second loss is harmless
	select {
	case <-sb.lag:
	default:
		t.Fatal("no lag after a lost push")
	}

	// End to end: Serve sends the close frame and ends.
	ws := serve(t, s)
	s.mu.RLock()
	var live *sub
	for x := range s.subs[owner] {
		if x != sb {
			live = x
		}
	}
	s.mu.RUnlock()
	live.lost()
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, _, err := ws.ReadMessage()
		if err == nil {
			continue
		}
		var ce *websocket.CloseError
		if !errors.As(err, &ce) || ce.Code != websocket.CloseTryAgainLater {
			t.Fatalf("read error = %v, want close %d", err, websocket.CloseTryAgainLater)
		}
		break
	}
}
