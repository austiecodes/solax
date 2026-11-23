package main

import (
	"fmt"
	"slices"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/locales"
)

// The graph view of a run: a node for each step, from left to right in
// the order they ran, joined by edges. Steps that ran at the same time
// stand in one column. A loop is a frame that shows one round at a time,
// with tabs for its rounds and an edge back to its start; the edges into
// a step that works flow. The view scrolls sideways and follows the step
// that works.

const (
	gNodeW    = 196
	gNodeH    = 60
	gGapX     = 60 // between columns: room for the edges
	gGapY     = 14 // between the nodes of a column
	gPad      = 22
	gHeadH    = 26 // the phase names over the nodes
	gLoopHead = 38 // the head of a loop frame: its name and rounds
	gLoopPad  = 14
	gReturnH  = 40 // the edge back to the start of a loop, under its frame
)

// gCol is a column of the graph: the steps of one stage, or a planned
// phase.
type gCol struct {
	steps []tlStep
	phase int // the index of its phase in the timeline
	loop  int // the index of its loop; -1 out of a loop
	round int
	plan  bool // a planned phase: one ghost node
}

// gLoop is a loop of the graph.
type gLoop struct {
	key    string // the loop's key in wfState.rounds
	head   tlBlock
	rounds []int
}

// graphCols lays the steps of tl out in columns, and finds the loops.
func graphCols(tl timeline) ([]gCol, []gLoop) {
	var cols []gCol
	var loops []gLoop
	for pi, ph := range tl.phases {
		if ph.planned {
			cols = append(cols, gCol{phase: pi, loop: -1, plan: true})
			continue
		}
		for _, b := range ph.blocks {
			li := -1
			if b.inLoop {
				if !b.cont || len(loops) == 0 {
					var rounds []int
					for _, s := range b.all().steps {
						if !slices.Contains(rounds, s.round) {
							rounds = append(rounds, s.round)
						}
					}
					slices.Sort(rounds)
					loops = append(loops, gLoop{key: fmt.Sprintf("%d:%s", pi, b.loop), head: b, rounds: rounds})
				}
				li = len(loops) - 1
			}
			for _, stg := range stages(b.steps) {
				cols = append(cols, gCol{steps: stg, phase: pi, loop: li, round: stg[0].round})
			}
		}
	}
	return cols, loops
}

// gPlaced is a column that shows, with its x.
type gPlaced struct {
	gCol
	x float32
}

// gFrame is a loop frame as it shows.
type gFrame struct {
	loop        int
	left, right float32
}

// graphView shows a run as a graph.
func (d *desktop) graphView(c *ui.Context, pal *palette, tl timeline) {
	cols, loops := graphCols(tl)
	if d.wf.rounds == nil {
		d.wf.rounds = map[string]int{}
	}
	shown := func(l gLoop) int {
		if n, ok := d.wf.rounds[l.key]; ok && slices.Contains(l.rounds, n) {
			return n
		}
		return l.rounds[len(l.rounds)-1]
	}

	// Place the columns that show: a loop shows the columns of one round.
	var placed []gPlaced
	var frames []gFrame
	x, rows, prev := float32(gPad), 1, -1
	for _, col := range cols {
		if col.loop >= 0 && col.round != shown(loops[col.loop]) {
			continue
		}
		if col.loop != prev {
			if prev >= 0 {
				x += gLoopPad // close the frame before
			}
			if col.loop >= 0 {
				frames = append(frames, gFrame{loop: col.loop, left: x})
				x += gLoopPad
			}
		}
		placed = append(placed, gPlaced{gCol: col, x: x})
		if col.loop >= 0 {
			frames[len(frames)-1].right = x + gNodeW + gLoopPad
		}
		x += gNodeW + gGapX
		rows, prev = max(rows, len(col.steps)), col.loop
	}
	if prev >= 0 {
		x += gLoopPad
	}
	width := x - gGapX + gPad
	top := float32(gPad + gHeadH)
	if len(frames) > 0 {
		top += gLoopHead
	}
	nodesH := float32(rows)*(gNodeH+gGapY) - gGapY
	height := top + nodesH + gPad
	if len(frames) > 0 {
		height += gLoopPad + gReturnH
	}
	nodeY := func(col gPlaced, k int) float32 {
		n := max(len(col.steps), 1)
		h := float32(n)*(gNodeH+gGapY) - gGapY
		return top + (nodesH-h)/2 + float32(k)*(gNodeH+gGapY)
	}

	ui.ScrollHorizontal(c.Key("graph")).Children(func() {
		ui.Box(c).Size(width, height).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
			d.paintGraph(p, pal, tl, r, placed, frames, loops, nodeY, top+nodesH)
		}).Children(func() {
			last := -1
			for i, col := range placed {
				if col.phase != last {
					last = col.phase
					phaseLabel(c, pal, tl, i, tl.phases[col.phase], col.x)
				}
			}
			for _, f := range frames {
				l := loops[f.loop]
				d.loopHead(c, pal, l, shown(l), f.left, top-gLoopHead)
				// What the loop says of itself, on its edge back.
				text, m := loopChip(l.head)
				ui.Row(c.Key("loop-chip-" + l.key)).Absolute().Left(f.left).Width(f.right - f.left).
					Top(top + nodesH + gLoopPad + gReturnH - 20).Justify(ui.Center).Children(func() {
					ui.Text(c, "↻ "+text).FontSize(11.5).FontWeight(600).TextColor(markColor(pal, l.head)).SingleLine().
						Padding(2, 9).Radius(10).Background(pal.card).Border(1, m.color(pal).Alpha(0.4))
				})
			}
			for _, col := range placed {
				if col.plan {
					ghostNode(c, pal, tl.phases[col.phase], col.x, nodeY(col, 0))
					continue
				}
				for k, s := range col.steps {
					n := d.graphNode(c, pal, tl, s, col.x, nodeY(col, k))
					if s.waiting() && d.wf.focus != s.Key {
						d.wf.focus = s.Key
						n.ScrollIntoView(ui.ScrollIntoViewOptions{Inline: ui.ScrollCenter, Block: ui.ScrollNearest, Animation: &ui.Smooth})
					}
				}
			}
		})
	})
}

// paintGraph paints what is under the nodes: a grid of dots, the phase
// names, the loop frames and their edges back, and the edges.
func (d *desktop) paintGraph(p *ui.Painter, pal *palette, tl timeline, r ui.Rect, placed []gPlaced, frames []gFrame, loops []gLoop,
	nodeY func(gPlaced, int) float32, bottom float32) {
	// The dots, only where they show.
	vis := p.Visible()
	for y := r.Y + 9; y < r.Y+r.H; y += 18 {
		if y < vis.Y || y > vis.Y+vis.H {
			continue
		}
		for x := r.X + 9; x < r.X+r.W; x += 18 {
			if x >= vis.X && x <= vis.X+vis.W {
				p.Fill(ui.Rect{X: x, Y: y, W: 1.4, H: 1.4}, pal.text.Alpha(0.07), 0.7)
			}
		}
	}

	// The phase names, over the first column of each phase, with a line
	// between phases.
	last := -1
	for i, col := range placed {
		if col.phase == last {
			continue
		}
		last = col.phase
		x := r.X + col.x
		if i > 0 {
			sep := (&ui.Path{}).MoveTo(x-gGapX/2, r.Y+gPad).LineTo(x-gGapX/2, r.Y+r.H-gPad)
			p.StrokePath(sep.Dash(0, 3, 5), 1, pal.divider)
		}
	}

	// The loop frames, and the edge from the end of each back to its
	// start.
	for _, f := range frames {
		col := markColor(pal, loops[f.loop].head)
		fr := ui.Rect{X: r.X + f.left, Y: r.Y + gPad + gHeadH, W: f.right - f.left, H: bottom - (gPad + gHeadH) + gLoopPad}
		p.Fill(fr, col.Alpha(0.035), 14)
		p.Stroke(fr, col.Alpha(0.35), 14, 1)
		xr, xl, yb := fr.X+fr.W-40, fr.X+40, fr.Y+fr.H
		back := (&ui.Path{}).MoveTo(xr, yb).CubeTo(xr, yb+gReturnH-6, xl, yb+gReturnH-6, xl, yb+3)
		if waits(loops[f.loop].head.all().loopState()) {
			p.AnimationFrameRate(runFPS)
			off := -float32(p.Now().UnixMilli()%900) / 900 * 12
			p.StrokePath(back.Dash(off, 6, 6), 1.6, col)
		} else {
			p.StrokePath(back, 1.4, col.Alpha(0.6))
		}
		head := (&ui.Path{}).MoveTo(xl-5, yb+10).LineTo(xl, yb+3).LineTo(xl+5, yb+10)
		p.StrokePath(head, 1.6, col)
	}

	// The edges: each node of a column to each node of the next. An edge
	// takes the state of the node it goes into.
	for i := 1; i < len(placed); i++ {
		a, b := placed[i-1], placed[i]
		for ka := range max(len(a.steps), 1) {
			for kb := range max(len(b.steps), 1) {
				st := "planned"
				if !b.plan {
					st = b.steps[kb].state
				}
				if !a.plan && len(a.steps) > 0 && waits(a.steps[ka].state) && !waits(st) {
					st = "planned" // nothing after a step that works has run
				}
				x1, y1 := r.X+a.x+gNodeW, r.Y+nodeY(a, ka)+gNodeH/2
				x2, y2 := r.X+b.x, r.Y+nodeY(b, kb)+gNodeH/2
				graphEdge(p, pal, st, x1, y1, x2, y2)
			}
		}
	}

	// The halos of the nodes that wait, at their state marks.
	for _, col := range placed {
		for k, s := range col.steps {
			if s.waiting() {
				halo(p, r.X+col.x+gNodeW-2, r.Y+nodeY(col, k)+2, 8, stateColor(pal, s.state))
			}
		}
	}
}

// graphEdge paints an edge from (x1, y1) to (x2, y2) by the state of the
// node it goes into.
func graphEdge(p *ui.Painter, pal *palette, state string, x1, y1, x2, y2 float32) {
	dx := (x2 - x1) / 2
	edge := func() *ui.Path { return (&ui.Path{}).MoveTo(x1, y1).CubeTo(x1+dx, y1, x2-dx, y2, x2, y2) }
	switch state {
	case "live", "ask":
		col := stateColor(pal, state)
		p.StrokePath(edge(), 6, col.Alpha(0.14))
		p.AnimationFrameRate(runFPS)
		off := -float32(p.Now().UnixMilli()%900) / 900 * 12
		p.StrokePath(edge().Dash(off, 6, 6), 2, col)
	case "failed":
		p.StrokePath(edge(), 1.6, pal.danger.Alpha(0.75))
	case "planned", "skipped", "stopped":
		p.StrokePath(edge().Dash(0, 4, 5), 1.4, pal.textFaint.Alpha(0.6))
	default:
		p.StrokePath(edge(), 1.5, pal.text.Alpha(0.24))
	}
	// A dot where the edge goes in.
	var in ui.Path
	in.Circle(x2, y2, 2.5)
	p.FillPath(&in, pal.text.Alpha(0.3))
}

// graphNode is the node of a step: its kind, its text, its agent and
// time, and its state at the corner. A task opens on a click.
func (d *desktop) graphNode(c *ui.Context, pal *palette, tl timeline, s tlStep, x, y float32) ui.Element {
	text, agent := d.stepText(s.WorkflowStep)
	if s.Kind == "ask" {
		text = locales.WorkflowWaitsForAnswer()
		if s.state == "done" {
			text, _ = d.stepText(s.WorkflowStep)
		}
	}
	col := stateColor(pal, s.state)
	hot := waits(s.state) || s.state == "failed"
	b := ui.ButtonBase(c.Key(s.Key)).Absolute().Left(x).Top(y).Size(gNodeW, gNodeH).Radius(12).Label(text).
		Padding(0, 12, 0, 10).Gap(10).AlignItems(ui.Center).Background(pal.card).Border(1, pal.divider).
		Shadow(0, 4, 14, -6, pal.rowShadow)
	switch {
	case hot:
		b.Border(1, col.Alpha(0.6)).Shadow(0, 0, 22, -2, col.Alpha(0.4))
	case b.Hovered():
		b.Border(1, pal.text.Alpha(0.25))
	}
	b.Children(func() {
		tile, ink := pal.chip, pal.textMuted
		if hot {
			tile, ink = col.Alpha(0.16), col
		}
		ui.Box(c).Size(30, 30).Radius(8).Center().Background(tile).Shrink(0).Children(func() {
			ui.Icon(c, kindIcon(s.Kind)).Size(14, 14).TextColor(ink)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			label := text
			if s.round > 0 {
				label = fmt.Sprintf("R%d · %s", s.round, text)
			}
			ui.Text(c, label).FontSize(12.5).FontWeight(600).TextColor(pal.text).SingleLine().Ellipsis("…")
			act := ""
			if s.state == "live" {
				act = d.activity(s.Target)
			}
			ui.Box(c).Height(15).Draw(func(p *ui.Painter, r ui.Rect) {
				if s.waiting() {
					p.AnimationFrameRate(2)
				}
				spans := []ui.Span{}
				if agent != "" {
					spans = append(spans, ui.Span{Text: agent + " · ", Size: 11.5, Color: pal.textFaint})
				}
				tcol := pal.textFaint
				if hot {
					tcol = col
				}
				spans = append(spans, ui.Span{Text: span(tl.to(s, p.Now()) - s.from), Size: 11.5, Color: tcol})
				if act != "" {
					spans = append(spans, ui.Span{Text: " · " + act, Font: "monospace", Size: 11, Color: pal.textMuted})
				}
				p.Clip(r, 0, func() { p.RichText(r.X, r.Y, 0, spans...) })
			})
		})
		ui.Box(c).Absolute().Top(-7).Left(gNodeW-11).Size(18, 18).Radius(9).Center().Background(pal.card).Children(func() {
			statusIcon(c, pal, iconState(s.state), false, 15)
		})
	})
	if (s.Kind == "task" || s.Kind == "reply") && s.Target != "" && b.Clicked() {
		d.openTask(s.Target)
	}
	return b
}

// ghostNode is a planned phase: a dashed node with its name and what it
// will do.
func ghostNode(c *ui.Context, pal *palette, ph tlPhase, x, y float32) {
	ui.Row(c.Key("ghost-"+ph.title)).Absolute().Left(x).Top(y).Size(gNodeW, gNodeH).Padding(0, 12).Gap(10).AlignItems(ui.Center).
		Draw(func(p *ui.Painter, r ui.Rect) { p.StrokeDashed(r, pal.textFaint.Alpha(0.6), 12, 1.2) }).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, ph.title).FontSize(12.5).FontWeight(600).TextColor(pal.textFaint).SingleLine().Ellipsis("…")
			detail := ph.detail
			if detail == "" {
				detail = locales.WorkflowPlanned()
			}
			ui.Text(c, detail).FontSize(11.5).TextColor(pal.textFaint).SingleLine().Ellipsis("…")
		})
	})
}

// loopHead is the head of a loop frame: its name and a tab for each
// round, to show that round's steps.
func (d *desktop) loopHead(c *ui.Context, pal *palette, l gLoop, shown int, x, y float32) {
	col := markColor(pal, l.head)
	ui.Row(c.Key("loop-" + l.key)).Absolute().Left(x + 12).Top(y + gHeadH/2 + 4).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, reloadIcon).Size(12, 12).TextColor(col).Shrink(0)
		ui.Text(c, loopName(l.head)).FontSize(12.5).FontWeight(600).TextColor(pal.text).SingleLine()
		if len(l.rounds) < 2 {
			return
		}
		ui.Row(c).Gap(2).Padding(2).Radius(7).Background(pal.chip).Children(func() {
			for _, n := range l.rounds {
				on := n == shown
				b := ui.ButtonBase(c.Key(n)).Padding(1, 7).Radius(5).Label(locales.WorkflowRoundN(n))
				ink := pal.textMuted
				if on {
					b.Background(pal.card)
					ink = col
				}
				b.Children(func() { ui.Textf(c, "R%d", n).FontSize(11).FontWeight(600).TextColor(ink).FontFeatures("tnum") })
				if b.Clicked() {
					d.wf.rounds[l.key] = n
				}
			}
		})
	})
}

// phaseLabel is the name of a phase and its time, over its first column.
func phaseLabel(c *ui.Context, pal *palette, tl timeline, i int, ph tlPhase, x float32) {
	if ph.title == "" {
		return // a script with no phases
	}
	st := phaseState(tl, ph)
	ink := pal.textMuted
	if st == "planned" || st == "skipped" {
		ink = pal.textFaint
	}
	meta := locales.WorkflowPlanned()
	switch {
	case st == "skipped":
		meta = locales.WorkflowNotReached()
	case len(ph.steps) > 0:
		meta = span(phaseTime(tl, ph))
	}
	ui.Row(c.Key(fmt.Sprintf("phase-%d", i))).Absolute().Left(x).Top(gPad - 2).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, ph.title).FontSize(12).FontWeight(600).TextColor(ink).SingleLine()
		ui.Text(c, meta).FontSize(11.5).TextColor(pal.textFaint).FontFeatures("tnum").SingleLine()
	})
}
