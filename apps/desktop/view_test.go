package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/internal/profile"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

type memSecrets map[string]string

func (m memSecrets) Get(p string) (string, error) { return m[p], nil }
func (m memSecrets) Set(p, t string) error        { m[p] = t; return nil }
func (m memSecrets) Delete(p string) error        { delete(m, p); return nil }

func testDesktop(t *testing.T) *desktop {
	t.Helper()
	st, err := profile.Open(t.TempDir()+"/profiles.json", memSecrets{})
	if err != nil {
		t.Fatal(err)
	}
	return newDesktop(st)
}

func push(t *testing.T, event string, v any) protocol.Push {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.Push{Event: event, Payload: b}
}

// Every page renders without a server, and a click on an empty form
// starts no request.
func TestPagesRender(t *testing.T) {
	for _, page := range []string{pageChat, pageJobs, pageAgents, pageMachines, pageSettings} {
		t.Run(page, func(t *testing.T) {
			d := testDesktop(t)
			d.page = page
			tt := ui.NewTester(d.view, 1240, 820)
			if !tt.HasText("Solax") {
				t.Fatalf("page did not render: %q", tt.Texts())
			}
		})
	}
}

// Pushes from the stream build the conversation, the jobs and the live
// state of the Super Agent; the jobs page shows the events of a job.
func TestStreamPushes(t *testing.T) {
	d := testDesktop(t)
	now := time.Now()
	d.apply(push(t, "super.message", protocol.Message{ID: "m2", Seq: 2, Author: protocol.Author{Kind: "super"}, Text: "On it."}))
	d.apply(push(t, "super.message", protocol.Message{ID: "m1", Seq: 1, Author: protocol.Author{Kind: "human"}, Text: "Fix the bug"}))
	d.apply(push(t, "super.message", protocol.Message{ID: "m1", Seq: 1, Author: protocol.Author{Kind: "human"}, Text: "Fix the bug"}))
	if len(d.messages) != 2 || d.messages[0].ID != "m1" {
		t.Fatalf("messages = %+v", d.messages)
	}

	d.apply(push(t, "session.event", protocol.SessionEventNote{Super: true, Event: ir.Event{Type: ir.TurnStarted}}))
	if !d.superBusy {
		t.Fatal("Super Agent turn did not show as busy")
	}

	job := protocol.Job{ID: "job_1", Agent: "coder", SessionID: "ses_1", State: "running", Title: "Fix the bug", Prompt: "Fix the bug", CreatedAt: now}
	d.apply(push(t, "job", job))
	tool := &ir.ToolCall{Name: "Bash", Class: ir.ToolShell, Input: json.RawMessage(`{"command":"go test ./..."}`), Output: "ok"}
	for _, e := range []ir.Event{
		{Seq: 1, Type: ir.TurnStarted, Turn: &ir.Turn{Cause: ir.CauseInput}},
		{Seq: 2, Type: ir.ItemCompleted, Item: &ir.Item{ID: "t1", Kind: ir.ItemToolCall, Status: ir.StatusCompleted, Tool: tool}},
		{Seq: 0, Type: ir.ItemDelta, Delta: &ir.Delta{ItemID: "a1", Kind: "text", Text: "Fixed"}},
	} {
		d.apply(push(t, "session.event", protocol.SessionEventNote{SessionID: "ses_1", JobID: "job_1", Agent: "coder", Event: e}))
	}
	if len(d.events["ses_1"]) != 2 || d.live["ses_1"]["a1"] != "Fixed" {
		t.Fatalf("events = %+v live = %+v", d.events["ses_1"], d.live["ses_1"])
	}

	d.page, d.jobSel = pageJobs, "job_1"
	tt := ui.NewTester(d.view, 1240, 820)
	for _, want := range []string{"coder", "Bash: go test ./...", "Fixed"} {
		if !tt.HasText(want) {
			t.Fatalf("jobs page lacks %q: %q", want, tt.Texts())
		}
	}

	job.State, job.Result = "done", "All tests pass."
	d.apply(push(t, "job", job))
	if d.runningJobs() != 0 || len(d.jobs) != 1 {
		t.Fatalf("jobs = %+v", d.jobs)
	}
}

func TestJobOf(t *testing.T) {
	if got := jobOf("[job job_9] agent coder: done"); got != "job_9" {
		t.Fatalf("jobOf = %q", got)
	}
	if jobOf("hello") != "" {
		t.Fatal("jobOf of a plain text")
	}
}
