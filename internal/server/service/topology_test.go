package service_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/internal/server/service"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// fakeClock is a settable test clock.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time      { return c.t }
func (c *fakeClock) add(d time.Duration) { c.t = c.t.Add(d) }

// stub replaces a service function for one test.
func stub[T any](t *testing.T, v *T, fn T) {
	old := *v
	*v = fn
	t.Cleanup(func() { *v = old })
}

type env struct {
	clock        *fakeClock
	disconnected []model.ID // machines that service.Disconnect closed
	owner        model.Owner
	human        model.Actor
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	if err := model.Open(ctx, filepath.Join(t.TempDir(), "solax.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { model.Close() })
	e := &env{clock: &fakeClock{t: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)}}
	stub(t, &service.Now, e.clock.now)
	stub(t, &service.Disconnect, func(id model.ID) { e.disconnected = append(e.disconnected, id) })
	var err error
	if e.owner, err = service.InitOwner(ctx); err != nil {
		t.Fatal(err)
	}
	e.human = model.Actor{Kind: model.AuthorHuman, ID: "cli_1", OwnerID: e.owner.ID}
	return e
}

func hash(c byte) string { return strings.Repeat(string(c), 64) }

// machine registers an approved machine with agents.
// agentDef is an agent to set up on a test machine.
type agentDef struct {
	Name    string
	Runtime ir.Kind
	Role    string
}

func (e *env) machine(t *testing.T, name string, h byte, agents ...agentDef) model.Machine {
	t.Helper()
	ctx := context.Background()
	m, err := service.RegisterMachine(ctx, e.human, name, hash(h))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.MachineConnected(ctx, m.ID, protocol.Hello{MachineID: string(m.ID), FreeSlots: 3}); err != nil {
		t.Fatal(err)
	}
	for _, a := range agents {
		_, err := service.CreateAgent(ctx, e.human, service.AgentSpec{
			Machine: string(m.ID), Name: a.Name, Runtime: a.Runtime, Super: a.Role == "super",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func is(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestInitOwnerOnce(t *testing.T) {
	newEnv(t)
	_, err := service.InitOwner(context.Background())
	is(t, err, errs.ErrConflict)
}

func TestEnrollApproveRevoke(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	m, err := service.EnrollMachine(ctx, "pi5", hash('a'))
	if err != nil {
		t.Fatal(err)
	}
	if m.State != model.MachinePending || len(m.PairCode) != 9 || m.OwnerID != e.owner.ID {
		t.Fatalf("enrolled = %+v", m)
	}
	// A retry keeps the identity and the code.
	again, err := service.EnrollMachine(ctx, "pi5", hash('a'))
	if err != nil || again.ID != m.ID || again.PairCode != m.PairCode {
		t.Fatalf("retry = %+v, %v", again, err)
	}

	// Only a person approves; codes are case- and dash-insensitive.
	agent := model.Actor{Kind: model.AuthorSuper, ID: "agt_s", OwnerID: e.owner.ID}
	_, err = service.ApproveMachine(ctx, agent, m.PairCode)
	is(t, err, errs.ErrForbidden)
	loose := strings.ToLower(strings.ReplaceAll(m.PairCode, "-", ""))
	approved, err := service.ApproveMachine(ctx, e.human, loose)
	if err != nil || approved.State != model.MachineApproved || approved.PairCode != "" {
		t.Fatalf("approve = %+v, %v", approved, err)
	}
	_, err = service.ApproveMachine(ctx, e.human, m.PairCode)
	is(t, err, errs.ErrNotFound) // the code is used up

	// Another owner cannot see it.
	stranger := model.Actor{Kind: model.AuthorHuman, ID: "cli_x", OwnerID: "own_other"}
	is(t, service.RevokeMachine(ctx, stranger, m.ID), errs.ErrNotFound)

	if err := service.RevokeMachine(ctx, e.human, m.ID); err != nil {
		t.Fatal(err)
	}
	if len(e.disconnected) != 1 || e.disconnected[0] != m.ID {
		t.Fatalf("disconnected = %v", e.disconnected)
	}
	// A revoked machine stays revoked, even when it enrolls again.
	back, err := service.EnrollMachine(ctx, "pi5", hash('a'))
	if err != nil || back.State != model.MachineRevoked {
		t.Fatalf("re-enroll after revoke = %+v, %v", back, err)
	}
	is(t, service.MachineConnected(ctx, m.ID, protocol.Hello{}), errs.ErrForbidden)
}

func TestPairCodeExpires(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	m, err := service.EnrollMachine(ctx, "pi5", hash('a'))
	if err != nil {
		t.Fatal(err)
	}
	e.clock.add(model.PairTTL + time.Second)
	_, err = service.ApproveMachine(ctx, e.human, m.PairCode)
	is(t, err, errs.ErrNotFound)
	// Enrolling again issues a fresh code.
	fresh, err := service.EnrollMachine(ctx, "pi5", hash('a'))
	if err != nil || fresh.PairCode == m.PairCode || fresh.ID != m.ID {
		t.Fatalf("fresh code = %+v, %v", fresh, err)
	}
	if _, err := service.ApproveMachine(ctx, e.human, fresh.PairCode); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollBeforeInit(t *testing.T) {
	if err := model.Open(context.Background(), filepath.Join(t.TempDir(), "x.db")); err != nil {
		t.Fatal(err)
	}
	defer model.Close()
	_, err := service.EnrollMachine(context.Background(), "m", hash('a'))
	is(t, err, errs.ErrUnavailable)
}

// A hello stores the detected runtimes. An agent is online only when its
// machine is connected and has a working CLI for its runtime. Agents stay
// across hellos: the server defines them, not the daemon.
func TestMachineConnectedStoresRuntimes(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	m := e.machine(t, "mac", 'a',
		agentDef{Name: "backend", Runtime: ir.KindClaude},
		agentDef{Name: "super", Runtime: ir.KindPi, Role: "super"})
	err := service.MachineConnected(ctx, m.ID, protocol.Hello{FreeSlots: 3, Runtimes: []protocol.RuntimeProbe{
		{Kind: ir.KindClaude, OK: true, Version: "2.1"}, {Kind: ir.KindPi, OK: false, Problem: "not found"}}})
	if err != nil {
		t.Fatal(err)
	}
	agents, err := service.Agents(ctx, e.human)
	if err != nil || len(agents) != 2 {
		t.Fatalf("agents = %+v, %v", agents, err)
	}
	online := map[string]bool{}
	for _, a := range agents {
		online[a.Name] = a.Online
	}
	if !online["backend"] || online["super"] {
		t.Fatalf("online = %v, want backend only", online)
	}
	// Names are unique across machines.
	other, err := service.RegisterMachine(ctx, e.human, "linux", hash('b'))
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateAgent(ctx, e.human, service.AgentSpec{Machine: "linux", Name: "backend", Runtime: ir.KindCodex})
	is(t, err, errs.ErrConflict)
	machines, _ := service.Machines(ctx, e.human)
	if len(machines) != 2 || other.Online {
		t.Fatalf("machines = %+v", machines)
	}
	if mac := machines[1]; mac.Name != "mac" || !mac.Online || mac.Slots != 3 {
		t.Fatalf("machines = %+v", machines)
	}
	if err := service.MachineDisconnected(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	machines, _ = service.Machines(ctx, e.human)
	if machines[0].Online {
		t.Fatal("still online after disconnect")
	}
}

// A built-in runtime needs no report; a fork of Claude Code from the
// config of a daemon must be one that the machine reported.
func TestCreateAgentRuntimes(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	m, err := service.RegisterMachine(ctx, e.human, "mac", hash('a'))
	if err != nil {
		t.Fatal(err)
	}
	add := func(name string, k ir.Kind) error {
		_, err := service.CreateAgent(ctx, e.human, service.AgentSpec{Machine: "mac", Name: name, Runtime: k})
		return err
	}
	if err := add("cc", ir.KindClaude); err != nil {
		t.Fatalf("claude before any report: %v", err)
	}
	is(t, add("fork", "mycc"), errs.ErrInvalid)
	is(t, add("bad", "My CC"), errs.ErrInvalid)
	err = service.MachineConnected(ctx, m.ID, protocol.Hello{Runtimes: []protocol.RuntimeProbe{{Kind: "mycc", OK: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := add("fork", "mycc"); err != nil {
		t.Fatalf("mycc after the report: %v", err)
	}
}
func TestScopesAndRecall(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	m := e.machine(t, "mac", 'a',
		agentDef{Name: "backend", Runtime: ir.KindClaude},
		agentDef{Name: "router", Runtime: ir.KindPi, Role: "super"})
	views, err := service.Agents(ctx, e.human)
	if err != nil || len(views) != 2 {
		t.Fatalf("agents = %+v, %v", views, err)
	}
	var backend service.AgentView
	for _, v := range views {
		if v.Name == "backend" {
			backend = v
		}
	}

	// A worker agent cannot manage or dispatch.
	worker := model.Actor{Kind: model.AuthorAgent, ID: backend.ID, OwnerID: e.owner.ID}
	_, err = service.Machines(ctx, worker)
	is(t, err, errs.ErrForbidden)
	_, err = service.Dispatch(ctx, worker, "backend", "", "x")
	is(t, err, errs.ErrForbidden)
	_, err = service.Tasks(ctx, worker, 0)
	is(t, err, errs.ErrForbidden)

	msg, err := service.SendToSuper(ctx, e.human, "hello")
	if err != nil {
		t.Fatal(err)
	}
	obj, err := service.Recall(ctx, e.human, msg.ID)
	if err != nil || obj.Type != "message" || !strings.Contains(obj.Summary, "hello") {
		t.Fatalf("recall message = %+v, %v", obj, err)
	}
	// The conversation is hidden from workers and other owners.
	_, err = service.Recall(ctx, worker, msg.ID)
	is(t, err, errs.ErrNotFound)
	stranger := model.Actor{Kind: model.AuthorHuman, OwnerID: "own_other"}
	_, err = service.Recall(ctx, stranger, msg.ID)
	is(t, err, errs.ErrNotFound)
	_, err = service.Recall(ctx, stranger, m.ID)
	is(t, err, errs.ErrNotFound)

	obj, err = service.Recall(ctx, e.human, backend.ID)
	if err != nil || obj.Type != "agent" || !strings.Contains(obj.Summary, "backend@mac") || obj.Links["machine"] != string(m.ID) {
		t.Fatalf("recall agent = %+v, %v", obj, err)
	}
	obj, err = service.Recall(ctx, e.human, m.ID)
	if err != nil || obj.Type != "machine" {
		t.Fatalf("recall machine = %+v, %v", obj, err)
	}
	_, err = service.Recall(ctx, e.human, "zzz_1")
	is(t, err, errs.ErrNotFound)
}
