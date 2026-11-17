package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
)

var t0 = time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)

// open opens a new database for the test.
func open(t *testing.T) {
	t.Helper()
	if err := Open(context.Background(), filepath.Join(t.TempDir(), "solax.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Close() })
}

func atomic_(t *testing.T, fn func(tx *sql.Tx) error) {
	t.Helper()
	if err := Atomic(context.Background(), fn); err != nil {
		t.Fatal(err)
	}
}

// seed creates an owner, a machine and two agents.
func seed(t *testing.T) (Owner, Agent, Agent) {
	t.Helper()
	ctx := context.Background()
	o := Owner{ID: "own_1", CreatedAt: t0}
	m := Machine{ID: "mch_1", OwnerID: o.ID, Name: "mac", State: MachineApproved, TokenHash: "h1"}
	a := Agent{ID: "agt_a", MachineID: m.ID, Name: "backend", Runtime: ir.KindClaude}
	b := Agent{ID: "agt_b", MachineID: m.ID, Name: "infra", Runtime: ir.KindCodex}
	atomic_(t, func(tx *sql.Tx) error {
		return errors.Join(
			SaveOwner(ctx, tx, o),
			SaveMachine(ctx, tx, m),
			SaveAgent(ctx, tx, a),
			SaveAgent(ctx, tx, b),
		)
	})
	return o, a, b
}

func TestOpenTwiceIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "solax.db")
	for range 2 {
		if err := Open(context.Background(), path); err != nil {
			t.Fatal(err)
		}
		Close()
	}
	if err := Open(context.Background(), ":memory:"); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("in-memory: %v", err)
	}
}

func TestTopology(t *testing.T) {
	open(t)
	ctx := context.Background()
	o, a, _ := seed(t)

	err := Snapshot(ctx, func(tx *sql.Tx) error {
		got, err := GetAgentByName(ctx, tx, "backend")
		if err != nil || got.ID != a.ID || got.Runtime != ir.KindClaude {
			t.Fatalf("AgentByName = %+v, %v", got, err)
		}
		if _, err := GetMachine(ctx, tx, "mch_x"); !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("missing machine: %v", err)
		}
		owners, err := ListOwners(ctx, tx)
		if err != nil || len(owners) != 1 || owners[0].ID != o.ID || !owners[0].CreatedAt.Equal(t0) {
			t.Fatalf("owners = %+v, %v", owners, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// A second agent with the same (machine, name) is a conflict.
	err = Atomic(ctx, func(tx *sql.Tx) error {
		return SaveAgent(ctx, tx, Agent{ID: "agt_dup", MachineID: "mch_1", Name: "backend", Runtime: ir.KindPi})
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("duplicate agent: %v", err)
	}

	// Pairing codes and token hashes.
	pending := Machine{ID: "mch_2", OwnerID: o.ID, Name: "pi", State: MachinePending, TokenHash: "h2",
		PairCode: "ABCD-1234", PairUntil: t0.Add(PairTTL)}
	atomic_(t, func(tx *sql.Tx) error { return SaveMachine(ctx, tx, pending) })
	atomic_(t, func(tx *sql.Tx) error {
		got, err := GetMachineByPairCode(ctx, tx, "ABCD-1234")
		if err != nil || got.ID != pending.ID || !got.PairUntil.Equal(pending.PairUntil) {
			t.Fatalf("ByPairCode = %+v, %v", got, err)
		}
		if _, err := GetMachineByPairCode(ctx, tx, ""); !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("empty code matched: %v", err)
		}
		return nil
	})
	if m, err := GetMachineByTokenHash(ctx, ReadDB, "h2"); err != nil || m.ID != pending.ID {
		t.Fatalf("MachineByTokenHash = %+v, %v", m, err)
	}

	// ListMachines returns the machines of one owner only.
	other := Machine{ID: "mch_3", OwnerID: "own_2", Name: "other", State: MachineApproved, TokenHash: "h3"}
	atomic_(t, func(tx *sql.Tx) error {
		return errors.Join(SaveOwner(ctx, tx, Owner{ID: "own_2", CreatedAt: t0}), SaveMachine(ctx, tx, other))
	})
	if ms, err := ListMachines(ctx, ReadDB, o.ID); err != nil || len(ms) != 2 {
		t.Fatalf("ListMachines = %+v, %v", ms, err)
	}
}

func TestClientTokens(t *testing.T) {
	open(t)
	ctx := context.Background()
	seed(t)
	tok := ClientToken{ID: "cli_1", OwnerID: "own_1", Name: "desk", Hash: "x", CreatedAt: t0}
	if err := SaveClient(ctx, WriteDB, tok); err != nil {
		t.Fatal(err)
	}
	tok.Revoked, tok.LastUsed = true, t0.Add(time.Minute)
	if err := SaveClient(ctx, WriteDB, tok); err != nil {
		t.Fatal(err)
	}
	got, err := GetClientByHash(ctx, ReadDB, "x")
	if err != nil || !got.Revoked || !got.LastUsed.Equal(tok.LastUsed) {
		t.Fatalf("ClientByHash = %+v, %v", got, err)
	}
	list, err := ListClients(ctx, ReadDB, "own_1")
	if err != nil || len(list) != 1 {
		t.Fatalf("Clients = %+v, %v", list, err)
	}
}

func TestMessageSeqs(t *testing.T) {
	open(t)
	ctx := context.Background()
	o, _, _ := seed(t)

	const n = 40
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			m := Message{ID: NewID(PrefixMessage), OwnerID: o.ID,
				Author: Author{Kind: AuthorHuman, ID: o.ID}, Kind: MessageChat, Text: "hi", CreatedAt: t0}
			if err := Atomic(ctx, func(tx *sql.Tx) error { _, err := AppendMessage(ctx, tx, m); return err }); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()

	err := Snapshot(ctx, func(tx *sql.Tx) error {
		all, err := ListMessages(ctx, tx, o.ID, 0, 0)
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
		tail, err := ListMessages(ctx, tx, o.ID, n-5, 2)
		if err != nil || len(tail) != 2 || tail[0].Seq != n-4 {
			t.Fatalf("Since tail = %+v, %v", tail, err)
		}
		if other, err := ListMessages(ctx, tx, "own_x", 0, 0); err != nil || len(other) != 0 {
			t.Fatalf("other owner = %+v, %v", other, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSessions(t *testing.T) {
	open(t)
	ctx := context.Background()
	_, a, _ := seed(t)
	ses := Session{ID: "ses_1", AgentID: a.ID, JobID: "job_1", State: ir.SessionIdle,
		Ref: &ir.SessionRef{Kind: ir.KindClaude, NativeID: "abc"}, UpdatedAt: t0}
	atomic_(t, func(tx *sql.Tx) error { return SaveSession(ctx, tx, ses) })
	err := Snapshot(ctx, func(tx *sql.Tx) error {
		got, err := GetSessionFor(ctx, tx, a.ID, "job_1")
		if err != nil || got.ID != ses.ID || got.JobID != "job_1" || got.Ref == nil || got.Ref.NativeID != "abc" {
			t.Fatalf("For = %+v, %v", got, err)
		}
		on, err := ListSessionsOnMachine(ctx, tx, "mch_1")
		if err != nil || len(on) != 1 {
			t.Fatalf("OnMachine = %+v, %v", on, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Migration 0003 keeps the Super Agent conversation and the job sessions
// of a v2 database, and drops the task model.
func TestMigrateDropsTasks(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "solax.db")
	db, err := sql.Open("sqlite", dsn(path, "immediate", false))
	if err != nil {
		t.Fatal(err)
	}
	ms, err := migrations()
	if err != nil {
		t.Fatal(err)
	}
	stmts := []string{`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL)`}
	for _, m := range ms[:2] {
		stmts = append(stmts, m.sql, fmt.Sprintf(`INSERT INTO schema_migrations VALUES (%d, '%s', 0)`, m.version, m.name))
	}
	stmts = append(stmts,
		`INSERT INTO owners(id, super_seq, created_at) VALUES ('own_1', 1, 0)`,
		`INSERT INTO machines(id, owner_id, name, state, token_hash) VALUES ('mch_1', 'own_1', 'mac', 'approved', 'h')`,
		`INSERT INTO agents(id, machine_id, name, driver) VALUES ('agt_a', 'mch_1', 'backend', 'claude')`,
		`INSERT INTO workspaces(id, owner_id, name, created_at) VALUES ('ws_1', 'own_1', 'api', 0)`,
		`INSERT INTO members(workspace_id, agent_id, role) VALUES ('ws_1', 'agt_a', 'leader')`,
		`INSERT INTO messages(id, owner_id, workspace_id, task_id, seq, author_kind, kind, text, created_at)
			VALUES ('msg_s', 'own_1', '', '', 1, 'human', 'chat', 'hello super', 0),
			       ('msg_t', 'own_1', 'ws_1', 'tsk_1', 1, 'agent', 'chat', 'task talk', 0)`,
		`INSERT INTO sessions(id, agent_id, task_id, state, updated_at) VALUES ('ses_1', 'agt_a', 'job_1', 'idle', 0)`,
	)
	for _, q := range stmts {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	db.Close()

	if err := Open(ctx, path); err != nil {
		t.Fatal(err)
	}
	defer Close()
	err = Snapshot(ctx, func(tx *sql.Tx) error {
		msgs, err := ListMessages(ctx, tx, "own_1", 0, 0)
		if err != nil || len(msgs) != 1 || msgs[0].Text != "hello super" {
			t.Fatalf("messages = %+v, %v", msgs, err)
		}
		ses, err := GetSessionFor(ctx, tx, "agt_a", "job_1")
		if err != nil || ses.ID != "ses_1" {
			t.Fatalf("session = %+v, %v", ses, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The next message continues the owner sequence.
	atomic_(t, func(tx *sql.Tx) error {
		m, err := AppendMessage(ctx, tx, Message{ID: "msg_2", OwnerID: "own_1",
			Author: Author{Kind: AuthorHuman}, Kind: MessageChat, Text: "again", CreatedAt: t0})
		if err != nil || m.Seq != 2 {
			t.Fatalf("Append = %+v, %v", m, err)
		}
		return nil
	})
}

func TestAtomicRollsBackAndSnapshotIsReadOnly(t *testing.T) {
	open(t)
	ctx := context.Background()
	boom := errors.New("boom")
	err := Atomic(ctx, func(tx *sql.Tx) error {
		if err := SaveOwner(ctx, tx, Owner{ID: "own_x", CreatedAt: t0}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Atomic = %v", err)
	}
	err = Snapshot(ctx, func(tx *sql.Tx) error {
		if owners, err := ListOwners(ctx, tx); err != nil || len(owners) != 0 {
			t.Fatalf("rolled back owner exists: %+v, %v", owners, err)
		}
		return SaveOwner(ctx, tx, Owner{ID: "own_y", CreatedAt: t0})
	})
	if err == nil {
		t.Fatal("write in Snapshot succeeded")
	}
}
