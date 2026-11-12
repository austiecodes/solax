// Package delivery runs the delivery loop of the server: it takes the
// pending inputs of each session from the engine, starts the session on
// its machine when needed, and sends the inputs. It is the only caller of
// session.ensure and session.deliver.
//
// The loop is level-triggered: each pass asks the engine for all due work,
// so a lost wake-up or a failed send is repaired by the next pass.
package delivery

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/pkg/protocol"
)

const (
	// tick is the period of a pass without a wake-up. It retries failed
	// sends.
	tick = 5 * time.Second
	// ensureTimeout bounds a session start. A Codex handshake can take
	// most of a minute on a cold machine.
	ensureTimeout  = 2 * time.Minute
	deliverTimeout = 30 * time.Second
)

// Engine is the part of the core engine that the loop uses.
type Engine interface {
	PendingDeliveries(ctx context.Context) ([]core.Delivery, error)
	Delivered(ctx context.Context, sessionID core.ID, inputIDs []core.ID) error
	DeliveryFailed(ctx context.Context, sessionID core.ID, reason string) error
	SessionLost(ctx context.Context, sessionID core.ID) error
}

// unknownSession is the ack error of a daemon that does not run the
// session (supervisor.ErrUnknownSession). The server is not imported by
// the daemon, so the text is the contract.
const unknownSession = "unknown session"

// Loop delivers inputs.
type Loop struct {
	eng  Engine
	hub  core.Dispatcher
	log  *slog.Logger
	wake chan struct{}

	mu       sync.Mutex
	inFlight map[core.ID]bool // sessions with a delivery under way
}

// New returns a Loop.
func New(eng Engine, hub core.Dispatcher, log *slog.Logger) *Loop {
	return &Loop{eng: eng, hub: hub, log: log, wake: make(chan struct{}, 1), inFlight: map[core.ID]bool{}}
}

// Kick asks for a pass soon. It never blocks.
func (l *Loop) Kick() {
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// Run makes passes until ctx ends.
func (l *Loop) Run(ctx context.Context) error {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		l.pass(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-l.wake:
		case <-t.C:
		}
	}
}

// pass starts one goroutine for each session with due work that has no
// delivery under way. Sessions do not wait for each other: one slow
// session start must not hold up the inbox of the Super Agent.
func (l *Loop) pass(ctx context.Context) {
	ds, err := l.eng.PendingDeliveries(ctx)
	if err != nil {
		l.log.Warn("delivery: plan failed", "err", err)
		return
	}
	for _, d := range ds {
		l.mu.Lock()
		busy := l.inFlight[d.SessionID]
		l.inFlight[d.SessionID] = true
		l.mu.Unlock()
		if busy {
			continue
		}
		go func() {
			l.deliver(ctx, d)
			l.mu.Lock()
			delete(l.inFlight, d.SessionID)
			l.mu.Unlock()
			// Inputs may have arrived during the delivery.
			l.Kick()
		}()
	}
}

func (l *Loop) deliver(ctx context.Context, d core.Delivery) {
	log := l.log.With("session", d.SessionID, "machine", d.MachineID)
	if d.Ensure != nil {
		ectx, cancel := context.WithTimeout(ctx, ensureTimeout)
		ack, err := l.hub.Send(ectx, d.MachineID, envelope(protocol.TypeSessionEnsure, d.Ensure))
		cancel()
		if err != nil {
			log.Warn("delivery: ensure not sent", "err", err)
			return // the machine went away; the next pass retries
		}
		if !ack.OK {
			log.Warn("delivery: session did not start", "err", ack.Error)
			_ = l.eng.DeliveryFailed(ctx, d.SessionID, "session did not start: "+ack.Error)
			return
		}
	}
	dctx, cancel := context.WithTimeout(ctx, deliverTimeout)
	defer cancel()
	ack, err := l.hub.Send(dctx, d.MachineID, envelope(protocol.TypeSessionDeliver, protocol.SessionDeliver{
		SessionID: string(d.SessionID), Input: d.Input,
	}))
	if err != nil {
		log.Warn("delivery: deliver not sent", "err", err)
		return
	}
	if !ack.OK && strings.Contains(ack.Error, unknownSession) {
		log.Info("delivery: session is gone on the machine; starting it again")
		_ = l.eng.SessionLost(ctx, d.SessionID)
		return
	}
	if !ack.OK {
		log.Warn("delivery: input rejected", "err", ack.Error)
		_ = l.eng.DeliveryFailed(ctx, d.SessionID, "input rejected: "+ack.Error)
		return
	}
	if err := l.eng.Delivered(ctx, d.SessionID, d.InputIDs); err != nil {
		log.Warn("delivery: record failed", "err", err)
	}
}

func envelope(t protocol.MessageType, v any) protocol.Envelope {
	b, _ := json.Marshal(v)
	return protocol.Envelope{Type: t, Payload: b}
}
