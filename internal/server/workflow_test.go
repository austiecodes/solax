package server_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/agent/agenttest"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/internal/server/service"
	"github.com/austiecodes/solax/internal/server/workflow"
	"github.com/austiecodes/solax/pkg/protocol"
)

// workers answers as the worker agents of the workflow tests. The
// reviewer first answers without JSON, so the runner has to ask again.
func workers(t *testing.T) (func(r *rig, spec agent.SessionSpec, text string) string, func() []string) {
	var mu sync.Mutex
	var prompts []string
	reply := func(_ *rig, spec agent.SessionSpec, text string) string {
		mu.Lock()
		prompts = append(prompts, text)
		mu.Unlock()
		switch {
		case strings.HasPrefix(text, "update "):
			return "updated " + strings.TrimPrefix(text, "update ")
		case strings.HasPrefix(text, "review "):
			return "Looks fine to me."
		case strings.HasPrefix(text, "Your answer did not match"):
			return "Sorry.\n```json\n{\"pass\": true, \"issues\": []}\n```"
		case strings.HasPrefix(text, "fail "):
			return "cannot"
		}
		return "ok: " + text
	}
	return reply, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), prompts...)
	}
}

const fanOutReview = `
export const meta = {
  name: "fanout-review",
  description: "Update packages in parallel, then review them",
  phases: [{ title: "Code" }, { title: "Review" }],
};

const VERDICT = {
  type: "object",
  properties: { pass: { type: "boolean" }, issues: { type: "array", items: { type: "string" } } },
  required: ["pass", "issues"],
};

interface Args { pkgs: string[] }

export async function run(wf: any, args: Args) {
  wf.phase("Code");
  const changes = await wf.parallel(args.pkgs.map((p: string) => () => wf.task("coder", "update " + p)));
  wf.log("updated " + changes.length + " packages");
  wf.phase("Review");
  const review = await wf.task("reviewer", "review " + changes.map((c: any) => c.result).join("; "), { schema: VERDICT });
  return { changed: changes.length, pass: review.data.pass, results: changes.map((c: any) => c.result) };
}
`

func waitWorkflow(t *testing.T, r *rig, id string, state string) protocol.Workflow {
	t.Helper()
	var w protocol.Workflow
	waitFor(t, "workflow "+id+" "+state, func() bool {
		item, err := r.client.Workflow(context.Background(), id)
		if err != nil || item.Run == nil {
			return false
		}
		w = *item.Run
		return w.State == state
	})
	return w
}

// A fan-out and review workflow survives a server restart in the middle:
// the new runner replays the finished steps and starts no task twice. The
// reviewer's answer without JSON gets one follow-up.
func TestWorkflowFanOutSurvivesRestart(t *testing.T) {
	reply, prompts := workers(t)
	r := newRig(t, reply, func(f *agenttest.FakeRuntime) { f.TurnDelay = 300 * time.Millisecond })
	r.addAgent(t, "coder", false)
	r.addAgent(t, "reviewer", false)
	ctx := context.Background()

	w, err := r.client.StartWorkflow(ctx, protocol.StartWorkflowRequest{
		Source: fanOutReview, Args: json.RawMessage(`{"pkgs": ["auth", "server"]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.Title != "fanout-review" || w.State != "running" {
		t.Fatalf("started = %+v", w)
	}

	// The coder tasks run: stop the runner, as a server stop does, and
	// start a new one.
	waitFor(t, "two coder tasks", func() bool {
		ts, _ := r.client.Tasks(ctx, 10)
		return len(ts) == 2
	})
	service.StopWorkflow(model.ID(w.ID))
	time.Sleep(50 * time.Millisecond)
	next := workflow.New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(next.Close)
	service.CheckWorkflow = next.Check
	service.RunWorkflow, service.StopWorkflow, service.WakeWorkflow = next.Start, next.Stop, next.Wake
	if err := next.Resume(ctx); err != nil {
		t.Fatal(err)
	}

	done := waitWorkflow(t, r, w.ID, "done")
	var res struct {
		Changed int
		Pass    bool
		Results []string
	}
	if err := json.Unmarshal(done.Result, &res); err != nil {
		t.Fatalf("result %s: %v", done.Result, err)
	}
	if res.Changed != 2 || !res.Pass || strings.Join(res.Results, ",") != "updated auth,updated server" {
		t.Fatalf("result = %+v", res)
	}
	ts, _ := r.client.Tasks(ctx, 10)
	if len(ts) != 3 {
		t.Fatalf("tasks = %d, want 3 (no task started twice)", len(ts))
	}
	retries := 0
	for _, p := range prompts() {
		if strings.HasPrefix(p, "Your answer did not match") {
			retries++
		}
	}
	if retries != 1 {
		t.Fatalf("schema follow-ups = %d, want 1", retries)
	}
	if len(done.Steps) != 3 || done.Steps[2].Phase != "Review" || done.Steps[0].Phase != "Code" {
		t.Fatalf("steps = %+v", done.Steps)
	}
	if !strings.Contains(done.Logs, "updated 2 packages") || strings.Count(done.Logs, "phase: Code") != 1 {
		t.Fatalf("logs = %q, want each line once", done.Logs)
	}
	// The person who started it sees the end in the conversation.
	waitFor(t, "a system line", func() bool {
		msgs, _ := r.client.SuperMessages(ctx, 0)
		for _, m := range msgs {
			if m.Kind == "system" && strings.Contains(m.Text, "[workflow "+w.ID+"] fanout-review: done") {
				return true
			}
		}
		return false
	})
}

// wf.ask waits for an answer; the answer goes back to the script.
func TestWorkflowAskAndAnswer(t *testing.T) {
	reply, _ := workers(t)
	r := newRig(t, reply)
	ctx := context.Background()
	w, err := r.client.StartWorkflow(ctx, protocol.StartWorkflowRequest{Source: `
export async function run(wf: any) {
  const a = await wf.ask("Ship it?", ["yes", "no"]);
  return a.choice + ":" + a.text;
}`})
	if err != nil {
		t.Fatal(err)
	}
	waitWorkflow(t, r, w.ID, "waiting")
	if _, err := r.client.AnswerWorkflow(ctx, w.ID, "maybe", ""); err == nil {
		t.Fatal("answer outside the options: want an error")
	}
	if _, err := r.client.AnswerWorkflow(ctx, w.ID, "yes", "after lunch"); err != nil {
		t.Fatal(err)
	}
	done := waitWorkflow(t, r, w.ID, "done")
	if string(done.Result) != `"yes:after lunch"` {
		t.Fatalf("result = %s", done.Result)
	}
}

// A failed run can resume with a fixed script: the finished task step is
// reused, not run again.
func TestWorkflowResumeEditedScript(t *testing.T) {
	reply, prompts := workers(t)
	r := newRig(t, reply)
	r.addAgent(t, "coder", false)
	ctx := context.Background()
	broken := `
export async function run(wf: any) {
  const a = await wf.task("coder", "update auth");
  const n: number = a.result.lenght.toFixed(); // typo
  return n;
}`
	w, err := r.client.StartWorkflow(ctx, protocol.StartWorkflowRequest{Source: broken})
	if err != nil {
		t.Fatal(err)
	}
	failed := waitWorkflow(t, r, w.ID, "failed")
	if !strings.Contains(failed.Error, "script.ts:4") {
		t.Fatalf("error = %q, want the script line", failed.Error)
	}
	fixed := strings.Replace(broken, "a.result.lenght.toFixed()", "a.result.length", 1)
	w2, err := r.client.StartWorkflow(ctx, protocol.StartWorkflowRequest{Source: fixed, Resume: w.ID})
	if err != nil {
		t.Fatal(err)
	}
	done := waitWorkflow(t, r, w2.ID, "done")
	if string(done.Result) != "12" { // len("updated auth")
		t.Fatalf("result = %s", done.Result)
	}
	n := 0
	for _, p := range prompts() {
		if p == "update auth" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the coder got the prompt %d times, want 1", n)
	}
}

// A script with a syntax error fails at start with its position; one with
// no run export fails when it runs.
func TestWorkflowScriptErrors(t *testing.T) {
	r := newRig(t, func(*rig, agent.SessionSpec, string) string { return "ok" })
	ctx := context.Background()
	_, err := r.client.StartWorkflow(ctx, protocol.StartWorkflowRequest{Source: "export async function run(wf) {\n  return (;\n}"})
	if err == nil || !strings.Contains(err.Error(), "script.ts:2") {
		t.Fatalf("err = %v, want script.ts:2", err)
	}
	if _, err := r.client.SaveWorkflow(ctx, "export async function run() {}"); err == nil {
		t.Fatal("save without meta: want an error")
	}
	w, err := r.client.StartWorkflow(ctx, protocol.StartWorkflowRequest{Source: "export const x = 1;"})
	if err != nil {
		t.Fatal(err)
	}
	failed := waitWorkflow(t, r, w.ID, "failed")
	if !strings.Contains(failed.Error, "must export") {
		t.Fatalf("error = %q", failed.Error)
	}
}

// A saved workflow runs by name with args; cancel stops a run and its task.
func TestWorkflowSavedAndCancel(t *testing.T) {
	r := newRig(t, func(*rig, agent.SessionSpec, string) string { return "ok" },
		func(f *agenttest.FakeRuntime) { f.TurnDelay = 2 * time.Second })
	r.addAgent(t, "coder", false)
	ctx := context.Background()
	sw, err := r.client.SaveWorkflow(ctx, `
export const meta = { name: "one-task", description: "Give one task to an agent" };
export async function run(wf: any, args: { agent: string; prompt: string }) {
  return (await wf.task(args.agent, args.prompt)).result;
}`)
	if err != nil || sw.Name != "one-task" {
		t.Fatalf("save = %+v, %v", sw, err)
	}
	w, err := r.client.StartWorkflow(ctx, protocol.StartWorkflowRequest{Name: "one-task", Args: json.RawMessage(`{"agent":"coder","prompt":"slow work"}`)})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the task", func() bool {
		ts, _ := r.client.Tasks(ctx, 10)
		return len(ts) == 1
	})
	c, err := r.client.CancelWorkflow(ctx, w.ID)
	if err != nil || c.State != "cancelled" {
		t.Fatalf("cancel = %+v, %v", c, err)
	}
	ts, _ := r.client.Tasks(ctx, 10)
	if ts[0].State != "cancelled" {
		t.Fatalf("task = %s, want cancelled", ts[0].State)
	}
	l, err := r.client.Workflows(ctx)
	if err != nil || len(l.Saved) != 1 || len(l.Runs) != 1 || l.Runs[0].State != "cancelled" {
		t.Fatalf("list = %+v, %v", l, err)
	}
}
