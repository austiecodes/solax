package main

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/austiecodes/solax/pkg/protocol"
)

// The timeline of a workflow run: each step is a bar in time, under its
// phase, and the rounds of a loop are bands behind their steps. This file
// builds it from a run; timeline_view.go draws it.

// tlStep is a step on the timeline.
type tlStep struct {
	protocol.WorkflowStep
	state    string        // done | failed | live | ask | stopped
	from, to time.Duration // from the start of the run; to is not final while the step waits
	round    int           // the round of the step's loop; 0 out of a loop
}

// waiting tells whether the step still waits: its bar grows with the clock.
func (s tlStep) waiting() bool { return s.state == "live" || s.state == "ask" }

// tlPhase is a phase and its steps. A phase of the script's meta with no
// steps yet is planned, or not reached when the run stopped.
type tlPhase struct {
	title, detail string
	steps         []tlStep
	blocks        []tlBlock // the steps, split by loop
	planned       bool
}

// timeline is a run in time.
type timeline struct {
	start, end time.Time // end is the end of a run that stopped
	state      string    // the state of the run
	live       bool
	phases     []tlPhase
}

// elapsed is the length of the run at now.
func (tl timeline) elapsed(now time.Time) time.Duration {
	if !tl.live {
		now = tl.end
	}
	return max(now.Sub(tl.start), 0)
}

// to is the end of the bar of s at now.
func (tl timeline) to(s tlStep, now time.Time) time.Duration {
	if s.waiting() {
		return max(tl.elapsed(now), s.from)
	}
	return s.to
}

// buildTimeline puts the steps of w in time. meta is the phases of its
// saved script, if any: those with no steps yet show as planned.
func buildTimeline(w protocol.Workflow, meta []protocol.WorkflowPhase) timeline {
	tl := timeline{start: w.CreatedAt, end: w.UpdatedAt, state: w.State, live: workflowLive(w.State)}
	steps := make([]tlStep, 0, len(w.Steps))
	for _, st := range w.Steps {
		s := tlStep{WorkflowStep: st, state: st.State, round: st.Round}
		if st.State == "pending" {
			switch {
			case !tl.live:
				s.state = "stopped"
			case st.Kind == "ask":
				s.state = "ask"
			default:
				s.state = "live"
			}
		}
		s.from = offset(tl.start, st.CreatedAt)
		s.to = max(offset(tl.start, st.UpdatedAt), s.from)
		if s.state == "stopped" {
			s.to = max(offset(tl.start, tl.end), s.from)
		}
		if st.Loop == "" {
			s.round = 0
		}
		steps = append(steps, s)
	}
	if !slices.ContainsFunc(steps, func(s tlStep) bool { return s.Loop != "" }) {
		repliesAsRounds(steps)
	}

	// A step with no phase stays in the phase of the step before it.
	phase, at := "", map[string]int{}
	for _, s := range steps {
		if s.Phase != "" {
			phase = s.Phase
		}
		i, ok := at[phase]
		if !ok {
			i = len(tl.phases)
			at[phase] = i
			tl.phases = append(tl.phases, tlPhase{title: phase})
		}
		tl.phases[i].steps = append(tl.phases[i].steps, s)
	}
	for i := range tl.phases {
		tl.phases[i].blocks = blocks(tl.phases[i].steps)
	}
	linkLoops(tl.phases)
	for _, p := range meta {
		if i, ok := at[p.Title]; ok {
			tl.phases[i].detail = p.Detail
			continue
		}
		if w.State != "done" {
			tl.phases = append(tl.phases, tlPhase{title: p.Title, detail: p.Detail, planned: true})
		}
	}
	return tl
}

// offset is the time from start to t; 0 when t is not set.
func offset(start, t time.Time) time.Duration {
	if t.IsZero() {
		return 0
	}
	return max(t.Sub(start), 0)
}

// repliesAsRounds finds a loop in a run whose script does not call
// wf.round: a task that gets replies is round 1, and each reply to it is
// one more round. The steps between the first and the last of those stay
// in the round before them. The loop has no name.
func repliesAsRounds(steps []tlStep) {
	replied := map[string]bool{}
	for _, s := range steps {
		if s.Kind == "reply" {
			replied[s.Target] = true
		}
	}
	if len(replied) == 0 {
		return
	}
	n := map[string]int{}
	first, last := -1, -1
	for i := range steps {
		s := &steps[i]
		switch {
		case s.Kind == "task" && replied[s.Target]:
			s.round = 1
		case s.Kind == "reply":
			n[s.Target]++
			s.round = n[s.Target] + 1
		default:
			continue
		}
		if first < 0 {
			first = i
		}
		last = i
	}
	for i := first + 1; i <= last; i++ {
		if steps[i].round == 0 {
			steps[i].round = steps[i-1].round
		}
	}
}

// tlBlock is one step out of a loop, or the steps of a phase in a row
// that are in one loop.
type tlBlock struct {
	loop   string // the loop's name; "" when repliesAsRounds found it
	inLoop bool
	steps  []tlStep
	cont   bool     // the loop goes on from the phase before
	whole  []tlStep // the first block of a loop: the steps of the loop in all phases
}

// all is the block that sums up the loop of b: its steps in all phases.
func (b tlBlock) all() tlBlock {
	if b.whole != nil {
		b.steps = b.whole
	}
	return b
}

// linkLoops finds the loops that go on from one phase to the next: the
// first block of a phase goes on when the last block of the phase before
// is in the same loop.
func linkLoops(phases []tlPhase) {
	var head *tlBlock
	for i := range phases {
		bs := phases[i].blocks
		if len(bs) == 0 {
			continue
		}
		if b := &bs[0]; head != nil && b.inLoop && b.loop == head.loop {
			b.cont = true
			head.whole = append(head.whole, b.steps...)
			if len(bs) > 1 {
				head = nil
			}
		} else {
			head = nil
		}
		if last := &bs[len(bs)-1]; last.inLoop && !last.cont {
			head = last
			head.whole = slices.Clone(last.steps)
		}
	}
}

// blocks splits the steps of a phase into blocks.
func blocks(steps []tlStep) []tlBlock {
	var out []tlBlock
	for _, s := range steps {
		in := s.round > 0
		if n := len(out); in && n > 0 && out[n-1].inLoop && out[n-1].loop == s.Loop {
			out[n-1].steps = append(out[n-1].steps, s)
			continue
		}
		out = append(out, tlBlock{loop: s.Loop, inLoop: in, steps: []tlStep{s}})
	}
	return out
}

// tlRound is one round of a loop in time.
type tlRound struct {
	n        int
	from, to time.Duration
	waiting  bool // a step of the round still waits
}

// rounds are the rounds of a loop block, in order.
func (b tlBlock) rounds(tl timeline, now time.Time) []tlRound {
	var out []tlRound
	for _, s := range b.steps {
		to := tl.to(s, now)
		i := slices.IndexFunc(out, func(r tlRound) bool { return r.n == s.round })
		if i < 0 {
			out = append(out, tlRound{n: s.round, from: s.from, to: to, waiting: s.waiting()})
			continue
		}
		r := &out[i]
		r.from, r.to, r.waiting = min(r.from, s.from), max(r.to, to), r.waiting || s.waiting()
	}
	slices.SortFunc(out, func(a, b tlRound) int { return a.n - b.n })
	return out
}

// loopState is the state of a loop block: live while a step waits, failed
// when a step failed, else done.
func (b tlBlock) loopState() string {
	st := "done"
	for _, s := range b.steps {
		switch {
		case s.state == "failed":
			return "failed"
		case s.waiting():
			st = "live"
		}
	}
	return st
}

// lastRound is the highest round of the block, and the most rounds that
// the script gives (0 when it gives none).
func (b tlBlock) lastRound() (n, most int) {
	for _, s := range b.steps {
		n, most = max(n, s.round), max(most, s.MaxRounds)
	}
	return n, most
}

// span says a duration in a short form: "4s", "2m 14s", "1h 05m".
func span(d time.Duration) string {
	s := int(d.Round(time.Second) / time.Second)
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm %02ds", s/60, s%60)
	}
	return fmt.Sprintf("%dh %02dm", s/3600, s/60%60)
}

// stages splits steps into runs of steps that ran at the same time, as
// wf.parallel makes them: a step that began before every step of a stage
// ended joins that stage. Steps of other rounds never share one.
func stages(steps []tlStep) [][]tlStep {
	var out [][]tlStep
	for _, s := range steps {
		if n := len(out); n > 0 && out[n-1][0].round == s.round && s.from < stageEnd(out[n-1]) {
			out[n-1] = append(out[n-1], s)
			continue
		}
		out = append(out, []tlStep{s})
	}
	return out
}

// stageEnd is when the first step of a stage that ended, ended; a stage
// whose steps all wait has no end yet.
func stageEnd(stage []tlStep) time.Duration {
	end := time.Duration(math.MaxInt64)
	for _, s := range stage {
		if !s.waiting() {
			end = min(end, s.to)
		}
	}
	return end
}
