package conn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/austiecodes/solax/pkg/protocol"
)

func short(t *testing.T) {
	t.Helper()
	hb, rt := heartbeatEvery, readTimeout
	heartbeatEvery, readTimeout = 50*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { heartbeatEvery, readTimeout = hb, rt })
}

// server upgrades daemon connections. A live server keeps reading, which
// answers pings; a dead one reads the hello and then hangs, as a
// half-open link does. closed gets the close code of each connection.
func server(t *testing.T, live bool, closed chan<- int) string {
	t.Helper()
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		if !live {
			_, _, _ = ws.ReadMessage()
			<-r.Context().Done()
			return
		}
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				code := -1
				if ce, ok := err.(*websocket.CloseError); ok {
					code = ce.Code
				}
				closed <- code
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func newConn(url string) *Conn {
	hello := func(context.Context) protocol.Hello { return protocol.Hello{} }
	hb := func() protocol.Heartbeat { return protocol.Heartbeat{} }
	return New(url, "tok", hello, hb, func(context.Context, protocol.Envelope) protocol.Ack { return protocol.Ack{OK: true} }, nil)
}

// A link where the server is gone without a close ends after readTimeout,
// so the daemon reconnects.
func TestDeadLinkEnds(t *testing.T) {
	short(t)
	c := newConn(server(t, false, nil))
	errc := make(chan error, 1)
	go func() { errc <- c.session(context.Background()) }()
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("session ended without an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session on a dead link did not end")
	}
}

// A link where the server answers pings stays up past readTimeout, and
// the daemon closes it with "going away" when it stops.
func TestLiveLinkStays(t *testing.T) {
	short(t)
	closed := make(chan int, 1)
	c := newConn(server(t, true, closed))
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- c.session(ctx) }()
	select {
	case err := <-errc:
		t.Fatalf("live session ended: %v", err)
	case <-time.After(3 * readTimeout):
	}
	if !c.Connected() {
		t.Fatal("not connected")
	}
	cancel()
	select {
	case code := <-closed:
		if code != websocket.CloseGoingAway {
			t.Fatalf("close code = %d, want %d", code, websocket.CloseGoingAway)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server saw no close")
	}
	<-errc // the session reads the timeouts until it ends
}
