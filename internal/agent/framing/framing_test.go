package framing

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// Records split only on '\n'. U+2028 inside a string is data, '\r\n' is
// accepted, blank lines are skipped, and a last line without '\n' counts.
func TestReaderSplitsOnLFOnly(t *testing.T) {
	in := "{\"a\":\"x y\"}\r\n\n{\"b\":1}\n{\"c\":2}"
	r := NewReader(strings.NewReader(in))
	want := []string{"{\"a\":\"x y\"}", `{"b":1}`, `{"c":2}`}
	for i, w := range want {
		got, err := r.Next()
		if err != nil || string(got) != w {
			t.Fatalf("record %d = %q, %v; want %q", i, got, err, w)
		}
	}
	if _, err := r.Next(); err != io.EOF {
		t.Fatalf("after last record: %v, want EOF", err)
	}
}

// A record longer than the bufio buffer is joined, not cut.
func TestReaderLongRecord(t *testing.T) {
	long := `{"x":"` + strings.Repeat("a", 200<<10) + `"}`
	got, err := NewReader(strings.NewReader(long + "\n")).Next()
	if err != nil || string(got) != long {
		t.Fatalf("len = %d, err = %v", len(got), err)
	}
}

type echoHandler struct {
	mu    sync.Mutex
	notes []string
}

func (h *echoHandler) HandleRequest(_ context.Context, method string, p json.RawMessage) (any, error) {
	if method == "fail" {
		return nil, errors.New("boom")
	}
	return map[string]string{"echo": method}, nil
}

func (h *echoHandler) HandleNotification(_ context.Context, method string, _ json.RawMessage, _ []byte) {
	h.mu.Lock()
	h.notes = append(h.notes, method)
	h.mu.Unlock()
}

// Two peers talk to each other: calls in both directions, errors and
// notifications all arrive.
func TestPeerRoundTrip(t *testing.T) {
	ar, bw := io.Pipe()
	br, aw := io.Pipe()
	ha, hb := &echoHandler{}, &echoHandler{}
	a, b := NewPeer(ar, aw, ha), NewPeer(br, bw, hb)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go a.Run(ctx)
	go b.Run(ctx)

	var out struct{ Echo string }
	if err := a.Call(ctx, "ping", nil, &out); err != nil || out.Echo != "ping" {
		t.Fatalf("a→b call = %+v, %v", out, err)
	}
	if err := b.Call(ctx, "pong", nil, &out); err != nil || out.Echo != "pong" {
		t.Fatalf("b→a call = %+v, %v", out, err)
	}
	var re *RPCError
	if err := a.Call(ctx, "fail", nil, nil); !errors.As(err, &re) {
		t.Fatalf("failing call returned %v, want an RPCError", err)
	}
	if err := a.Notify(ctx, "hello", nil); err != nil {
		t.Fatal(err)
	}
	// A call after the notification proves the notification was read.
	_ = a.Call(ctx, "ping", nil, nil)
	hb.mu.Lock()
	defer hb.mu.Unlock()
	if len(hb.notes) != 1 || hb.notes[0] != "hello" {
		t.Fatalf("notifications = %v", hb.notes)
	}
}

// Open calls fail when the peer stops reading.
func TestPeerCloseFailsCalls(t *testing.T) {
	r, w := io.Pipe()
	p := NewPeer(r, io.Discard, &echoHandler{})
	done := make(chan error, 1)
	go func() { done <- p.Call(context.Background(), "x", nil, nil) }()
	go p.Run(context.Background())
	time.Sleep(20 * time.Millisecond)
	w.Close()
	if err := <-done; !errors.Is(err, ErrClosed) {
		t.Fatalf("call = %v, want ErrClosed", err)
	}
}
