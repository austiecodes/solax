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
	ses := Session{ID: "ses_1", AgentID: a.ID, TaskID: "tsk_1", State: ir.SessionIdle,
		Ref: &ir.SessionRef{Kind: ir.KindClaude, NativeID: "abc"}, UpdatedAt: t0}
	atomic_(t, func(tx *sql.Tx) error { return SaveSession(ctx, tx, ses) })
	err := Snapshot(ctx, func(tx *sql.Tx) error {
		got, err := GetSessionFor(ctx, tx, a.ID, "tsk_1")
		if err != nil || got.ID != ses.ID || got.TaskID != "tsk_1" || got.Ref == nil || got.Ref.NativeID != "abc" {
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

// Migration 0003 keeps the Super Agent conversation and the task sessions
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
		`INSERT INTO sessions(id, agent_id, task_id, state, updated_at) VALUES ('ses_1', 'agt_a', 'tsk_1', 'idle', 0)`,
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
		ses, err := GetSessionFor(ctx, tx, "agt_a", "tsk_1")
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

// A session whose unsent inputs are all notes does not wake; the notes go
// with the next waking input, in order. A task keeps who owns its turn.
func TestNotesWaitForAWakingInput(t *testing.T) {
	open(t)
	o, a, _ := seed(t)
	ctx := context.Background()
	add := func(id ID, mode ir.InputMode, at time.Time) {
		atomic_(t, func(tx *sql.Tx) error {
			return AddInput(ctx, tx, Input{ID: id, SessionID: "ses_1", Mode: mode, Text: string(id), CreatedAt: at})
		})
	}
	add("inp_1", ir.ModeNote, t0)
	if ids, err := ListPendingSessions(ctx, ReadDB); err != nil || len(ids) != 0 {
		t.Fatalf("pending with only a note = %v, %v; want none", ids, err)
	}
	add("inp_2", "", t0.Add(time.Second)) // the default mode wakes
	ids, err := ListPendingSessions(ctx, ReadDB)
	if err != nil || len(ids) != 1 || ids[0] != "ses_1" {
		t.Fatalf("pending = %v, %v; want ses_1", ids, err)
	}
	ins, err := ListPendingInputs(ctx, ReadDB, "ses_1")
	if err != nil || len(ins) != 2 || ins[0].Mode != ir.ModeNote || ins[1].Mode != ir.ModeFollowUp {
		t.Fatalf("inputs = %+v, %v", ins, err)
	}

	j := Task{ID: "tsk_1", OwnerID: o.ID, AgentID: a.ID, SessionID: "ses_1", RequestedBy: AuthorSuper, TurnBy: AuthorSuper,
		State: TaskQueued, CreatedAt: t0, UpdatedAt: t0}
	atomic_(t, func(tx *sql.Tx) error { return SaveTask(ctx, tx, j) })
	j.TurnBy = AuthorHuman
	atomic_(t, func(tx *sql.Tx) error { return SaveTask(ctx, tx, j) })
	got, err := GetTask(ctx, ReadDB, "tsk_1")
	if err != nil || got.RequestedBy != AuthorSuper || got.TurnBy != AuthorHuman {
		t.Fatalf("task = %+v, %v; want requested by super, turn by human", got, err)
	}
}

// Schema v10 makes a project of each folder of the worker agents, and
// links old tasks to it. Folders with the same last name get the machine
// name after it; "~" is "home"; the Super Agent folder makes no project.
func TestMigrateMakesProjects(t *testing.T) {
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
	for _, m := range ms[:9] {
		stmts = append(stmts, m.sql, fmt.Sprintf(`INSERT INTO schema_migrations VALUES (%d, '%s', 0)`, m.version, m.name))
	}
	stmts = append(stmts,
		`INSERT INTO owners(id, super_seq, created_at) VALUES ('own_1', 0, 0)`,
		`INSERT INTO machines(id, owner_id, name, state, token_hash) VALUES
			('mch_1', 'own_1', 'mac', 'approved', 'h1'), ('mch_2', 'own_1', 'dev', 'approved', 'h2')`,
		`INSERT INTO agents(id, machine_id, name, runtime, role, dir, created_at) VALUES
			('agt_000000001aa', 'mch_1', 'router', 'claude', 'super', '~/codes', 1),
			('agt_000000002aa', 'mch_1', 'coder', 'claude', '', '/src/app/', 2),
			('agt_000000003aa', 'mch_1', 'reviewer', 'codex', '', '/src/app', 3),
			('agt_000000004aa', 'mch_2', 'builder', 'codex', '', '/srv/app', 4),
			('agt_000000005aa', 'mch_1', 'helper', 'pi', '', '~', 5)`,
		`INSERT INTO tasks(id, owner_id, agent_id, session_id, prompt, state, created_at, updated_at) VALUES
			('tsk_1', 'own_1', 'agt_000000003aa', 'ses_1', 'review', 'done', 0, 0),
			('tsk_2', 'own_1', 'agt_000000004aa', 'ses_2', 'build', 'done', 0, 0)`,
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
	ps, err := ListProjects(ctx, ReadDB, "own_1", false)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, p := range ps {
		got[p.Name] = string(p.Source.MachineID) + ":" + p.Source.Path
		if p.Source.Kind != SourceLocal || p.ID.Prefix() != PrefixProject {
			t.Errorf("project %+v", p)
		}
	}
	want := map[string]string{"app": "mch_1:/src/app", "app-dev": "mch_2:/srv/app", "home": "mch_1:~"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("projects = %v, want %v", got, want)
	}
	for id, name := range map[ID]string{"tsk_1": "app", "tsk_2": "app-dev"} {
		j, err := GetTask(ctx, ReadDB, id)
		if err != nil {
			t.Fatal(err)
		}
		p, err := GetProject(ctx, ReadDB, j.ProjectID)
		if err != nil || p.Name != name {
			t.Errorf("project of %s = %+v, %v; want %s", id, p, err, name)
		}
	}
}
