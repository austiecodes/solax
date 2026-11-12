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
	"github.com/austiecodes/solax/internal/server"
	"github.com/austiecodes/solax/internal/store/sqlite"
	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// rig is one server and one daemon in a test process. The daemon runs a
// fake agent driver; everything else is the real code.
type rig struct {
	url    string
	client *client.Client
	fake   *agenttest.FakeDriver

	mu     sync.Mutex
	pushes []protocol.Push
}

func newRig(t *testing.T, reply func(r *rig, spec agent.SessionSpec, text string) string) *rig {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "solax.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	app := server.New(store, []byte("test-secret"), log)
	srv := httptest.NewServer(app.Handler)
	t.Cleanup(srv.Close)
	app.Start(ctx, make(chan error, 4))

	owner, err := app.Engine.InitOwner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := app.Auth.CreateClient(ctx, owner.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{url: srv.URL, client: client.New(srv.URL, token)}

	// The machine, as "solax daemon start" sets it up.
	mtoken, _ := auth.NewToken(auth.PrefixMachine)
	if _, err := r.client.RegisterMachine(ctx, protocol.MachineEnrollment{Name: "mac", TokenHash: auth.Hash(mtoken)}); err != nil {
		t.Fatal(err)
	}

	// The daemon: real supervisor and connection, fake agent.
	r.fake = &agenttest.FakeDriver{KindV: ir.KindClaude, TurnDelay: 20 * time.Millisecond}
	r.fake.Reply = func(spec agent.SessionSpec, text string) string { return reply(r, spec, text) }
	var sup *supervisor.Supervisor
	cn := conn.New(srv.URL, mtoken,
		func(ctx context.Context) protocol.Hello {
			return protocol.Hello{
				Drivers:  []protocol.DriverProbe{{Kind: ir.KindClaude, OK: true, Caps: agenttest.FakeCaps}},
				Sessions: sup.States(), FreeSlots: sup.FreeSlots(),
			}
		},
		func() protocol.Heartbeat { return protocol.Heartbeat{FreeSlots: sup.FreeSlots()} },
		func(ctx context.Context, env protocol.Envelope) protocol.Ack { return sup.Handle(ctx, env) },
		log)
	sup = supervisor.New(ctx, supervisor.Config{
		Registry: agent.NewRegistry(r.fake),
		Env: func(e protocol.SessionEnsure) map[string]string {
			return map[string]string{"SOLAX_SERVER": srv.URL, "SOLAX_TOKEN": e.AgentToken, "SOLAX_JOB": e.Job}
		},
		MaxSlots: 4, Sink: cn, Log: log,
	})
	go cn.Run(ctx)
	go r.client.Stream(ctx, func(p protocol.Push) {
		r.mu.Lock()
		r.pushes = append(r.pushes, p)
		r.mu.Unlock()
	})
	waitFor(t, "daemon online", func() bool { return cn.Connected() })
	return r
}

func (r *rig) addAgent(t *testing.T, name string, super bool) {
	t.Helper()
	_, err := r.client.CreateAgent(context.Background(), protocol.CreateAgentRequest{
		Machine: "mac", Name: name, Driver: ir.KindClaude, Dir: "/src", Description: name + " agent", Super: super,
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

// A person dispatches a job: the call returns at once, the daemon starts
// a session, the agent answers, and the job is done with the answer. A
// follow-up runs the same session again.
func TestJobRoundTrip(t *testing.T) {
	r := newRig(t, func(_ *rig, _ agent.SessionSpec, text string) string { return "echo: " + text })
	r.addAgent(t, "coder", false)
	ctx := context.Background()

	j, err := r.client.Dispatch(ctx, "coder", "fix the bug")
	if err != nil {
		t.Fatal(err)
	}
	if j.State != "queued" {
		t.Fatalf("dispatch returned state %q, want queued (it must not wait)", j.State)
	}
	var got protocol.Job
	waitFor(t, "job done", func() bool {
		got, _ = r.client.Job(ctx, j.ID)
		return got.State == "done"
	})
	if got.Result != "echo: fix the bug" {
		t.Fatalf("result = %q", got.Result)
	}
	starts := r.fake.Starts()
	if len(starts) != 1 || starts[0].Env["SOLAX_TOKEN"] == "" || starts[0].Env["SOLAX_JOB"] != j.ID {
		t.Fatalf("session spec = %+v", starts)
	}

	// Follow-up: same session, new result.
	if _, err := r.client.FollowUp(ctx, j.ID, "and add a test"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "follow-up done", func() bool {
		got, _ = r.client.Job(ctx, j.ID)
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
	waitFor(t, "stream pushes", func() bool { return r.pushCount("session.event") >= 8 && r.pushCount("job") >= 2 })
}

// The Super Agent routes asynchronously: the user message wakes it, it
// dispatches a job with its own agent token and ends its turn at once.
// The job result wakes it again, and its summary reaches the user.
func TestSuperAgentRoutesAsync(t *testing.T) {
	r := newRig(t, func(r *rig, spec agent.SessionSpec, text string) string {
		if spec.Env["SOLAX_JOB"] != "" {
			return "worker did: " + text // a worker
		}
		// The Super Agent, acting with its own token, as the solax CLI would.
		me := client.New(r.url, spec.Env["SOLAX_TOKEN"])
		switch {
		case strings.HasPrefix(text, "[user]"):
			j, err := me.Dispatch(context.Background(), "coder", strings.TrimPrefix(text, "[user] "))
			if err != nil {
				return "dispatch failed: " + err.Error()
			}
			return "I gave it to coder as " + j.ID + "."
		case strings.HasPrefix(text, "[job"):
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
	// you → super (dispatched) → system note (job done) → super (summary).
	kinds := []string{}
	for _, m := range msgs {
		kinds = append(kinds, m.Author.Kind)
	}
	if strings.Join(kinds, ",") != "human,super,system,super" {
		t.Fatalf("conversation = %v", kinds)
	}
	jobs, _ := r.client.Jobs(ctx, 10)
	if len(jobs) != 1 || jobs[0].RequestedBy != "super" || jobs[0].State != "done" {
		t.Fatalf("jobs = %+v", jobs)
	}
}
