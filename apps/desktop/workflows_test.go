package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/pkg/protocol"
)

const sampleSource = `export const meta = { name: "fix-until-pass", description: "Coder fixes, reviewer checks, until the review passes" };

export async function run(wf, args) {
  let code = await wf.task("coder", args.goal);
  let review = await wf.task("reviewer", "Review: " + args.goal, { schema: VERDICT });
  return review.data;
}`

// sampleWorkflows are a run that asks, a run that is done, and a saved
// script.
func sampleWorkflows(at time.Time) protocol.WorkflowList {
	return protocol.WorkflowList{
		Saved: []protocol.SavedWorkflow{{
			Name: "fix-until-pass", Description: "Coder fixes, reviewer checks, until the review passes",
			Meta: protocol.WorkflowMeta{
				Name: "fix-until-pass", WhenToUse: "A change that must pass a review",
				Phases: []protocol.WorkflowPhase{{Title: "Code"}, {Title: "Review", Detail: "Up to 5 rounds"}, {Title: "Ship", Detail: "Open a pull request"}},
			},
		}},
		Runs: []protocol.Workflow{
			{ID: "wfl_1", Name: "fix-until-pass", Title: "fix-until-pass", RequestedBy: "super", State: "waiting", CreatedAt: at.Add(-12 * time.Minute)},
			{ID: "wfl_2", Title: "update-and-review", RequestedBy: "human", State: "done", CreatedAt: at.Add(-2 * time.Hour)},
		},
	}
}

func sampleRun(at time.Time) protocol.WorkflowItem {
	w := sampleWorkflows(at).Runs[0]
	w.Source, w.Logs = sampleSource, "round 2: 3 issues\nround 3: 1 issues\n"
	t := func(m, s int) time.Time {
		return w.CreatedAt.Add(time.Duration(m)*time.Minute + time.Duration(s)*time.Second)
	}
	w.Steps = []protocol.WorkflowStep{
		{Key: "task:aa:0", Seq: 1, Kind: "task", Target: "tsk_1", Phase: "Code", State: "done", CreatedAt: t(0, 2), UpdatedAt: t(2, 14)},
		{Key: "task:bb:0", Seq: 2, Kind: "task", Target: "tsk_2", Phase: "Review", Label: "auth", Loop: "fix", Round: 1, MaxRounds: 3,
			State: "done", CreatedAt: t(2, 14), UpdatedAt: t(4, 15)},
		{Key: "reply:cc:0", Seq: 3, Kind: "reply", Target: "tsk_1", Phase: "Review", Label: "fix round 2", Loop: "fix", Round: 2, MaxRounds: 3,
			State: "done", CreatedAt: t(4, 15), UpdatedAt: t(6, 24)},
		{Key: "reply:dd:0", Seq: 4, Kind: "reply", Target: "tsk_2", Phase: "Review", Label: "review round 2", Loop: "fix", Round: 2, MaxRounds: 3,
			State: "done", CreatedAt: t(6, 24), UpdatedAt: t(7, 46)},
		{Key: "ask:ee:0", Seq: 5, Kind: "ask", Target: `{"prompt":"The review still fails after 5 rounds. Keep the branch?","options":["keep","drop"]}`,
			Phase: "Review", State: "pending", CreatedAt: t(7, 46), UpdatedAt: t(7, 46)},
	}
	return protocol.WorkflowItem{Run: &w}
}

// sampleLiveRun is a run in round 1 of a loop, with two tasks at work.
func sampleLiveRun(at time.Time) protocol.WorkflowItem {
	w := protocol.Workflow{ID: "wfl_3", Name: "fix-until-pass", Title: "fix-until-pass", RequestedBy: "human", State: "running",
		Source: sampleSource, CreatedAt: at.Add(-3 * time.Minute)}
	t := func(m, s int) time.Time {
		return w.CreatedAt.Add(time.Duration(m)*time.Minute + time.Duration(s)*time.Second)
	}
	w.Steps = []protocol.WorkflowStep{
		{Key: "agents:aa:0", Seq: 1, Kind: "agents", Phase: "Code", State: "done", CreatedAt: t(0, 0), UpdatedAt: t(0, 1)},
		{Key: "task:bb:0", Seq: 2, Kind: "task", Target: "tsk_3", Phase: "Code", State: "done", CreatedAt: t(0, 1), UpdatedAt: t(2, 14)},
		{Key: "task:cc:0", Seq: 3, Kind: "task", Target: "tsk_2", Phase: "Review", Loop: "fix", Round: 1, MaxRounds: 3,
			State: "pending", CreatedAt: t(2, 14), UpdatedAt: t(2, 14)},
		{Key: "task:dd:0", Seq: 4, Kind: "task", Target: "tsk_1", Phase: "Review", Label: "go test ./internal/auth/...", Loop: "fix", Round: 1, MaxRounds: 3,
			State: "pending", CreatedAt: t(2, 15), UpdatedAt: t(2, 15)},
	}
	return protocol.WorkflowItem{Run: &w}
}

// sampleFailedRun is a run whose script does not call wf.round: its
// replies are its rounds. A review reply failed.
func sampleFailedRun(at time.Time) protocol.WorkflowItem {
	w := protocol.Workflow{ID: "wfl_4", Title: "update-and-review", RequestedBy: "human", State: "failed",
		Error: "task tsk_2 (agent reviewer) failed: go test: 3 failures", Source: sampleSource,
		CreatedAt: at.Add(-20 * time.Minute), UpdatedAt: at.Add(-11 * time.Minute)}
	t := func(m, s int) time.Time {
		return w.CreatedAt.Add(time.Duration(m)*time.Minute + time.Duration(s)*time.Second)
	}
	w.Steps = []protocol.WorkflowStep{
		{Key: "task:aa:0", Seq: 1, Kind: "task", Target: "tsk_1", State: "done", CreatedAt: t(0, 1), UpdatedAt: t(2, 30)},
		{Key: "task:bb:0", Seq: 2, Kind: "task", Target: "tsk_2", State: "done", CreatedAt: t(2, 30), UpdatedAt: t(4, 0)},
		{Key: "reply:cc:0", Seq: 3, Kind: "reply", Target: "tsk_1", Label: "fix 2 issues", State: "done", CreatedAt: t(4, 0), UpdatedAt: t(6, 40)},
		{Key: "reply:dd:0", Seq: 4, Kind: "reply", Target: "tsk_2", Label: "review again", State: "failed", CreatedAt: t(6, 40), UpdatedAt: t(9, 0)},
	}
	return protocol.WorkflowItem{Run: &w}
}

func sampleDoneRun(at time.Time) protocol.WorkflowItem {
	w := sampleWorkflows(at).Runs[1]
	w.ResumeFrom = "wfl_0"
	w.UpdatedAt = w.CreatedAt.Add(4 * time.Minute)
	w.Result = json.RawMessage(`[{"pkg":"internal/auth","pass":true,"issues":[]}]`)
	w.Steps = []protocol.WorkflowStep{{Key: "task:aa:0", Kind: "task", Target: "tsk_3", State: "done",
		CreatedAt: w.CreatedAt.Add(time.Second), UpdatedAt: w.CreatedAt.Add(4 * time.Minute)}}
	return protocol.WorkflowItem{Run: &w}
}

// The list shows the runs and the saved scripts; a run shows its question,
// its steps by phase, its log and its script; the sidebar row says that a
// run asks.
func TestWorkflowsPage(t *testing.T) {
	d := sampleDesktop(t)
	d.page = pageWorkflows
	tt := ui.NewTester(d.view, 1240, 820)
	for _, want := range []string{"Workflows", "Runs", "Saved", "fix-until-pass", "update-and-review", "asks", "Coder fixes, reviewer checks, until the review passes"} {
		if !tt.HasText(want) {
			t.Errorf("list: no %q in %q", want, tt.Texts())
		}
	}

	d.wf.sel, d.wf.item = "wfl_1", sampleRun(time.Now())
	tt = ui.NewTester(d.view, 1240, 820)
	for _, want := range []string{
		"The workflow asks", "The review still fails after 5 rounds. Keep the branch?", "keep", "drop",
		"Progress", "Graph", "Rail", "Code", "Review", "Ship", "Waits for your answer", "fix", "2 of 3 rounds", "R2", "R2 · fix round 2",
		"Log", "round 3: 1 issues", "Script", "Cancel workflow",
	} {
		if !tt.HasText(want) {
			t.Errorf("run: no %q in %q", want, tt.Texts())
		}
	}

	d.wf.sel, d.wf.item = "wfl_2", sampleDoneRun(time.Now())
	tt = ui.NewTester(d.view, 1240, 820)
	for _, want := range []string{"Result", "wfl_0", "\"pass\": true"} {
		if !tt.HasText(want) {
			t.Errorf("done run: no %q in %q", want, tt.Texts())
		}
	}
	if tt.HasText("Cancel workflow") || tt.HasText("The workflow asks") {
		t.Error("a done run offers cancel or a question")
	}
}

// A push adds or replaces a run; a pushed task of the run in view, and
// the run itself, load it again.
func TestWorkflowPushes(t *testing.T) {
	d := testDesktop(t)
	d.apply(push(t, "workflow", protocol.Workflow{ID: "wfl_1", Title: "a", State: "running"}))
	d.apply(push(t, "workflow", protocol.Workflow{ID: "wfl_2", Title: "b", State: "running"}))
	d.apply(push(t, "workflow", protocol.Workflow{ID: "wfl_1", Title: "a", State: "waiting"}))
	runs := d.wf.list.Runs
	if len(runs) != 2 || runs[0].ID != "wfl_2" || runs[1].State != "waiting" {
		t.Fatalf("runs = %+v", runs)
	}

	// Without a client, a load sets the status: that shows that it ran.
	d.page, d.wf.sel = pageWorkflows, "wfl_1"
	d.apply(push(t, "task", protocol.Task{ID: "tsk_9", WorkflowID: "wfl_2", State: "done"}))
	if d.status != "" {
		t.Fatalf("a task of another run loaded the run: %q", d.status)
	}
	d.apply(push(t, "task", protocol.Task{ID: "tsk_9", WorkflowID: "wfl_1", State: "done"}))
	if d.status == "" {
		t.Fatal("a task of the run in view did not load the run")
	}
}

func TestOpenQuestionAndResult(t *testing.T) {
	q, ok := openQuestion(*sampleRun(time.Now()).Run)
	if !ok || q.Prompt == "" || len(q.Options) != 2 {
		t.Fatalf("openQuestion = %+v %v", q, ok)
	}
	if _, ok := openQuestion(*sampleDoneRun(time.Now()).Run); ok {
		t.Fatal("a done run has an open question")
	}
	for raw, want := range map[string]string{
		`"plain text"`: "plain text",
		`{"a":1}`:      "{\n  \"a\": 1\n}",
		`42`:           "42",
	} {
		if got := resultText(json.RawMessage(raw)); got != want {
			t.Errorf("resultText(%s) = %q, want %q", raw, got, want)
		}
	}
}

// A saved script shows how to run it, and refuses args that are not JSON.
func TestSavedWorkflow(t *testing.T) {
	d := sampleDesktop(t)
	s := d.wf.list.Saved[0]
	d.page, d.wf.sel, d.wf.item = pageWorkflows, s.Name, protocol.WorkflowItem{Saved: &s}
	tt := ui.NewTester(d.view, 1240, 820)
	for _, want := range []string{"Run it", "A change that must pass a review", "Phase 2: Review", "Up to 5 rounds", "Arguments", "Run"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	d.wf.args = "{not json"
	d.startSaved(s.Name)
	if d.status != "The arguments are not valid JSON." || d.wf.busy {
		t.Fatalf("status = %q, busy = %v", d.status, d.wf.busy)
	}
}

// A run shows its facts in a strip; a failed run says what failed and
// offers to run again from it.
func TestRunSummary(t *testing.T) {
	d := sampleDesktop(t)
	d.page, d.wf.sel, d.wf.item = pageWorkflows, "wfl_2", sampleDoneRun(time.Now())
	tt := ui.NewTester(d.view, 1240, 820)
	for _, want := range []string{"Status", "Done", "Duration", "Steps", "1 task", "Started by", "You", "wfl_2", "Resumes", "wfl_0"} {
		if !tt.HasText(want) {
			t.Errorf("done: no %q in %q", want, tt.Texts())
		}
	}
	d.wf.sel, d.wf.item = "wfl_4", sampleFailedRun(time.Now())
	tt = ui.NewTester(d.view, 1240, 820)
	for _, want := range []string{"“review again” failed in round 2", "go test: 3 failures", "Open task", "Run again from here", "stopped in round 2", "1 failed"} {
		if !tt.HasText(want) {
			t.Errorf("failed: no %q in %q", want, tt.Texts())
		}
	}
}
