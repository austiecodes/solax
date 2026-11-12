// Package control is the local control channel between "solax daemon ..."
// commands and the running daemon: a small HTTP API on a Unix socket with
// mode 0600.
package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
)

// Status is the answer of GET /status.
type Status struct {
	PID       int                        `json:"pid"`
	Version   string                     `json:"version"`
	Machine   string                     `json:"machine"`
	Backend   string                     `json:"backend"`
	Connected bool                       `json:"connected"`
	FreeSlots int                        `json:"free_slots"`
	Sessions  map[string]ir.SessionState `json:"sessions"`
}

// Daemon is what the control server needs from the running daemon.
type Daemon interface {
	Status() Status
	Reload(ctx context.Context) error
	Stop()
	Logs(ctx context.Context, w io.Writer, follow bool) error
}

// Server serves the control API on the socket.
type Server struct {
	path string
	d    Daemon
}

// NewServer returns a control server for the socket at path.
func NewServer(path string, d Daemon) *Server { return &Server{path: path, d: d} }

// Serve listens on the socket until ctx ends. It removes a stale socket
// file first and sets mode 0600.
func (s *Server) Serve(ctx context.Context) error {
	_ = os.Remove(s.path)
	ln, err := net.Listen("unix", s.path)
	if err != nil {
		return fmt.Errorf("control socket: %w", err)
	}
	defer os.Remove(s.path)
	if err := os.Chmod(s.path, 0o600); err != nil {
		ln.Close()
		return err
	}
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Handler returns the HTTP routes: GET /status, POST /reload, POST /stop,
// GET /logs?follow=1.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(s.d.Status())
	})
	mux.HandleFunc("POST /reload", func(w http.ResponseWriter, r *http.Request) {
		if err := s.d.Reload(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	})
	mux.HandleFunc("POST /stop", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		go s.d.Stop()
	})
	mux.HandleFunc("GET /logs", func(w http.ResponseWriter, r *http.Request) {
		_ = s.d.Logs(r.Context(), flushWriter{w}, r.URL.Query().Get("follow") == "1")
	})
	return mux
}

type flushWriter struct{ w http.ResponseWriter }

func (f flushWriter) Write(b []byte) (int, error) {
	n, err := f.w.Write(b)
	if fl, ok := f.w.(http.Flusher); ok {
		fl.Flush()
	}
	return n, err
}

// Client calls the control API of the local daemon.
type Client struct {
	path string
	hc   *http.Client
}

// NewClient returns a client for the socket at path.
func NewClient(path string) *Client {
	return &Client{path: path, hc: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", path)
		},
	}}}
}

func (c *Client) do(ctx context.Context, method, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://daemon"+path, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("no daemon runs (%s): %w", c.path, errs.ErrUnavailable)
	}
	if res.StatusCode >= 300 {
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return nil, errors.New(strings.TrimSpace(string(b)))
	}
	return res, nil
}

// Status returns the daemon status. It returns errs.ErrUnavailable when
// no daemon listens on the socket.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var st Status
	res, err := c.do(ctx, http.MethodGet, "/status")
	if err != nil {
		return st, err
	}
	defer res.Body.Close()
	return st, json.NewDecoder(res.Body).Decode(&st)
}

func (c *Client) Reload(ctx context.Context) error {
	res, err := c.do(ctx, http.MethodPost, "/reload")
	if err == nil {
		res.Body.Close()
	}
	return err
}

func (c *Client) Stop(ctx context.Context) error {
	res, err := c.do(ctx, http.MethodPost, "/stop")
	if err == nil {
		res.Body.Close()
	}
	return err
}

func (c *Client) Logs(ctx context.Context, w io.Writer, follow bool) error {
	q := ""
	if follow {
		q = "?follow=1"
	}
	res, err := c.do(ctx, http.MethodGet, "/logs"+q)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, err = io.Copy(w, res.Body)
	return err
}
