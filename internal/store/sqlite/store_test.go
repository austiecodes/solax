package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/austiecodes/solax/internal/auth"
	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
)

var t0 = time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "solax.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func atomic_(t *testing.T, s *Store, fn func(tx core.Tx) error) {
	t.Helper()
	if err := s.Atomic(context.Background(), fn); err != nil {
		t.Fatal(err)
	}
}

// seed creates an owner, a machine, two agents and a workspace.
func seed(t *testing.T, s *Store) (core.Owner, core.Workspace, core.Agent, core.Agent) {
	t.Helper()
	ctx := context.Background()
	o := core.Owner{ID: "own_1", CreatedAt: t0}
	m := core.Machine{ID: "mch_1", OwnerID: o.ID, Name: "mac", State: core.MachineApproved, TokenHash: "h1"}
	a := core.Agent{ID: "agt_a", MachineID: m.ID, Name: "backend", Driver: ir.KindClaude}
	b := core.Agent{ID: "agt_b", MachineID: m.ID, Name: "infra", Driver: ir.KindCodex}
	w := core.Workspace{ID: "ws_1", OwnerID: o.ID, Name: "api", LeaderID: a.ID, CreatedAt: t0}
	atomic_(t, s, func(tx core.Tx) error {
		return errors.Join(
			tx.Owners().Save(ctx, o),
			tx.Machines().Save(ctx, m),
			tx.Agents().Save(ctx, a),
			tx.Agents().Save(ctx, b),
			tx.Workspaces().Save(ctx, w),
			tx.Workspaces().SaveMember(ctx, core.Member{WorkspaceID: w.ID, AgentID: a.ID, Role: core.MemberLeader}),
			tx.Workspaces().SaveMember(ctx, core.Member{WorkspaceID: w.ID, AgentID: b.ID, Role: core.MemberPlain}),
		)
	})
	return o, w, a, b
}

func TestOpenTwiceIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "solax.db")
	for range 2 {
		s, err := Open(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
	}
	if _, err := Open(context.Background(), ":memory:"); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("in-memory: %v", err)
	}
}

func TestTopology(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	o, w, a, _ := seed(t, s)

	err := s.Read(ctx, func(tx core.Tx) error {
		got, err := tx.Agents().ByName(ctx, "mch_1", "backend")
		if err != nil || got.ID != a.ID || got.Driver != ir.KindClaude {
			t.Fatalf("ByName = %+v, %v", got, err)
		}
		ms, err := tx.Workspaces().Members(ctx, w.ID)
		if err != nil || len(ms) != 2 || ms[0].Role != core.MemberLeader {
			t.Fatalf("Members = %+v, %v", ms, err)
		}
		if _, err := tx.Machines().Get(ctx, "mch_x"); !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("missing machine: %v", err)
		}
		owners, err := tx.Owners().List(ctx)
		if err != nil || len(owners) != 1 || owners[0].ID != o.ID || !owners[0].CreatedAt.Equal(t0) {
			t.Fatalf("owners = %+v, %v", owners, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// A second agent with the same (machine, name) is a conflict.
	err = s.Atomic(ctx, func(tx core.Tx) error {
		return tx.Agents().Save(ctx, core.Agent{ID: "agt_dup", MachineID: "mch_1", Name: "backend", Driver: ir.KindPi})
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("duplicate agent: %v", err)
	}

	// Pairing codes and token hashes.
	pending := core.Machine{ID: "mch_2", OwnerID: o.ID, Name: "pi", State: core.MachinePending, TokenHash: "h2",
		PairCode: "ABCD-1234", PairUntil: t0.Add(core.PairTTL)}
	atomic_(t, s, func(tx core.Tx) error { return tx.Machines().Save(ctx, pending) })
	atomic_(t, s, func(tx core.Tx) error {
		got, err := tx.Machines().ByPairCode(ctx, "ABCD-1234")
		if err != nil || got.ID != pending.ID || !got.PairUntil.Equal(pending.PairUntil) {
			t.Fatalf("ByPairCode = %+v, %v", got, err)
		}
		if _, err := tx.Machines().ByPairCode(ctx, ""); !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("empty code matched: %v", err)
		}
		return nil
	})
	if m, err := s.MachineByTokenHash(ctx, "h2"); err != nil || m.ID != pending.ID {
		t.Fatalf("MachineByTokenHash = %+v, %v", m, err)
	}

	atomic_(t, s, func(tx core.Tx) error { return tx.Workspaces().DeleteMember(ctx, w.ID, "agt_b") })
	err = s.Atomic(ctx, func(tx core.Tx) error { return tx.Workspaces().DeleteMember(ctx, w.ID, "agt_b") })
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("delete missing member: %v", err)
	}
}

func TestClientTokens(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	seed(t, s)
	tok := auth.ClientToken{ID: "cli_1", OwnerID: "own_1", Name: "desk", Hash: "x", CreatedAt: t0}
	if err := s.SaveClient(ctx, tok); err != nil {
		t.Fatal(err)
	}
	tok.Revoked, tok.LastUsed = true, t0.Add(time.Minute)
	if err := s.SaveClient(ctx, tok); err != nil {
		t.Fatal(err)
	}
	got, err := s.ClientByHash(ctx, "x")
	if err != nil || !got.Revoked || !got.LastUsed.Equal(tok.LastUsed) {
		t.Fatalf("ClientByHash = %+v, %v", got, err)
	}
	list, err := s.Clients(ctx, "own_1")
	if err != nil || len(list) != 1 {
		t.Fatalf("Clients = %+v, %v", list, err)
	}
}

func TestTaskRevisionAndSpent(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	_, w, _, _ := seed(t, s)
	task := core.Task{ID: "tsk_1", WorkspaceID: w.ID, Title: "t", Brief: "b", State: core.TaskRunning,
		Budget: core.DefaultBudget, OrchState: json.RawMessage(`{"n":1}`), Orchestrator: "team", CreatedAt: t0, UpdatedAt: t0}
	child := core.Task{ID: "tsk_2", WorkspaceID: w.ID, ParentID: task.ID, ParentNode: "api#1", Depth: 1,
		TargetAgentID: "agt_b", State: core.TaskDone, CreatedAt: t0.Add(time.Second), UpdatedAt: t0}
	general := core.Task{ID: "tsk_g", WorkspaceID: w.ID, General: true, State: core.TaskRunning, CreatedAt: t0, UpdatedAt: t0}
	atomic_(t, s, func(tx core.Tx) error {
		return errors.Join(tx.Tasks().Create(ctx, task), tx.Tasks().Create(ctx, child), tx.Tasks().Create(ctx, general))
	})

	// Only one general task per workspace.
	err := s.Atomic(ctx, func(tx core.Tx) error {
		return tx.Tasks().Create(ctx, core.Task{ID: "tsk_g2", WorkspaceID: w.ID, General: true, State: core.TaskRunning})
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("second general task: %v", err)
	}

	// Spent does not move the rev.
	atomic_(t, s, func(tx core.Tx) error {
		return tx.Tasks().SetSpent(ctx, task.ID, core.Spent{AgentTurns: 3, HopsWithoutHuman: 2})
	})

	// Compare-and-set on rev.
	next := task
	next.State, next.Rev, next.OrchLastSeq = core.TaskBlocked, 1, 7
	next.BlockedReason = core.BlockedAsk
	atomic_(t, s, func(tx core.Tx) error { return tx.Tasks().Update(ctx, next, 0) })
	stale := task
	stale.State, stale.Rev = core.TaskDone, 1
	err = s.Atomic(ctx, func(tx core.Tx) error { return tx.Tasks().Update(ctx, stale, 0) })
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("stale update: %v", err)
	}
	err = s.Atomic(ctx, func(tx core.Tx) error { return tx.Tasks().Update(ctx, core.Task{ID: "tsk_x"}, 0) })
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("missing task update: %v", err)
	}

	err = s.Read(ctx, func(tx core.Tx) error {
		got, err := tx.Tasks().Get(ctx, task.ID)
		if err != nil {
			return err
		}
		if got.Rev != 1 || got.State != core.TaskBlocked || got.BlockedReason != core.BlockedAsk || got.OrchLastSeq != 7 ||
			got.Spent.AgentTurns != 3 || got.Budget != core.DefaultBudget || string(got.OrchState) != `{"n":1}` {
			t.Fatalf("task = %+v", got)
		}
		open, err := tx.Tasks().Open(ctx, w.ID)
		if err != nil || len(open) != 2 { // the child is done
			t.Fatalf("Open = %d tasks, %v", len(open), err)
		}
		kids, err := tx.Tasks().Children(ctx, task.ID)
		if err != nil || len(kids) != 1 || kids[0].ParentNode != "api#1" || kids[0].TargetAgentID != "agt_b" {
			t.Fatalf("Children = %+v, %v", kids, err)
		}
		g, err := tx.Tasks().General(ctx, w.ID)
		if err != nil || g.ID != general.ID {
			t.Fatalf("General = %+v, %v", g, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMessageSeqsPerStream(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	o, w, a, b := seed(t, s)

	const n = 40
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m := core.Message{ID: core.NewID(core.PrefixMessage), OwnerID: o.ID, WorkspaceID: w.ID, TaskID: "tsk_1",
				Author: core.Author{Kind: core.AuthorAgent, ID: a.ID}, Kind: core.MessageChat, Text: "hi", CreatedAt: t0}
			if i%2 == 0 {
				m.Mentions = []core.ID{b.ID}
			}
			if err := s.Atomic(ctx, func(tx core.Tx) error { _, err := tx.Messages().Append(ctx, m); return err }); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	superMsg := core.Message{ID: "msg_s", OwnerID: o.ID, Author: core.Author{Kind: core.AuthorHuman, ID: o.ID},
		Kind: core.MessageChat, Text: "hello super", CreatedAt: t0}
	atomic_(t, s, func(tx core.Tx) error {
		got, err := tx.Messages().Append(ctx, superMsg)
		if err != nil || got.Seq != 1 {
			t.Fatalf("super Append = %+v, %v", got, err)
		}
		return nil
	})

	err := s.Read(ctx, func(tx core.Tx) error {
		all, err := tx.Messages().Since(ctx, "tsk_1", 0, 0)
		if err != nil {
			return err
		}
		if len(all) != n {
			t.Fatalf("Since = %d messages", len(all))
		}
		for i, m := range all {
			if m.Seq != int64(i+1) {
				t.Fatalf("message %d has seq %d", i, m.Seq)
			}
		}
		tail, err := tx.Messages().Since(ctx, "tsk_1", n-5, 2)
		if err != nil || len(tail) != 2 || tail[0].Seq != n-4 {
			t.Fatalf("Since tail = %+v, %v", tail, err)
		}
		sup, err := tx.Messages().SuperSince(ctx, o.ID, 0, 0)
		if err != nil || len(sup) != 1 || sup[0].Text != "hello super" || sup[0].TaskID != "" {
			t.Fatalf("SuperSince = %+v, %v", sup, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTaskEvents(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	atomic_(t, s, func(tx core.Tx) error {
		for i := range 5 {
			e, err := tx.TaskEvents().Append(ctx, core.TaskEvent{TaskID: "tsk_1", Kind: core.EventMessage,
				Payload: json.RawMessage(`{"text":"0123456789"}`), At: t0.Add(time.Duration(i) * time.Second)})
			if err != nil {
				return err
			}
			if e.Seq != int64(i+1) {
				t.Fatalf("seq = %d, want %d", e.Seq, i+1)
			}
		}
		_, err := tx.TaskEvents().Append(ctx, core.TaskEvent{TaskID: "tsk_2", Kind: core.EventTaskStarted})
		return err
	})
	err := s.Atomic(ctx, func(tx core.Tx) error {
		_, err := tx.TaskEvents().Append(ctx, core.TaskEvent{TaskID: "tsk_1", Kind: core.EventMessage, Payload: json.RawMessage(`{`)})
		return err
	})
	if !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("invalid payload: %v", err)
	}

	err = s.Read(ctx, func(tx core.Tx) error {
		if last, err := tx.TaskEvents().Last(ctx, "tsk_1"); err != nil || last != 5 {
			t.Fatalf("Last = %d, %v", last, err)
		}
		if last, err := tx.TaskEvents().Last(ctx, "tsk_2"); err != nil || last != 1 {
			t.Fatalf("Last other task = %d, %v", last, err)
		}
		evs, err := tx.TaskEvents().Since(ctx, "tsk_1", 1, 2, 0)
		if err != nil || len(evs) != 2 || evs[0].Seq != 2 || !evs[0].At.Equal(t0.Add(time.Second)) {
			t.Fatalf("Since limit = %+v, %v", evs, err)
		}
		// Each payload is 21 bytes: 50 bytes fit two.
		evs, err = tx.TaskEvents().Since(ctx, "tsk_1", 0, 100, 50)
		if err != nil || len(evs) != 2 {
			t.Fatalf("Since maxBytes = %d events, %v", len(evs), err)
		}
		// One oversized event still comes back alone.
		evs, err = tx.TaskEvents().Since(ctx, "tsk_1", 0, 100, 1)
		if err != nil || len(evs) != 1 {
			t.Fatalf("Since tiny maxBytes = %d events, %v", len(evs), err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSessionsAndCursors(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	_, _, a, _ := seed(t, s)
	ses := core.Session{ID: "ses_1", AgentID: a.ID, TaskID: "tsk_1", State: ir.SessionIdle,
		Ref: &ir.SessionRef{Kind: ir.KindClaude, NativeID: "abc"}, UpdatedAt: t0}
	atomic_(t, s, func(tx core.Tx) error {
		return errors.Join(
			tx.Sessions().Save(ctx, ses),
			tx.Cursors().Save(ctx, core.Cursor{SessionID: ses.ID, TaskID: "tsk_1", DeliveredSeq: 10, ReadSeq: 4}),
			// A late, lower ack does not rewind.
			tx.Cursors().Save(ctx, core.Cursor{SessionID: ses.ID, TaskID: "tsk_1", DeliveredSeq: 8, ReadSeq: 6}),
		)
	})
	err := s.Read(ctx, func(tx core.Tx) error {
		got, err := tx.Sessions().For(ctx, a.ID, "tsk_1")
		if err != nil || got.ID != ses.ID || got.Ref == nil || got.Ref.NativeID != "abc" {
			t.Fatalf("For = %+v, %v", got, err)
		}
		on, err := tx.Sessions().OnMachine(ctx, "mch_1")
		if err != nil || len(on) != 1 {
			t.Fatalf("OnMachine = %+v, %v", on, err)
		}
		c, err := tx.Cursors().Get(ctx, ses.ID, "tsk_1")
		if err != nil || c.DeliveredSeq != 10 || c.ReadSeq != 6 {
			t.Fatalf("cursor = %+v, %v", c, err)
		}
		zero, err := tx.Cursors().Get(ctx, ses.ID, "tsk_none")
		if err != nil || zero.DeliveredSeq != 0 || zero.TaskID != "tsk_none" {
			t.Fatalf("zero cursor = %+v, %v", zero, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func claim(t *testing.T, s *Store, now time.Time) []core.Effect {
	t.Helper()
	var out []core.Effect
	atomic_(t, s, func(tx core.Tx) error {
		var err error
		out, err = tx.Effects().ClaimDue(context.Background(), "deliver", now, 10)
		return err
	})
	return out
}

func TestEffectGenerationKeepsLateSignal(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	var kicks atomic.Int32
	s.OnEffects(func() { kicks.Add(1) })

	atomic_(t, s, func(tx core.Tx) error { return tx.Effects().Enqueue(ctx, "deliver", "ses_1", nil) })
	if kicks.Load() != 1 {
		t.Fatalf("kicks = %d", kicks.Load())
	}
	got := claim(t, s, t0)
	if len(got) != 1 || got[0].Generation != 1 || got[0].ClaimToken == "" {
		t.Fatalf("claim = %+v", got)
	}
	if again := claim(t, s, t0); len(again) != 0 {
		t.Fatalf("claimed twice: %+v", again)
	}

	// A signal arrives while the effect runs.
	atomic_(t, s, func(tx core.Tx) error {
		return tx.Effects().Enqueue(ctx, "deliver", "ses_1", json.RawMessage(`{"n":2}`))
	})

	var removed bool
	atomic_(t, s, func(tx core.Tx) error {
		var err error
		removed, err = tx.Effects().Complete(ctx, got[0])
		return err
	})
	if removed {
		t.Fatal("Complete removed an effect that was signalled during the run")
	}
	again := claim(t, s, t0)
	if len(again) != 1 || again[0].Generation != 2 || string(again[0].Payload) != `{"n":2}` {
		t.Fatalf("requeued claim = %+v", again)
	}
	atomic_(t, s, func(tx core.Tx) error {
		var err error
		removed, err = tx.Effects().Complete(ctx, again[0])
		return err
	})
	if !removed {
		t.Fatal("Complete kept a finished effect")
	}
	if left := claim(t, s, t0.Add(time.Hour)); len(left) != 0 {
		t.Fatalf("left over: %+v", left)
	}
}

func TestEffectLostClaim(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	atomic_(t, s, func(tx core.Tx) error { return tx.Effects().Enqueue(ctx, "deliver", "ses_1", nil) })
	first := claim(t, s, t0)[0]

	// The claim expires and a second worker takes the effect.
	second := claim(t, s, t0.Add(core.ClaimTTL+time.Second))
	if len(second) != 1 || second[0].ClaimToken == first.ClaimToken {
		t.Fatalf("reclaim = %+v", second)
	}
	atomic_(t, s, func(tx core.Tx) error {
		if ok, err := tx.Effects().Holds(ctx, first); err != nil || ok {
			t.Fatalf("stale worker still holds: %v %v", ok, err)
		}
		if ok, err := tx.Effects().Holds(ctx, second[0]); err != nil || !ok {
			t.Fatalf("new worker does not hold: %v %v", ok, err)
		}
		// The stale worker can neither complete nor reschedule.
		if removed, err := tx.Effects().Complete(ctx, first); err != nil || removed {
			t.Fatalf("stale Complete = %v, %v", removed, err)
		}
		return tx.Effects().Reschedule(ctx, first, t0.Add(time.Hour), 9, "stale")
	})
	if got := claim(t, s, t0.Add(core.ClaimTTL+2*time.Second)); len(got) != 0 {
		t.Fatalf("stale worker released the new claim: %+v", got)
	}

	// The new worker fails and backs off.
	atomic_(t, s, func(tx core.Tx) error {
		return tx.Effects().Reschedule(ctx, second[0], t0.Add(time.Hour), 1, "boom")
	})
	if got := claim(t, s, t0.Add(10*time.Minute)); len(got) != 0 {
		t.Fatalf("ran before backoff: %+v", got)
	}
	got := claim(t, s, t0.Add(2*time.Hour))
	if len(got) != 1 || got[0].Attempts != 1 {
		t.Fatalf("after backoff = %+v", got)
	}

	atomic_(t, s, func(tx core.Tx) error { return tx.Effects().Cancel(ctx, "deliver", "ses_1") })
	if got := claim(t, s, t0.Add(5*time.Hour)); len(got) != 0 {
		t.Fatalf("cancelled effect ran: %+v", got)
	}
}

func TestEnqueueAtDelays(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	atomic_(t, s, func(tx core.Tx) error {
		return tx.Effects().EnqueueAt(ctx, "deliver", "timer", nil, t0.Add(time.Minute))
	})
	if got := claim(t, s, t0); len(got) != 0 {
		t.Fatalf("timer ran early: %+v", got)
	}
	if got := claim(t, s, t0.Add(time.Minute)); len(got) != 1 {
		t.Fatalf("timer did not run: %+v", got)
	}
}

func TestAtomicRollsBackAndReadIsReadOnly(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	boom := errors.New("boom")
	err := s.Atomic(ctx, func(tx core.Tx) error {
		if err := tx.Owners().Save(ctx, core.Owner{ID: "own_x", CreatedAt: t0}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Atomic = %v", err)
	}
	err = s.Read(ctx, func(tx core.Tx) error {
		if _, err := tx.Owners().Get(ctx, "own_x"); !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("rolled back owner exists: %v", err)
		}
		return tx.Owners().Save(ctx, core.Owner{ID: "own_y", CreatedAt: t0})
	})
	if err == nil {
		t.Fatal("write in Read succeeded")
	}
}
