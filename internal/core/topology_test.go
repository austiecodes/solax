package core_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/store/sqlite"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// fakeClock is a settable test clock.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time      { return c.t }
func (c *fakeClock) add(d time.Duration) { c.t = c.t.Add(d) }

// fakeDispatcher records disconnects.
type fakeDispatcher struct{ disconnected []core.ID }

func (d *fakeDispatcher) Send(context.Context, core.ID, protocol.Envelope) (protocol.Ack, error) {
	return protocol.Ack{OK: true}, nil
}
func (d *fakeDispatcher) Online(core.ID) bool   { return false }
func (d *fakeDispatcher) Disconnect(id core.ID) { d.disconnected = append(d.disconnected, id) }

type env struct {
	eng   *core.Engine
	store *sqlite.Store
	clock *fakeClock
	disp  *fakeDispatcher
	owner core.Owner
	human core.Actor
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "solax.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	e := &env{store: store, clock: &fakeClock{t: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)}, disp: &fakeDispatcher{}}
	e.eng = core.NewEngine(core.Deps{Store: store, Dispatcher: e.disp, Clock: e.clock.now})
	if e.owner, err = e.eng.InitOwner(ctx); err != nil {
		t.Fatal(err)
	}
	e.human = core.Actor{Kind: core.AuthorHuman, ID: "cli_1", OwnerID: e.owner.ID}
	return e
}

func hash(c byte) string { return strings.Repeat(string(c), 64) }

// machine registers an approved machine with agents.
func (e *env) machine(t *testing.T, name string, h byte, agents ...protocol.AgentInfo) core.Machine {
	t.Helper()
	ctx := context.Background()
	m, err := e.eng.RegisterMachine(ctx, e.human, name, hash(h))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.eng.MachineConnected(ctx, m.ID, protocol.Hello{MachineID: string(m.ID), Agents: agents, FreeSlots: 3}); err != nil {
		t.Fatal(err)
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
	e := newEnv(t)
	_, err := e.eng.InitOwner(context.Background())
	is(t, err, errs.ErrConflict)
}

func TestEnrollApproveRevoke(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	m, err := e.eng.EnrollMachine(ctx, "pi5", hash('a'))
	if err != nil {
		t.Fatal(err)
	}
	if m.State != core.MachinePending || len(m.PairCode) != 9 || m.OwnerID != e.owner.ID {
		t.Fatalf("enrolled = %+v", m)
	}
	// A retry keeps the identity and the code.
	again, err := e.eng.EnrollMachine(ctx, "pi5", hash('a'))
	if err != nil || again.ID != m.ID || again.PairCode != m.PairCode {
		t.Fatalf("retry = %+v, %v", again, err)
	}

	// Only a person approves; codes are case- and dash-insensitive.
	agent := core.Actor{Kind: core.AuthorSuper, ID: "agt_s", OwnerID: e.owner.ID}
	_, err = e.eng.ApproveMachine(ctx, agent, m.PairCode)
	is(t, err, errs.ErrForbidden)
	loose := strings.ToLower(strings.ReplaceAll(m.PairCode, "-", ""))
	approved, err := e.eng.ApproveMachine(ctx, e.human, loose)
	if err != nil || approved.State != core.MachineApproved || approved.PairCode != "" {
		t.Fatalf("approve = %+v, %v", approved, err)
	}
	_, err = e.eng.ApproveMachine(ctx, e.human, m.PairCode)
	is(t, err, errs.ErrNotFound) // the code is used up

	// Another owner cannot see it.
	stranger := core.Actor{Kind: core.AuthorHuman, ID: "cli_x", OwnerID: "own_other"}
	is(t, e.eng.RevokeMachine(ctx, stranger, m.ID), errs.ErrNotFound)

	if err := e.eng.RevokeMachine(ctx, e.human, m.ID); err != nil {
		t.Fatal(err)
	}
	if len(e.disp.disconnected) != 1 || e.disp.disconnected[0] != m.ID {
		t.Fatalf("disconnected = %v", e.disp.disconnected)
	}
	// A revoked machine stays revoked, even when it enrolls again.
	back, err := e.eng.EnrollMachine(ctx, "pi5", hash('a'))
	if err != nil || back.State != core.MachineRevoked {
		t.Fatalf("re-enroll after revoke = %+v, %v", back, err)
	}
	is(t, e.eng.MachineConnected(ctx, m.ID, protocol.Hello{}), errs.ErrForbidden)
}

func TestPairCodeExpires(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	m, err := e.eng.EnrollMachine(ctx, "pi5", hash('a'))
	if err != nil {
		t.Fatal(err)
	}
	e.clock.add(core.PairTTL + time.Second)
	_, err = e.eng.ApproveMachine(ctx, e.human, m.PairCode)
	is(t, err, errs.ErrNotFound)
	// Enrolling again issues a fresh code.
	fresh, err := e.eng.EnrollMachine(ctx, "pi5", hash('a'))
	if err != nil || fresh.PairCode == m.PairCode || fresh.ID != m.ID {
		t.Fatalf("fresh code = %+v, %v", fresh, err)
	}
	if _, err := e.eng.ApproveMachine(ctx, e.human, fresh.PairCode); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollBeforeInit(t *testing.T) {
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = core.NewEngine(core.Deps{Store: store}).EnrollMachine(context.Background(), "m", hash('a'))
	is(t, err, errs.ErrUnavailable)
}

func TestMachineConnectedUpsertsAgents(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	m := e.machine(t, "mac", 'a',
		protocol.AgentInfo{Name: "backend", Driver: ir.KindClaude, Dir: "/api"},
		protocol.AgentInfo{Name: "super", Driver: ir.KindPi, Role: "super"})
	agents, err := e.eng.Agents(ctx, e.human)
	if err != nil || len(agents) != 2 {
		t.Fatalf("agents = %+v, %v", agents, err)
	}
	first := agents[0].ID

	// A second hello updates in place and keeps IDs.
	err = e.eng.MachineConnected(ctx, m.ID, protocol.Hello{FreeSlots: 3, Agents: []protocol.AgentInfo{
		{Name: "backend", Driver: ir.KindCodex, Dir: "/api2"}}})
	if err != nil {
		t.Fatal(err)
	}
	agents, _ = e.eng.Agents(ctx, e.human)
	if len(agents) != 2 || agents[0].ID != first || agents[0].Driver != ir.KindCodex || agents[0].Dir != "/api2" {
		t.Fatalf("after second hello = %+v", agents)
	}
	machines, _ := e.eng.Machines(ctx, e.human)
	if len(machines) != 1 || !machines[0].Online || machines[0].Slots != 3 {
		t.Fatalf("machines = %+v", machines)
	}
	if err := e.eng.MachineDisconnected(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	machines, _ = e.eng.Machines(ctx, e.human)
	if machines[0].Online {
		t.Fatal("still online after disconnect")
	}
}

func TestWorkspacesAndMembers(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.machine(t, "mac", 'a',
		protocol.AgentInfo{Name: "backend", Driver: ir.KindClaude},
		protocol.AgentInfo{Name: "infra", Driver: ir.KindCodex},
		protocol.AgentInfo{Name: "super", Driver: ir.KindPi, Role: "super"})
	e.machine(t, "linux", 'b', protocol.AgentInfo{Name: "infra", Driver: ir.KindPi})

	// Worker agents cannot manage workspaces.
	worker := core.Actor{Kind: core.AuthorAgent, ID: "agt_w", OwnerID: e.owner.ID, Workspace: "ws_x"}
	_, err := e.eng.CreateWorkspace(ctx, worker, "api", "")
	is(t, err, errs.ErrForbidden)

	for _, bad := range []string{"", "has space", "ws_like-an-id", strings.Repeat("x", 65)} {
		_, err := e.eng.CreateWorkspace(ctx, e.human, bad, "")
		is(t, err, errs.ErrInvalid)
	}

	w, err := e.eng.CreateWorkspace(ctx, e.human, "api", "backend")
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.eng.CreateWorkspace(ctx, e.human, "api", "")
	is(t, err, errs.ErrConflict)

	// Ambiguous, qualified, super and unknown references.
	is(t, e.eng.AddMember(ctx, e.human, w.ID, "infra", false), errs.ErrInvalid)
	is(t, e.eng.AddMember(ctx, e.human, w.ID, "super", false), errs.ErrInvalid)
	is(t, e.eng.AddMember(ctx, e.human, w.ID, "nobody", false), errs.ErrNotFound)
	if err := e.eng.AddMember(ctx, e.human, "api", "@infra@linux", false); err != nil { // by name, with @
		t.Fatal(err)
	}

	got, members, err := e.eng.Workspace(ctx, e.human, "api")
	if err != nil || got.ID != w.ID || len(members) != 2 {
		t.Fatalf("workspace = %+v %+v, %v", got, members, err)
	}
	leader := got.LeaderID

	// The general task exists from the start.
	tasks, err := e.eng.Tasks(ctx, e.human, w.ID)
	if err != nil || len(tasks) != 1 || !tasks[0].General || tasks[0].State != core.TaskRunning {
		t.Fatalf("tasks = %+v, %v", tasks, err)
	}

	// The leader can only leave last.
	is(t, e.eng.RemoveMember(ctx, e.human, w.ID, string(leader)), errs.ErrConflict)

	// Promote infra@linux; backend becomes a plain member.
	if err := e.eng.AddMember(ctx, e.human, w.ID, "infra@linux", true); err != nil {
		t.Fatal(err)
	}
	got, members, _ = e.eng.Workspace(ctx, e.human, w.ID)
	if got.LeaderID == leader {
		t.Fatal("leader did not change")
	}
	leaders := 0
	for _, m := range members {
		if m.Role == core.MemberLeader {
			leaders++
			if m.AgentID != got.LeaderID {
				t.Fatalf("leader member %s != workspace leader %s", m.AgentID, got.LeaderID)
			}
		}
	}
	if leaders != 1 {
		t.Fatalf("%d leaders", leaders)
	}
	if err := e.eng.RemoveMember(ctx, e.human, w.ID, string(leader)); err != nil {
		t.Fatal(err)
	}
	if err := e.eng.RemoveMember(ctx, e.human, w.ID, "infra@linux"); err != nil {
		t.Fatal(err)
	}
	got, members, _ = e.eng.Workspace(ctx, e.human, w.ID)
	if got.LeaderID != "" || len(members) != 0 {
		t.Fatalf("empty workspace = %+v %+v", got, members)
	}
	// The first member of a leaderless workspace leads.
	if err := e.eng.AddMember(ctx, e.human, w.ID, "backend", false); err != nil {
		t.Fatal(err)
	}
	got, _, _ = e.eng.Workspace(ctx, e.human, w.ID)
	if got.LeaderID == "" {
		t.Fatal("first member did not become leader")
	}
}

func TestScopesAndRecall(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.machine(t, "mac", 'a', protocol.AgentInfo{Name: "backend", Driver: ir.KindClaude})
	api, err := e.eng.CreateWorkspace(ctx, e.human, "api", "backend")
	if err != nil {
		t.Fatal(err)
	}
	web, err := e.eng.CreateWorkspace(ctx, e.human, "web", "")
	if err != nil {
		t.Fatal(err)
	}
	tasks, _ := e.eng.Tasks(ctx, e.human, api.ID)
	general := tasks[0]

	worker := core.Actor{Kind: core.AuthorAgent, ID: api.LeaderID, OwnerID: e.owner.ID, Workspace: api.ID, Task: general.ID}
	ws, err := e.eng.Workspaces(ctx, worker)
	if err != nil || len(ws) != 1 || ws[0].ID != api.ID {
		t.Fatalf("worker sees %+v, %v", ws, err)
	}
	_, _, err = e.eng.Workspace(ctx, worker, web.ID)
	is(t, err, errs.ErrForbidden)
	_, err = e.eng.Machines(ctx, worker)
	is(t, err, errs.ErrForbidden)
	if _, err := e.eng.Task(ctx, worker, general.ID); err != nil {
		t.Fatal(err)
	}

	stranger := core.Actor{Kind: core.AuthorHuman, OwnerID: "own_other"}
	_, err = e.eng.Task(ctx, stranger, general.ID)
	is(t, err, errs.ErrNotFound)
	ws, _ = e.eng.Workspaces(ctx, stranger)
	if len(ws) != 0 {
		t.Fatalf("stranger sees %+v", ws)
	}

	obj, err := e.eng.Recall(ctx, e.human, api.ID)
	if err != nil || obj.Type != "workspace" || obj.Links["leader"] != string(api.LeaderID) || obj.Links["task.0"] != string(general.ID) {
		t.Fatalf("recall workspace = %+v, %v", obj, err)
	}
	obj, err = e.eng.Recall(ctx, e.human, api.LeaderID)
	if err != nil || obj.Type != "agent" || !strings.Contains(obj.Summary, "backend@mac") {
		t.Fatalf("recall agent = %+v, %v", obj, err)
	}
	obj, err = e.eng.Recall(ctx, e.human, general.ID)
	if err != nil || obj.Type != "task" || obj.Links["workspace"] != string(api.ID) {
		t.Fatalf("recall task = %+v, %v", obj, err)
	}
	_, err = e.eng.Recall(ctx, e.human, "zzz_1")
	is(t, err, errs.ErrNotFound)
}
