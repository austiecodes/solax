package server_test

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/agent/agenttest"
	"github.com/austiecodes/solax/internal/auth"
	"github.com/austiecodes/solax/internal/daemon/conn"
	"github.com/austiecodes/solax/internal/daemon/supervisor"
	"github.com/austiecodes/solax/internal/daemon/workspace"
	"github.com/austiecodes/solax/internal/server"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/internal/server/service"
	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// rig is one server and one daemon in a test process. The daemon runs a
// fake agent runtime; everything else is the real code.
type rig struct {
	url    string
	client *client.Client
	fake   *agenttest.FakeRuntime
	home   string // the SOLAX_HOME of the daemon: its projects and super folders

	mu     sync.Mutex
	pushes []protocol.Push
}

func newRig(t *testing.T, reply func(r *rig, spec agent.SessionSpec, text string) string, opts ...func(*agenttest.FakeRuntime)) *rig {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := model.Open(ctx, filepath.Join(t.TempDir(), "solax.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { model.Close() })
	app := server.New([]byte("test-secret"), log)
	srv := httptest.NewServer(app.Handler)
	// Cleanups run last first: stop the daemon, the HTTP server and the
	// daemon handlers, then close the database.
	t.Cleanup(func() {
		srv.Close()
		app.Close()
	})
	app.Start(ctx, make(chan error, 4))

	owner, err := service.InitOwner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := auth.CreateClient(ctx, owner.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{url: srv.URL, client: client.New(srv.URL, token), home: t.TempDir()}

	// The machine, as "solax daemon start" sets it up.
	mtoken, _ := auth.NewToken(auth.PrefixMachine)
	if _, err := r.client.RegisterMachine(ctx, protocol.MachineEnrollment{Name: "mac", TokenHash: auth.Hash(mtoken)}); err != nil {
		t.Fatal(err)
	}

	// The daemon: real supervisor and connection, fake agent.
	r.fake = &agenttest.FakeRuntime{KindV: ir.KindClaude, TurnDelay: 20 * time.Millisecond}
	r.fake.Reply = func(spec agent.SessionSpec, text string) string { return reply(r, spec, text) }
	for _, o := range opts {
		o(r.fake)
	}
	var sup *supervisor.Supervisor
	cn := conn.New(srv.URL, mtoken,
		func(ctx context.Context) protocol.Hello {
			return protocol.Hello{
				Runtimes: []protocol.RuntimeProbe{{Kind: ir.KindClaude, OK: true, Caps: r.fake.Caps()}},
				Sessions: sup.States(), FreeSlots: sup.FreeSlots(),
			}
		},
		func() protocol.Heartbeat { return protocol.Heartbeat{FreeSlots: sup.FreeSlots()} },
		func(ctx context.Context, env protocol.Envelope) protocol.Ack { return sup.Handle(ctx, env) },
		log)
	sup = supervisor.New(ctx, supervisor.Config{
		Runtimes: []agent.Runtime{r.fake},
		Env: func(e protocol.SessionEnsure) map[string]string {
			return map[string]string{"SOLAX_SERVER": srv.URL, "SOLAX_TOKEN": e.AgentToken, "SOLAX_TASK": e.Task}
		},
		Workspace: workspace.Sources{
			protocol.WorkspaceLocal:   workspace.Local{},
			protocol.WorkspaceManaged: workspace.Managed{Root: filepath.Join(r.home, "projects")},
			protocol.WorkspaceSuper:   workspace.Fixed{Dir: filepath.Join(r.home, "super")},
		}.Prepare,
		MaxSlots: 4, Send: cn.Send, Log: log,
	})
	go cn.Run(ctx)
	go r.client.Stream(ctx, func(p protocol.Push) {
		r.mu.Lock()
		r.pushes = append(r.pushes, p)
		r.mu.Unlock()
	})
	waitFor(t, "daemon online", func() bool { return cn.Connected() })
	t.Cleanup(cancel)
	return r
}

func (r *rig) addAgent(t *testing.T, name string, super bool) {
	t.Helper()
	_, err := r.client.CreateAgent(context.Background(), protocol.CreateAgentRequest{
		Machine: "mac", Name: name, Runtime: ir.KindClaude, Description: name + " agent", Super: super,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (r *rig) pushCount(event string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, p := range r.pushes {
		if p.Event == event {
			n++
		}
	}
	return n
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A person dispatches a task: the call returns at once, the daemon starts
// a session, the agent answers, and the task is done with the answer. A
// follow-up runs the same session again.
func TestTaskRoundTrip(t *testing.T) {
	r := newRig(t, func(_ *rig, _ agent.SessionSpec, text string) string { return "echo: " + text })
	r.addAgent(t, "coder", false)
	ctx := context.Background()

	j, err := r.client.Dispatch(ctx, protocol.DispatchRequest{Agent: "coder", Prompt: "fix the bug"})
	if err != nil {
		t.Fatal(err)
	}
	if j.State != "queued" {
		t.Fatalf("dispatch returned state %q, want queued (it must not wait)", j.State)
	}
	var got protocol.Task
	waitFor(t, "task done", func() bool {
		got, _ = r.client.Task(ctx, j.ID)
		return got.State == "done"
	})
	if got.Result != "echo: fix the bug" {
		t.Fatalf("result = %q", got.Result)
	}
	starts := r.fake.Starts()
	if len(starts) != 1 || starts[0].Env["SOLAX_TOKEN"] == "" || starts[0].Env["SOLAX_TASK"] != j.ID {
		t.Fatalf("session spec = %+v", starts)
	}

	// Follow-up: same session, new result.
	if _, err := r.client.FollowUp(ctx, j.ID, "and add a test"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "follow-up done", func() bool {
		got, _ = r.client.Task(ctx, j.ID)
		return got.State == "done" && got.Result == "echo: and add a test"
	})
	if n := len(r.fake.Starts()); n != 1 {
		t.Fatalf("follow-up started %d sessions, want the same one", n)
	}

	// The stored events replay the session; the stream pushed them live.
	evs, err := r.client.SessionEvents(ctx, j.SessionID, 0)
	if err != nil || len(evs) < 8 {
		t.Fatalf("stored events = %d, %v", len(evs), err)
	}
	waitFor(t, "stream pushes", func() bool { return r.pushCount("session.event") >= 8 && r.pushCount("task") >= 2 })
}

// The Super Agent routes asynchronously: the user message wakes it, it
// dispatches a task with its own agent token and ends its turn at once.
// The task result wakes it again, and its summary reaches the user.
func TestSuperAgentRoutesAsync(t *testing.T) {
	r := newRig(t, func(r *rig, spec agent.SessionSpec, text string) string {
		if spec.Env["SOLAX_TASK"] != "" {
			return "worker did: " + text // a worker
		}
		// The Super Agent, acting with its own token, as the solax CLI would.
		me := client.New(r.url, spec.Env["SOLAX_TOKEN"])
		switch {
		case strings.HasPrefix(text, "[user]"):
			j, err := me.Dispatch(context.Background(), protocol.DispatchRequest{Agent: "coder", Prompt: strings.TrimPrefix(text, "[user] ")})
			if err != nil {
				return "dispatch failed: " + err.Error()
			}
			return "I gave it to coder as " + j.ID + "."
		case strings.HasPrefix(text, "[task"):
			return "Done. " + text[strings.Index(text, "result:"):]
		}
		return "?"
	})
	r.addAgent(t, "router", true)
	r.addAgent(t, "coder", false)
	ctx := context.Background()

	if _, err := r.client.SendToSuper(ctx, "write the README"); err != nil {
		t.Fatal(err)
	}
	var msgs []protocol.Message
	waitFor(t, "summary from the Super Agent", func() bool {
		msgs, _ = r.client.SuperMessages(ctx, 0)
		for _, m := range msgs {
			if m.Author.Kind == "super" && strings.Contains(m.Text, "worker did: write the README") {
				return true
			}
		}
		return false
	})
	// you → super (dispatched) → system note (task done) → super (summary).
	kinds := []string{}
	for _, m := range msgs {
		kinds = append(kinds, m.Author.Kind)
	}
	if strings.Join(kinds, ",") != "human,super,system,super" {
		t.Fatalf("conversation = %v", kinds)
	}
	tasks, _ := r.client.Tasks(ctx, 10)
	if len(tasks) != 1 || tasks[0].RequestedBy != "super" || tasks[0].State != "done" {
		t.Fatalf("tasks = %+v", tasks)
	}
	// The Super Agent has no project: it works in a folder of its own. Its
	// task named no project, so Solax launched one.
	for _, s := range r.fake.Starts() {
		if s.Env["SOLAX_TASK"] == "" && s.Dir != filepath.Join(r.home, "super") {
			t.Fatalf("the Super Agent works in %q", s.Dir)
		}
	}
	if tasks[0].Project != "write-the-readme" {
		t.Fatalf("task project = %q", tasks[0].Project)
	}
}

// The user stops the Super Agent: the interrupt reaches its live session.
// Before the session runs, there is nothing to stop.
func TestInterruptSuper(t *testing.T) {
	r := newRig(t, func(_ *rig, _ agent.SessionSpec, text string) string { return "ok" })
	r.addAgent(t, "router", true)
	ctx := context.Background()

	if err := r.client.InterruptSuper(ctx); err != nil || r.fake.Interrupts() != 0 {
		t.Fatalf("interrupt with no session: err %v, interrupts %d", err, r.fake.Interrupts())
	}
	if _, err := r.client.SendToSuper(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a reply", func() bool {
		msgs, _ := r.client.SuperMessages(ctx, 0)
		return len(msgs) == 2
	})
	if err := r.client.InterruptSuper(ctx); err != nil {
		t.Fatal(err)
	}
	if n := r.fake.Interrupts(); n != 1 {
		t.Fatalf("interrupts = %d, want 1", n)
	}
}

// The user stops the running turn of a task: the interrupt reaches its
// session. A task that does not run has nothing to stop.
func TestInterruptTask(t *testing.T) {
	r := newRig(t, func(_ *rig, _ agent.SessionSpec, text string) string { return "ok" },
		func(f *agenttest.FakeRuntime) { f.TurnDelay = 2 * time.Second })
	r.addAgent(t, "coder", false)
	ctx := context.Background()

	j, err := r.client.Dispatch(ctx, protocol.DispatchRequest{Agent: "coder", Prompt: "write the README"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the task runs", func() bool {
		j, _ = r.client.Task(ctx, j.ID)
		return j.State == "running"
	})
	if _, err := r.client.InterruptTask(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	if n := r.fake.Interrupts(); n != 1 {
		t.Fatalf("interrupts = %d, want 1", n)
	}
	waitFor(t, "the turn ends", func() bool {
		j, _ = r.client.Task(ctx, j.ID)
		return j.State != "running"
	})
	if _, err := r.client.InterruptTask(ctx, j.ID); err != nil || r.fake.Interrupts() != 1 {
		t.Fatalf("interrupt of a task that does not run: err %v, interrupts %d", err, r.fake.Interrupts())
	}
}

// A task works in the folder of its project. A task that names no project
// gets a new managed project, in the projects folder of the daemon. Several agents share a project; a local project needs an agent
// on its machine.
func TestTaskWorksInItsProject(t *testing.T) {
	r := newRig(t, func(_ *rig, _ agent.SessionSpec, text string) string { return "ok" })
	r.addAgent(t, "coder", false)
	r.addAgent(t, "reviewer", false)
	ctx := context.Background()

	dir := t.TempDir()
	p, err := r.client.CreateProject(ctx, protocol.CreateProjectRequest{Name: "app", Machine: "mac", Path: dir, Description: "the app"})
	if err != nil || p.Kind != "local" || p.Machine != "mac" || !p.Online {
		t.Fatalf("project = %+v, %v", p, err)
	}
	if _, err := r.client.CreateProject(ctx, protocol.CreateProjectRequest{Name: "app", Machine: "mac", Path: dir}); err == nil {
		t.Fatal("a second project named app: no error")
	}
	if _, err := r.client.CreateProject(ctx, protocol.CreateProjectRequest{Name: "rel", Machine: "mac", Path: "src/app"}); err == nil {
		t.Fatal("a relative folder: no error")
	}

	dirOf := map[string]string{} // task ID -> work folder of its session
	run := func(req protocol.DispatchRequest) protocol.Task {
		t.Helper()
		j, err := r.client.Dispatch(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, "task "+j.ID+" done", func() bool {
			j, _ = r.client.Task(ctx, j.ID)
			return j.State == "done"
		})
		for _, s := range r.fake.Starts() {
			if s.Env["SOLAX_TASK"] == j.ID {
				dirOf[j.ID] = s.Dir
			}
		}
		return j
	}
	a := run(protocol.DispatchRequest{Agent: "coder", Project: "app", Prompt: "fix the bug"})
	b := run(protocol.DispatchRequest{Agent: "reviewer", Project: p.ID, Prompt: "review the fix"})
	c := run(protocol.DispatchRequest{Agent: "coder", Prompt: "draft a note"})
	if a.Project != "app" || a.ProjectID != p.ID || dirOf[a.ID] != dir || dirOf[b.ID] != dir {
		t.Fatalf("project tasks: %+v in %q, %+v in %q; want %q", a, dirOf[a.ID], b, dirOf[b.ID], dir)
	}
	if c.Project != "draft-a-note" || c.ProjectID == "" || dirOf[c.ID] != filepath.Join(r.home, "projects", "draft-a-note") {
		t.Fatalf("task without a project: %+v in %q", c, dirOf[c.ID])
	}
	// The next task with the same title gets a project of its own; a task
	// that names the managed project works in its folder.
	d := run(protocol.DispatchRequest{Agent: "coder", Prompt: "draft a note"})
	e := run(protocol.DispatchRequest{Agent: "reviewer", Project: c.Project, Prompt: "review the note"})
	if d.Project != "draft-a-note-2" || dirOf[e.ID] != dirOf[c.ID] {
		t.Fatalf("second task %+v; reviewer in %q, want %q", d, dirOf[e.ID], dirOf[c.ID])
	}
	ps, _ := r.client.Projects(ctx)
	var managed protocol.Project
	for _, q := range ps {
		if q.ID == c.ProjectID {
			managed = q
		}
	}
	if managed.Kind != "managed" || managed.Path != "~/.solax/projects/draft-a-note" || managed.Machine != "mac" || managed.Description != "draft a note" {
		t.Fatalf("managed project = %+v", managed)
	}
	moved := "/tmp/elsewhere"
	if _, err := r.client.UpdateProject(ctx, c.Project, protocol.UpdateProjectRequest{Path: &moved}); err == nil {
		t.Fatal("moved the folder of a managed project: no error")
	}

	// A folder on another machine needs an agent there.
	mtoken, _ := auth.NewToken(auth.PrefixMachine)
	if _, err := r.client.RegisterMachine(ctx, protocol.MachineEnrollment{Name: "dev", TokenHash: auth.Hash(mtoken)}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.client.CreateProject(ctx, protocol.CreateProjectRequest{Name: "infra", Machine: "dev", Path: "/srv/infra"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.client.Dispatch(ctx, protocol.DispatchRequest{Agent: "coder", Project: "infra", Prompt: "x"}); err == nil ||
		!strings.Contains(err.Error(), "another machine") {
		t.Fatalf("dispatch to a project on another machine: err = %v", err)
	}

	// A removed project leaves the list; its tasks still name it.
	if err := r.client.DeleteProject(ctx, "app"); err != nil {
		t.Fatal(err)
	}
	ps, _ = r.client.Projects(ctx)
	var names []string
	for _, q := range ps {
		names = append(names, q.Name)
	}
	if strings.Join(names, ",") != "draft-a-note,draft-a-note-2,infra" {
		t.Fatalf("projects = %v", names)
	}
	if j, _ := r.client.Task(ctx, a.ID); j.Project != "app" {
		t.Fatalf("task of a removed project = %+v", j)
	}
}

// The instructions of an agent reach the system prompt of its sessions.
// An edit applies to the next session that starts.
func TestAgentInstructionsReachSession(t *testing.T) {
	r := newRig(t, func(_ *rig, _ agent.SessionSpec, text string) string { return "ok" })
	ctx := context.Background()
	a, err := r.client.CreateAgent(ctx, protocol.CreateAgentRequest{
		Machine: "mac", Name: "reviewer", Runtime: ir.KindClaude,
		Instructions: "  Review Go code. Report bugs only.\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.Instructions != "Review Go code. Report bugs only." {
		t.Fatalf("created instructions = %q, want them trimmed", a.Instructions)
	}
	edited := "Review Go code and its tests."
	if a, err = r.client.UpdateAgent(ctx, "reviewer", protocol.UpdateAgentRequest{Instructions: &edited}); err != nil || a.Instructions != edited {
		t.Fatalf("updated = %+v, %v", a, err)
	}
	as, err := r.client.Agents(ctx)
	if err != nil || len(as) != 1 || as[0].Instructions != edited {
		t.Fatalf("agents = %+v, %v", as, err)
	}

	j, err := r.client.Dispatch(ctx, protocol.DispatchRequest{Agent: "reviewer", Prompt: "review the auth package"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "task done", func() bool {
		got, _ := r.client.Task(ctx, j.ID)
		return got.State == "done"
	})
	// The rig has no role prompt, so the system prompt is the instructions.
	if starts := r.fake.Starts(); len(starts) != 1 || starts[0].Instructions != edited {
		t.Fatalf("session spec = %+v", starts)
	}
}

// Fast Path and Slow Path (system.md §4): the user steers and follows up a
// task that the Super Agent started, without a turn of the Super Agent. The
// Super Agent gets what the user did as [context] with its next waking
// input.
func TestFastPathNotesReachSuperAgent(t *testing.T) {
	var mu sync.Mutex
	var superGot []string
	superTurns := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), superGot...)
	}
	r := newRig(t, func(r *rig, spec agent.SessionSpec, text string) string {
		if spec.Env["SOLAX_TASK"] != "" {
			return "worker did: " + text
		}
		mu.Lock()
		superGot = append(superGot, text)
		mu.Unlock()
		if text == "[user] fix auth" {
			me := client.New(r.url, spec.Env["SOLAX_TOKEN"])
			if _, err := me.Dispatch(context.Background(), protocol.DispatchRequest{Agent: "coder", Prompt: "fix auth"}); err != nil {
				return "dispatch failed: " + err.Error()
			}
			return "sent to coder"
		}
		return "noted"
	}, func(f *agenttest.FakeRuntime) {
		f.Steer = true
		f.TurnDelay = 400 * time.Millisecond
	})
	r.addAgent(t, "router", true)
	r.addAgent(t, "coder", false)
	ctx := context.Background()

	// Slow Path: the user asks the Super Agent, which starts a task.
	if _, err := r.client.SendToSuper(ctx, "fix auth"); err != nil {
		t.Fatal(err)
	}
	var task protocol.Task
	waitFor(t, "the task runs", func() bool {
		ts, _ := r.client.Tasks(ctx, 10)
		if len(ts) == 1 && ts[0].State == "running" {
			task = ts[0]
			return true
		}
		return false
	})
	if task.RequestedBy != "super" || task.TurnBy != "super" {
		t.Fatalf("task = %+v, want requested and owned by super", task)
	}

	// Fast Path 1: the user steers the running turn. The turn stays the
	// Super Agent's, and it gets the steer and the result in one turn.
	steered, err := r.client.Steer(ctx, task.ID, "use a 30s leeway")
	if err != nil {
		t.Fatal(err)
	}
	if steered.TurnBy != "super" {
		t.Fatalf("after steer turn_by = %s, want super", steered.TurnBy)
	}
	waitFor(t, "the Super Agent gets the result", func() bool { return len(superTurns()) == 2 })
	got := superTurns()[1]
	for _, want := range []string{"[context]", "the user steered task " + task.ID, "agent coder: done", "worker did: fix auth\n\nuse a 30s leeway"} {
		if !strings.Contains(got, want) {
			t.Fatalf("second Super Agent turn has no %q:\n%s", want, got)
		}
	}

	// Fast Path 2: the user follows up the finished task directly. The
	// worker answers the user; the Super Agent does not wake.
	followed, err := r.client.FollowUp(ctx, task.ID, "add a benchmark")
	if err != nil {
		t.Fatal(err)
	}
	if followed.TurnBy != "human" {
		t.Fatalf("after follow-up turn_by = %s, want human", followed.TurnBy)
	}
	waitFor(t, "the follow-up is done", func() bool {
		j, _ := r.client.Task(ctx, task.ID)
		return j.State == "done" && strings.Contains(j.Result, "add a benchmark")
	})
	time.Sleep(300 * time.Millisecond) // a wrong wake would show by now
	if n := len(superTurns()); n != 2 {
		t.Fatalf("Super Agent turns = %d after the direct follow-up, want 2:\n%v", n, superTurns())
	}

	// The user talks to the Super Agent again: it gets the follow-up and
	// its result as context first.
	if _, err := r.client.SendToSuper(ctx, "status?"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the third Super Agent turn", func() bool { return len(superTurns()) == 3 })
	got = superTurns()[2]
	for _, want := range []string{"[context]", "the user followed up task " + task.ID, "agent coder: done", "add a benchmark", "[user] status?"} {
		if !strings.Contains(got, want) {
			t.Fatalf("third Super Agent turn has no %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "[context]") > strings.Index(got, "[user] status?") {
		t.Fatalf("context comes after the user message:\n%s", got)
	}
}
