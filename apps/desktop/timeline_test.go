package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// A step with no phase stays in the phase before it; the phases of the
// saved script with no steps are planned while the run is live, and left
// out once it is done.
func TestBuildTimelinePhases(t *testing.T) {
	w := *sampleRun(time.Now()).Run
	meta := sampleWorkflows(time.Now()).Saved[0].Meta.Phases
	tl := buildTimeline(w, meta)
	var titles []string
	for _, p := range tl.phases {
		titles = append(titles, p.title)
	}
	if !slices.Equal(titles, []string{"Code", "Review", "Ship"}) || !tl.phases[2].planned || tl.phases[1].detail != "Up to 5 rounds" {
		t.Fatalf("phases = %+v", tl.phases)
	}
	ask := tl.phases[1].steps[3]
	if ask.Kind != "ask" || ask.state != "ask" || !ask.waiting() {
		t.Fatalf("ask = %+v", ask)
	}

	w.Steps[4].Phase = "" // a step of an older server: no phase
	if tl := buildTimeline(w, meta); len(tl.phases[1].steps) != 4 {
		t.Fatalf("a step with no phase left Review: %+v", tl.phases)
	}

	w.State = "done"
	if tl := buildTimeline(w, meta); len(tl.phases) != 2 {
		t.Fatalf("a done run shows planned phases: %+v", tl.phases)
	}
}

// A pending step of a run that stopped is stopped, and ends with the run.
func TestBuildTimelineStopped(t *testing.T) {
	w := *sampleLiveRun(time.Now()).Run
	w.State, w.UpdatedAt = "cancelled", w.CreatedAt.Add(5*time.Minute)
	tl := buildTimeline(w, nil)
	s := tl.phases[1].steps[0]
	if s.state != "stopped" || s.waiting() || s.to != 5*time.Minute {
		t.Fatalf("step = %+v", s)
	}
	if got := tl.elapsed(time.Now().Add(time.Hour)); got != 5*time.Minute {
		t.Fatalf("elapsed = %v, want the length of the run", got)
	}
}

// With no wf.round, a task that gets replies is round 1, and each reply
// one more round; the steps between keep the round before them.
func TestRepliesAsRounds(t *testing.T) {
	st := func(kind, target string) tlStep {
		return tlStep{WorkflowStep: protocol.WorkflowStep{Kind: kind, Target: target}}
	}
	steps := []tlStep{
		st("agents", ""), st("task", "a"), st("task", "b"), st("reply", "a"), st("recall", "x"),
		st("reply", "b"), st("reply", "a"), st("task", "c"),
	}
	repliesAsRounds(steps)
	var got []int
	for _, s := range steps {
		got = append(got, s.round)
	}
	if want := []int{0, 1, 1, 2, 2, 2, 3, 0}; !slices.Equal(got, want) {
		t.Fatalf("rounds = %v, want %v", got, want)
	}

	if tl := buildTimeline(*sampleFailedRun(time.Now()).Run, nil); tl.phases[0].steps[3].round != 2 {
		t.Fatalf("failed run rounds = %+v", tl.phases[0].steps)
	}
}

// The steps of a loop make one block with a band for each round; a round
// waits while one of its steps waits.
func TestLoopBlocks(t *testing.T) {
	now := time.Now()
	tl := buildTimeline(*sampleRun(now).Run, nil)
	bs := blocks(tl.phases[1].steps)
	if len(bs) != 2 || !bs[0].inLoop || bs[0].loop != "fix" || len(bs[0].steps) != 3 || bs[1].inLoop {
		t.Fatalf("blocks = %+v", bs)
	}
	rs := bs[0].rounds(tl, now)
	if len(rs) != 2 || rs[1].n != 2 || rs[1].from != 4*time.Minute+15*time.Second || rs[1].to != 7*time.Minute+46*time.Second {
		t.Fatalf("rounds = %+v", rs)
	}
	if n, most := bs[0].lastRound(); n != 2 || most != 3 || bs[0].loopState() != "done" {
		t.Fatalf("last round = %d of %d, state %s", n, most, bs[0].loopState())
	}

	live := buildTimeline(*sampleLiveRun(now).Run, nil)
	lb := blocks(live.phases[1].steps)[0]
	if lb.loopState() != "live" || !lb.rounds(live, now)[0].waiting || live.liveRound() != "round 1 of 3" {
		t.Fatalf("live loop = %+v, round %q", lb, live.liveRound())
	}
}

// Steps whose times overlap make one stage; a step that began after
// one of a stage ended starts a new one.
func TestStages(t *testing.T) {
	sec := func(n int) time.Duration { return time.Duration(n) * time.Second }
	st := func(from, to int, state string) tlStep {
		return tlStep{state: state, from: sec(from), to: sec(to)}
	}
	got := stages([]tlStep{st(0, 5, "done"), st(5, 20, "done"), st(6, 9, "done"), st(20, 20, "live"), st(21, 21, "live")})
	var sizes []int
	for _, g := range got {
		sizes = append(sizes, len(g))
	}
	if !slices.Equal(sizes, []int{1, 2, 2}) {
		t.Fatalf("stages = %v", sizes)
	}
	if got := span(2*time.Minute + 14*time.Second); got != "2m 14s" {
		t.Fatalf("span = %q", got)
	}
}

// The graph puts parallel steps in one column, and a loop's columns in
// one loop with its rounds.
func TestGraphCols(t *testing.T) {
	cols, loops := graphCols(buildTimeline(*sampleLiveRun(time.Now()).Run, sampleWorkflows(time.Now()).Saved[0].Meta.Phases))
	if len(cols) != 4 || len(cols[2].steps) != 2 || cols[2].loop != 0 || !cols[3].plan {
		t.Fatalf("cols = %+v", cols)
	}
	if len(loops) != 1 || !slices.Equal(loops[0].rounds, []int{1}) {
		t.Fatalf("loops = %+v", loops)
	}
	_, loops = graphCols(buildTimeline(*sampleRun(time.Now()).Run, nil))
	if len(loops) != 1 || !slices.Equal(loops[0].rounds, []int{1, 2}) {
		t.Fatalf("loops = %+v", loops)
	}
}

// The rail's line is solid where the run went, runs into the step that
// works, and is dashed after it.
func TestRailItems(t *testing.T) {
	d := sampleDesktop(t)
	items := d.railItems(buildTimeline(*sampleLiveRun(time.Now()).Run, sampleWorkflows(time.Now()).Saved[0].Meta.Phases))
	var kinds []railKind
	for _, it := range items {
		kinds = append(kinds, it.kind)
	}
	// Code is done and folded; Review has a loop head and a stage; Ship is planned.
	if want := []railKind{railPhase, railPhase, railLoop, railStage, railPhase}; !slices.Equal(kinds, want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	if !items[0].folded || items[3].in != "live" || items[2].out != "done" || items[3].out != "future" || items[4].in != "future" {
		t.Fatalf("items = %+v", items)
	}
}

// A loop that goes on into the next phase has one head: the first block
// sums up the whole loop, and the later ones go on from it.
func TestLinkLoops(t *testing.T) {
	st := func(phase string, round int, state string) protocol.WorkflowStep {
		s := protocol.WorkflowStep{Kind: "task", Phase: phase, State: state, Round: round}
		if round > 0 {
			s.Loop, s.MaxRounds = "fix", 3
		}
		return s
	}
	w := protocol.Workflow{State: "running", Steps: []protocol.WorkflowStep{
		st("Code", 0, "done"), st("Code", 1, "done"),
		st("Review", 1, "done"), st("Review", 2, "done"),
		st("Ship", 3, "pending"),
	}}
	tl := buildTimeline(w, nil)
	code, review, ship := tl.phases[0].blocks, tl.phases[1].blocks, tl.phases[2].blocks
	if len(code) != 2 || code[1].cont || !review[0].cont || !ship[0].cont {
		t.Fatalf("blocks = %+v / %+v / %+v", code, review, ship)
	}
	whole := code[1].all()
	if n, most := whole.lastRound(); len(whole.steps) != 4 || n != 3 || most != 3 || whole.loopState() != "live" {
		t.Fatalf("whole loop = %+v", whole)
	}
	if got := tl.liveRound(); got != "round 3 of 3" {
		t.Fatalf("liveRound = %q", got)
	}
}

// followSample gives the running task of the live sample run a session
// in which a tool call runs.
func followSample(d *desktop) {
	d.tasks[0].SessionID = "ses_1"
	d.events["ses_1"] = []ir.Event{{Seq: 1, Type: ir.ItemStarted, At: time.Now(),
		Item: &ir.Item{ID: "t1", Kind: ir.ItemToolCall, Status: ir.StatusInProgress, Tool: shell("go test ./internal/auth/...")}}}
}

// A working bar says what its agent does now, as the task's transcript
// does.
func TestActivity(t *testing.T) {
	d := sampleDesktop(t)
	followSample(d)
	if got := d.activity("tsk_1"); !strings.Contains(got, "go test ./internal/auth/...") {
		t.Fatalf("activity = %q", got)
	}
	if got := d.activity("tsk_3"); got != "" {
		t.Fatalf("a done task has activity %q", got)
	}
	d.page, d.wf.sel, d.wf.item = pageWorkflows, "wfl_3", sampleLiveRun(time.Now())
	tt := ui.NewTester(d.view, 1240, 820)
	for _, want := range []string{"fix", "round 1 of 3", "planned"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
}
