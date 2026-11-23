package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/locales"
)

// The rail view of a run: the stations of its phases across the top,
// then a line down its steps. A finished phase folds to one row; steps
// that ran at the same time sit side by side; a loop has a head with its
// round. A light runs down the line into the step that works.

const (
	railGut  = 40 // the gutter that the line runs down
	stationW = 112
)

// railItem is a row of the rail.
type railItem struct {
	kind   railKind
	phase  tlPhase
	steps  []tlStep // a step, or the steps of a stage
	loop   tlBlock  // the head of a loop
	folded bool
	state  string // the state of its node
	in     string // the line into its node: done, live, ask or future
	out    string // the line out of its node
}

type railKind int

const (
	railPhase railKind = iota
	railStep
	railStage // steps that ran at the same time
	railLoop  // the head of a loop
)

// railItems lays out the rows of the rail and the line between them.
func (d *desktop) railItems(tl timeline) []railItem {
	var items []railItem
	for _, ph := range tl.phases {
		st := phaseState(tl, ph)
		folded := len(tl.phases) > 1 && st == "done" && !d.wf.unfold[ph.title]
		if ph.title != "" {
			items = append(items, railItem{kind: railPhase, phase: ph, folded: folded, state: st})
		}
		if folded {
			continue
		}
		for _, b := range ph.blocks {
			if b.inLoop && !b.cont {
				items = append(items, railItem{kind: railLoop, loop: b, state: b.all().loopState()})
			}
			for _, stg := range stages(b.steps) {
				it := railItem{kind: railStep, steps: stg, state: stg[0].state}
				if len(stg) > 1 {
					it.kind, it.state = railStage, stageState(stg)
				}
				items = append(items, it)
			}
		}
	}
	for i := range items {
		it := &items[i]
		switch {
		case it.state == "planned" || it.state == "skipped":
			it.in = "future"
		case waits(it.state) && (it.kind == railStep || it.kind == railStage):
			it.in = it.state // the light runs into a step, not into a head
		default:
			it.in = "done"
		}
		if i > 0 {
			prev := &items[i-1]
			prev.out = it.in
			if waits(it.in) {
				prev.out = "done"
			}
			if waits(prev.state) && it.in == "done" && (prev.kind == railStep || prev.kind == railStage) {
				prev.out = "future" // the steps after a live one have not run
			}
		}
	}
	return items
}

// stageState is the state of a stage: the liveliest of its steps.
func stageState(stage []tlStep) string {
	st := "done"
	for _, s := range stage {
		switch {
		case s.state == "failed":
			return "failed"
		case s.state == "ask":
			st = "ask"
		case s.state == "live" && st != "ask":
			st = "live"
		}
	}
	return st
}

// railView shows a run as a rail.
func (d *desktop) railView(c *ui.Context, pal *palette, tl timeline) {
	ui.Column(c).Children(func() {
		if len(tl.phases) > 1 {
			phaseStrip(c, pal, tl)
		}
		ui.Column(c).Padding(8, 14, 12, 6).Children(func() {
			for i, it := range d.railItems(tl) {
				d.railRow(c, pal, tl, i, it)
			}
		})
	})
}

// phaseStrip shows the phases as stations on a line, the way the run
// went: done, at work, asking, failed or planned.
func phaseStrip(c *ui.Context, pal *palette, tl timeline) {
	states := make([]string, len(tl.phases))
	for i, ph := range tl.phases {
		states[i] = phaseState(tl, ph)
	}
	n := len(tl.phases)
	ui.Row(c.Key("strip")).Padding(16, 8, 14, 8).Justify(ui.SpaceBetween).Draw(func(p *ui.Painter, r ui.Rect) {
		step := (r.W - 16 - stationW) / float32(n-1)
		y := r.Y + 16 + 11
		for i := 1; i < n; i++ {
			x0 := r.X + 8 + stationW/2 + float32(i-1)*step + 15
			x1 := r.X + 8 + stationW/2 + float32(i)*step - 15
			railLine(p, pal, states[i], x0, y, x1, y, true)
		}
	}).Children(func() {
		for i, ph := range tl.phases {
			st := states[i]
			meta := locales.WorkflowPlanned()
			switch {
			case st == "skipped":
				meta = locales.WorkflowNotReached()
			case len(ph.steps) > 0:
				meta = locales.WorkflowStepCount(len(ph.steps)) + " · " + span(phaseTime(tl, ph))
			}
			ink, metaInk := pal.text, pal.textFaint
			if st == "planned" || st == "skipped" {
				ink = pal.textFaint
			}
			if waits(st) || st == "failed" {
				metaInk = stateColor(pal, st)
			}
			ui.Column(c.Key(fmt.Sprintf("station-%d", i))).Width(stationW).Shrink(0).AlignItems(ui.Center).Gap(5).Children(func() {
				ui.Box(c).Size(22, 22).Center().Draw(func(p *ui.Painter, r ui.Rect) {
					if waits(st) {
						halo(p, r.X+11, r.Y+11, 9, stateColor(pal, st))
					}
					var bg ui.Path
					bg.Circle(r.X+11, r.Y+11, 11)
					p.FillPath(&bg, pal.card)
				}).Children(func() {
					if st == "planned" || st == "skipped" {
						ui.Box(c).Size(18, 18).Draw(func(p *ui.Painter, r ui.Rect) {
							var ring ui.Path
							ring.Circle(r.X+9, r.Y+9, 8)
							p.StrokePath(ring.Dash(0, 3, 3), 1.3, pal.textFaint)
						})
						return
					}
					statusIcon(c, pal, iconState(st), st == "done", 20)
				})
				ui.Text(c, ph.title).FontSize(12.5).FontWeight(600).TextColor(ink).SingleLine().Ellipsis("…").MaxWidth(stationW)
				ui.Text(c, meta).FontSize(11.5).TextColor(metaInk).FontFeatures("tnum").SingleLine().Ellipsis("…").MaxWidth(stationW)
			})
		}
	})
}

// phaseTime is how long a phase took, or has taken so far.
func phaseTime(tl timeline, ph tlPhase) time.Duration {
	if len(ph.steps) == 0 {
		return 0
	}
	from, to := ph.steps[0].from, ph.steps[0].from
	for _, s := range ph.steps {
		from, to = min(from, s.from), max(to, tl.to(s, time.Now()))
	}
	return to - from
}

// railLine paints a line of the rail or of the strip by its state: solid
// where the run went, dashed where it has not been, and with a running
// light into a step that works.
func railLine(p *ui.Painter, pal *palette, state string, x0, y0, x1, y1 float32, withComet bool) {
	if x0 == x1 && y1 <= y0 || x0 != x1 && x1 <= x0 {
		return
	}
	switch state {
	case "", "none":
	case "live", "ask":
		col := stateColor(pal, state)
		p.Line(x0, y0, x1, y1, 2, col.Alpha(0.3))
		if withComet {
			comet(p, x0, y0, x1, y1, col)
		}
	case "future":
		path := (&ui.Path{}).MoveTo(x0, y0).LineTo(x1, y1)
		p.StrokePath(path.Dash(0, 3, 4), 1.5, pal.textFaint.Alpha(0.7))
	default:
		p.Line(x0, y0, x1, y1, 2, pal.text.Alpha(0.22))
	}
}

// railRow shows one row of the rail: its node on the line, and its body.
func (d *desktop) railRow(c *ui.Context, pal *palette, tl timeline, i int, it railItem) {
	ny := float32(17) // where the node sits, from the top of the row
	switch {
	case it.kind == railStage:
		ny = 13
	case it.kind == railLoop:
		ny = 22
	case it.kind == railStep && it.state != "done" && it.state != "stopped":
		ny = 26
	}
	ui.Row(c.Key(fmt.Sprintf("rail-%d", i))).Children(func() {
		ui.Box(c).Width(railGut).Shrink(0).AlignSelf(ui.Stretch).Draw(func(p *ui.Painter, r ui.Rect) {
			x := r.X + railGut/2
			if i > 0 {
				railLine(p, pal, it.in, x, r.Y, x, r.Y+ny-10, true)
			}
			railLine(p, pal, it.out, x, r.Y+ny+10, x, r.Y+r.H, false)
			if waits(it.state) && it.kind != railLoop {
				halo(p, x, r.Y+ny, 7, stateColor(pal, it.state))
			}
		}).Children(func() {
			ui.Box(c).Absolute().Top(ny-10).Left(railGut/2-10).Size(20, 20).Center().Radius(10).Background(pal.card).Children(func() {
				railNode(c, pal, it)
			})
		})
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			switch it.kind {
			case railPhase:
				d.railPhaseRow(c, pal, tl, it)
			case railLoop:
				text, m := loopChip(it.loop)
				ui.Row(c).Height(44).Gap(8).AlignItems(ui.Center).Padding(0, 10).Children(func() {
					ui.Text(c, loopName(it.loop)).FontSize(13).FontWeight(600).TextColor(pal.text).SingleLine()
					chip(c, text, m)
				})
			case railStage:
				d.railStageRow(c, pal, tl, it.steps)
			default:
				d.railStepRow(c, pal, tl, it.steps[0])
			}
		})
	})
}

// railNode is the mark of a row on the line.
func railNode(c *ui.Context, pal *palette, it railItem) {
	switch it.kind {
	case railPhase:
		diamondMark(c, stateColor(pal, it.state), it.state == "planned" || it.state == "skipped")
	case railLoop:
		ui.Icon(c, reloadIcon).Size(13, 13).TextColor(markColor(pal, it.loop))
	case railStage:
		ui.Box(c).Size(18, 18).Radius(9).Center().Background(pal.chip).Children(func() {
			ui.Icon(c, workflowIcon).Size(10, 10).TextColor(pal.textMuted)
		})
	default:
		size := float32(16)
		if it.state == "done" {
			size = 13
		}
		statusIcon(c, pal, iconState(it.state), false, size)
	}
}

// markColor is the color of a loop's chip.
func markColor(pal *palette, b tlBlock) ui.Color {
	_, m := loopChip(b)
	if m == markQuiet {
		return pal.textFaint
	}
	return m.color(pal)
}

// railPhaseRow is the head of a phase; a folded phase opens on a click.
func (d *desktop) railPhaseRow(c *ui.Context, pal *palette, tl timeline, it railItem) {
	ph := it.phase
	meta := locales.WorkflowPlanned()
	switch {
	case it.state == "skipped":
		meta = locales.WorkflowNotReached()
	case len(ph.steps) > 0:
		meta = locales.WorkflowStepCount(len(ph.steps)) + " · " + span(phaseTime(tl, ph))
	}
	ink := pal.text
	if it.state == "planned" || it.state == "skipped" {
		ink = pal.textFaint
	}
	foldable := len(tl.phases) > 1 && it.state == "done"
	var r ui.Element
	if foldable {
		r = ui.ButtonBase(c.Key("phase-" + ph.title)).Label(ph.title).Justify(ui.Start)
		if r.Hovered() {
			r.Background(pal.rowHover)
		}
	} else {
		r = ui.Row(c.Key("phase-" + ph.title))
	}
	r.Height(36).Gap(10).AlignItems(ui.Center).Padding(0, 10).Radius(8).Children(func() {
		ui.Text(c, ph.title).FontSize(13).FontWeight(600).TextColor(ink).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		ui.Text(c, meta).FontSize(12).TextColor(pal.textFaint).FontFeatures("tnum").SingleLine().Shrink(0)
		if len(ph.steps) == 0 && ph.detail != "" {
			ui.Text(c, ph.detail).FontSize(12).TextColor(pal.textFaint).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		}
		if foldable {
			ui.Spacer(c)
			kinds := []string{}
			for _, s := range ph.steps {
				if !containsString(kinds, s.Kind) {
					kinds = append(kinds, s.Kind)
				}
			}
			if it.folded {
				for _, k := range kinds {
					chip(c, k, markQuiet)
				}
			}
			ui.Icon(c, chevronIcon).Size(11, 11).TextColor(pal.textFaint).Rotate(b2f(!it.folded) * 90)
		}
	})
	if foldable && r.Clicked() {
		if d.wf.unfold == nil {
			d.wf.unfold = map[string]bool{}
		}
		d.wf.unfold[ph.title] = it.folded
	}
}

// railStepRow is a step: a quiet line once done, a card while it works,
// asks or failed.
func (d *desktop) railStepRow(c *ui.Context, pal *palette, tl timeline, s tlStep) {
	text, agent := d.stepText(s.WorkflowStep)
	isTask := (s.Kind == "task" || s.Kind == "reply") && s.Target != ""
	round := func() {
		if s.round > 0 {
			ui.Textf(c, "R%d", s.round).FontSize(10.5).FontWeight(600).TextColor(pal.textFaint).FontFeatures("tnum").SingleLine().Shrink(0)
		}
	}
	if s.state == "done" || s.state == "stopped" {
		var r ui.Element
		if isTask {
			r = ui.ButtonBase(c.Key(s.Key)).Label(text).Radius(8)
			if r.Hovered() {
				r.Background(pal.rowHover)
			}
		} else {
			r = ui.Row(c.Key(s.Key))
		}
		r.Height(34).Gap(10).AlignItems(ui.Center).Padding(0, 10).Children(func() {
			round()
			ui.Icon(c, kindIcon(s.Kind)).Size(13, 13).TextColor(pal.textFaint).Shrink(0)
			ui.Text(c, text).FontSize(13).TextColor(pal.text.Alpha(0.85)).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
			if agent != "" {
				chip(c, agent, markQuiet)
			}
			ui.Text(c, stepTime(tl, s)).FontSize(12).TextColor(pal.textFaint).FontFeatures("tnum").SingleLine().Width(60).TextAlign(ui.End).Shrink(0)
		})
		if isTask && r.Clicked() {
			d.openTask(s.Target)
		}
		return
	}
	col := stateColor(pal, s.state)
	ui.Column(c.Key(s.Key)).Margin(4, 0, 8, 0).Padding(11, 14, 12, 14).Gap(8).Radius(12).
		Border(1, col.Alpha(0.45)).Background(col.Alpha(0.06)).Shadow(0, 8, 26, -10, col.Alpha(0.5)).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			round()
			ui.Icon(c, kindIcon(s.Kind)).Size(13, 13).TextColor(col).Shrink(0)
			title := text
			if s.state == "ask" {
				title = locales.WorkflowWaitsForAnswer()
			}
			ui.Text(c, title).FontSize(13.5).FontWeight(600).TextColor(pal.text).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
			if agent != "" {
				chip(c, agent, markQuiet)
			}
			liveTime(c, tl, s, col)
		})
		switch s.state {
		case "live":
			if act := d.activity(s.Target); act != "" {
				activityLine(c, pal, act, col)
			}
		case "ask":
			ui.Text(c, text).FontSize(13).LineHeight(1.5).TextColor(pal.textMuted)
		case "failed":
			if msg := stepError(s); msg != "" {
				ui.Text(c, msg).Font("monospace").FontSize(11.5).LineHeight(1.5).TextColor(pal.danger).
					Padding(8, 10).Radius(8).Background(pal.dangerBg).Selectable()
			}
		}
		if isTask && s.state == "failed" {
			ui.Row(c).Children(func() {
				if button(c, "open-"+s.Key, locales.WorkflowOpenTask(), plainButton, false) {
					d.openTask(s.Target)
				}
			})
		}
	})
}

// liveTime is the time of a step that the clock moves while it waits.
func liveTime(c *ui.Context, tl timeline, s tlStep, col ui.Color) {
	ui.Box(c).Width(64).Height(16).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		if s.waiting() {
			p.AnimationFrameRate(2)
		}
		label := ui.Span{Text: span(tl.to(s, p.Now()) - s.from), Size: 12, Color: col}
		w, h := p.MeasureText(0, label)
		p.RichText(r.X+r.W-w, r.Y+(r.H-h)/2, 0, label)
	})
}

// activityLine is what the agent of a step does now, with a caret that
// blinks.
func activityLine(c *ui.Context, pal *palette, act string, col ui.Color) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "› "+act).Font("monospace").FontSize(12).TextColor(pal.textMuted).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		ui.Box(c).Size(6, 13).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
			p.AnimationFrameRate(2)
			if p.Now().UnixMilli()%1000 < 500 {
				p.Fill(r, col, 1)
			}
		})
	})
}

// railStageRow shows steps that ran at the same time, side by side.
func (d *desktop) railStageRow(c *ui.Context, pal *palette, tl timeline, stage []tlStep) {
	ui.Column(c).Gap(4).Padding(0, 0, 8, 0).Children(func() {
		ui.Row(c).Height(26).Gap(6).AlignItems(ui.Center).Padding(0, 10).Children(func() {
			ui.Text(c, locales.WorkflowParallel(len(stage))).FontSize(12).TextColor(pal.textFaint).SingleLine()
		})
		ui.Row(c).Gap(10).AlignItems(ui.Start).Padding(0, 0, 0, 10).Children(func() {
			for _, s := range stage {
				d.miniCard(c, pal, tl, s)
			}
		})
	})
}

// miniCard is a step of a stage.
func (d *desktop) miniCard(c *ui.Context, pal *palette, tl timeline, s tlStep) {
	text, agent := d.stepText(s.WorkflowStep)
	col := stateColor(pal, s.state)
	b := ui.ButtonBase(c.Key(s.Key)).Grow(1).Basis(0).MinWidth(0).Padding(9, 11).Gap(6).Radius(10).Label(text).
		Border(1, pal.divider).Background(pal.panel)
	if s.state != "done" && s.state != "stopped" {
		b.Border(1, col.Alpha(0.45)).Background(col.Alpha(0.06))
	}
	b.Column().AlignItems(ui.Stretch).Justify(ui.Start).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			statusIcon(c, pal, iconState(s.state), false, 14)
			if s.round > 0 {
				ui.Textf(c, "R%d", s.round).FontSize(10.5).FontWeight(600).TextColor(pal.textFaint).SingleLine().Shrink(0)
			}
			ui.Text(c, text).FontSize(13).TextColor(pal.text).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(0, 0, 0, 22).Children(func() {
			if agent != "" {
				chip(c, agent, markQuiet)
			}
			ui.Spacer(c)
			liveTime(c, tl, s, map[bool]ui.Color{true: col, false: pal.textFaint}[s.waiting()])
		})
		if s.state == "live" {
			if act := d.activity(s.Target); act != "" {
				ui.Box(c).Padding(0, 0, 0, 22).Children(func() { activityLine(c, pal, act, col) })
			}
		}
	})
	if (s.Kind == "task" || s.Kind == "reply") && s.Target != "" && b.Clicked() {
		d.openTask(s.Target)
	}
}

// containsString tells whether ss holds s.
func containsString(ss []string, s string) bool {
	for _, x := range ss {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}
