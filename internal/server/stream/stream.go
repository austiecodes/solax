// Package stream pushes live events to connected clients over WebSocket
// (/v1/stream). It implements core.Notifier.
package stream

import (
	"context"
	"net/http"

	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/errs"
)

// Stream fans out events to the clients of each owner.
type Stream struct{}

// New returns a Stream.
func New() *Stream { return &Stream{} }

// ServeHTTP upgrades a client connection and subscribes it.
func (s *Stream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	http.Error(w, errs.ErrNotImplemented.Error(), http.StatusNotImplemented)
}

// Publish sends an event to all clients of an owner. It drops the event
// for clients that read too slowly.
func (s *Stream) Publish(ctx context.Context, ownerID core.ID, event string, payload any) error {
	return errs.ErrNotImplemented
}

var _ core.Notifier = (*Stream)(nil)
