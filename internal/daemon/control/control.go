// Package control is the local control channel between "solax daemon ..."
// commands and the running daemon: a small HTTP API on a Unix socket with
// mode 0600.
package control

import (
	"context"
	"io"
	"net/http"

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
	Agents    []string                   `json:"agents"`
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
	return errs.ErrNotImplemented
}

// Handler returns the HTTP routes: GET /status, POST /reload, POST /stop,
// GET /logs?follow=1.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	notImpl := func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, errs.ErrNotImplemented.Error(), http.StatusNotImplemented)
	}
	mux.HandleFunc("GET /status", notImpl)
	mux.HandleFunc("POST /reload", notImpl)
	mux.HandleFunc("POST /stop", notImpl)
	mux.HandleFunc("GET /logs", notImpl)
	return mux
}

// Client calls the control API of the local daemon.
type Client struct {
	path string
}

// NewClient returns a client for the socket at path.
func NewClient(path string) *Client { return &Client{path: path} }

// Status returns the daemon status. It returns errs.ErrUnavailable when
// no daemon listens on the socket.
func (c *Client) Status(ctx context.Context) (Status, error) {
	return Status{}, errs.ErrNotImplemented
}

func (c *Client) Reload(ctx context.Context) error { return errs.ErrNotImplemented }

func (c *Client) Stop(ctx context.Context) error { return errs.ErrNotImplemented }

func (c *Client) Logs(ctx context.Context, w io.Writer, follow bool) error {
	return errs.ErrNotImplemented
}
