package main

import (
	"encoding/json"
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/locales"
)

// The progress of a run, in one of two views that the user picks: the
// graph (graph_view.go), nodes and edges from left to right, and the rail
// (rail_view.go), phases and steps down a line. Both draw the timeline
// that timeline.go builds; this file has what they share.

const (
	viewGraph = "graph"
	viewRail  = "rail"
	runFPS    = 30 // the frame rate of the motion while a run is live
)

// runView shows the progress of a run in the view of the user's choice.
func (d *desktop) runView(c *ui.Context, tl timeline) {
	pal := paletteFor(c)
	view := d.prefs.WorkflowView
	if view != viewRail {
		view = viewGraph
	}
	section(c, "progress", sectionSpec{title: locales.WorkflowProgress(), actions: func() {
		segs := []segment{{viewGraph, locales.WorkflowGraph()}, {viewRail, locales.WorkflowRail()}}
		if v, ok := segmented(c, "view", view, segs); ok {
			d.prefs.WorkflowView = v
			d.savePrefs()
		}
	}}, func() {
		if view == viewRail {
			d.railView(c, pal, tl)
			return
		}
		d.graphView(c, pal, tl)
	})
}

// phaseState is the state of a phase: planned (skipped once the run
// stopped), or the state of its liveliest step.
func phaseState(tl timeline, ph tlPhase) string {
	if ph.planned {
		if tl.live {
			return "planned"
		}
		return "skipped"
	}
	st := "done"
	for _, s := range ph.steps {
		switch s.state {
		case "failed":
			return "failed"
		case "ask":
			st = "ask"
		case "live":
			if st != "ask" {
				st = "live"
			}
		case "stopped":
			if st == "done" {
				st = "stopped"
			}
		}
	}
	return st
}

// stateColor is the color of a state: blue while it works, amber while it
// asks, red when it failed, gray else.
func stateColor(pal *palette, state string) ui.Color {
	switch state {
	case "live":
		return pal.accent
	case "ask":
		return pal.attention
	case "failed":
		return pal.danger
	}
	return pal.textFaint
}

// waits tells whether a state still waits: its node moves.
func waits(state string) bool { return state == "live" || state == "ask" }

// iconState is the statusIcon state of a step state.
func iconState(state string) string {
	switch state {
	case "live":
		return "running"
	case "ask":
		return "waiting"
	case "stopped", "skipped":
		return "cancelled"
	case "planned":
		return "queued"
	}
	return state
}

// kindIcon is the icon of a kind of step.
func kindIcon(kind string) *ui.SVG {
	switch kind {
	case "task":
		return agentIcon
	case "reply":
		return replyIcon
	case "ask":
		return questionIcon
	case "sleep":
		return clockIcon
	case "agents":
		return peopleIcon
	case "recall":
		return bookIcon
	}
	return toolIcon
}

// stepTime is how long a step took, or has waited so far.
func stepTime(tl timeline, s tlStep) string { return span(tl.to(s, time.Now()) - s.from) }

// stepError is the error of a failed step: its output, a JSON string.
func stepError(s tlStep) string {
	var msg string
	_ = json.Unmarshal(s.Output, &msg)
	return msg
}

// halo paints a ring that grows and fades around (cx, cy), once every
// 2.4s: the mark of a node that waits.
func halo(p *ui.Painter, cx, cy, r float32, col ui.Color) {
	p.AnimationFrameRate(runFPS)
	t := float32(p.Now().UnixMilli()%2400) / 2400
	var ring ui.Path
	ring.Circle(cx, cy, r*(1+t*1.4))
	p.FillPath(&ring, col.Alpha(0.4*(1-t)))
}

// comet paints a light that runs along the line from (x0, y0) to (x1, y1)
// once every 1.6s, toward the node that works.
func comet(p *ui.Painter, x0, y0, x1, y1 float32, col ui.Color) {
	p.AnimationFrameRate(runFPS)
	t := float32(p.Now().UnixMilli()%1600) / 1600
	t = 1 - float32(math.Pow(float64(1-t), 2)) // ease out
	const tail = 0.35
	a, b := max(t-tail, 0), t
	p.Line(x0+(x1-x0)*a, y0+(y1-y0)*a, x0+(x1-x0)*b, y0+(y1-y0)*b, 2, col.Alpha(0.5))
	p.Line(x0+(x1-x0)*(b-tail/4), y0+(y1-y0)*(b-tail/4), x0+(x1-x0)*b, y0+(y1-y0)*b, 2.5, col)
}

// diamondMark is the mark of a phase: a small diamond, dashed while the
// phase is planned.
func diamondMark(c *ui.Context, col ui.Color, planned bool) ui.Element {
	return ui.Box(c).Size(12, 12).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy, k := r.X+r.W/2, r.Y+r.H/2, float32(5)
		d := (&ui.Path{}).MoveTo(cx, cy-k).LineTo(cx+k, cy).LineTo(cx, cy+k).LineTo(cx-k, cy).Close()
		if planned {
			p.StrokePath(d.Dash(0, 2, 2), 1.2, col)
			return
		}
		p.FillPath(d, col)
	})
}

// loopChip is what a loop says of itself, as "round 2 of 5", and its
// mark.
func loopChip(b tlBlock) (string, mark) {
	whole := b.all()
	n, most := whole.lastRound()
	switch st := whole.loopState(); {
	case st == "failed":
		return locales.WorkflowStoppedIn(n), markBad
	case st == "live" && most > 0:
		return locales.WorkflowRoundOf(n, most), markAccent
	case st == "live":
		return locales.WorkflowRoundN(n), markAccent
	case most > 0 && n >= most:
		return locales.WorkflowRoundsOf(n, most), markWarn // the script used all its rounds
	case most > 0:
		return locales.WorkflowRoundsOf(n, most), markQuiet
	}
	return locales.WorkflowRoundsDone(n), markQuiet
}

// loopName is the name of a loop; a loop that repliesAsRounds found has
// none of its own.
func loopName(b tlBlock) string {
	if b.loop == "" {
		return locales.WorkflowRounds()
	}
	return b.loop
}

// liveRound says the round of the loop that works now, as "round 2 of 5";
// "" when no loop works.
func (tl timeline) liveRound() string {
	for _, ph := range tl.phases {
		for _, b := range ph.blocks {
			if !b.inLoop || b.cont || b.all().loopState() != "live" {
				continue
			}
			n, most := b.all().lastRound()
			if most > 0 {
				return locales.WorkflowRoundOf(n, most)
			}
			return locales.WorkflowRoundN(n)
		}
	}
	return ""
}
