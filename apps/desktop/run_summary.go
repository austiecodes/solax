package main

import (
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/locales"
	"github.com/austiecodes/solax/pkg/protocol"
)

// The facts of a run, at a glance: a strip of four cells (status,
// duration, steps, who started it) over a quiet line with its ID. A run
// that failed gets a callout above the strip, with its error and what to
// do next.

// runSummary shows the strip of a run.
func (d *desktop) runSummary(c *ui.Context, w protocol.Workflow, tl timeline) {
	pal := paletteFor(c)
	ui.Column(c.Key("summary")).Radius(12).Border(1, pal.divider).Background(pal.panel).Clip().Dividers(1, pal.divider).Children(func() {
		ui.Row(c).Dividers(1, pal.divider).Children(func() {
			// Status.
			col := pal.text
			switch w.State {
			case "failed":
				col = pal.danger
			case "waiting":
				col = pal.attention
			}
			summaryCell(c, pal, locales.FieldStatus(), statusSub(w, tl), func() {
				statusIcon(c, pal, w.State, false, 15)
				ui.Text(c, capital(stateWord(w.State))).FontSize(16).FontWeight(600).TextColor(col).SingleLine()
			})

			// Duration: it ticks while the run is live.
			when := clock(w.CreatedAt) + " → " + clock(w.UpdatedAt)
			if tl.live {
				when = locales.WorkflowSince(clock(w.CreatedAt))
			}
			summaryCell(c, pal, locales.WorkflowDuration(), when, func() {
				ui.Box(c).Height(20).Grow(1).Draw(func(p *ui.Painter, r ui.Rect) {
					if tl.live {
						p.AnimationFrameRate(2)
					}
					label := ui.Span{Text: span(tl.elapsed(p.Now())), Size: 16, Weight: 600, Color: pal.text}
					_, h := p.MeasureText(0, label)
					p.RichText(r.X, r.Y+(r.H-h)/2, 0, label)
				})
			})

			// Steps.
			summaryCell(c, pal, locales.WorkflowStepsLabel(), stepsSub(w, tl), func() {
				ui.Textf(c, "%d", len(w.Steps)).FontSize(16).FontWeight(600).TextColor(pal.text).FontFeatures("tnum").SingleLine()
			})

			// Who started it.
			by := locales.WorkflowByYou()
			if w.RequestedBy == "super" {
				by = locales.NavChat()
			}
			summaryCell(c, pal, locales.WorkflowStartedBy(), ago(w.CreatedAt), func() {
				ui.Text(c, by).FontSize(16).FontWeight(600).TextColor(pal.text).SingleLine()
			})
		})

		// The ID, to copy, and the run this one resumes.
		ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(5, 10, 5, 18).Children(func() {
			ui.Text(c, w.ID).Font("monospace").FontSize(11.5).TextColor(pal.textFaint).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0).Selectable()
			id := w.ID
			iconButton(c, "copy-id", docIcon, locales.WorkflowCopy(), pal, func() { c.WriteClipboard(id) })
			ui.Spacer(c)
			if w.ResumeFrom != "" {
				ui.Text(c, locales.WorkflowResumes()).FontSize(11.5).TextColor(pal.textFaint).SingleLine().Shrink(0)
				b := ui.ButtonBase(c.Key("resume-from")).Label(w.ResumeFrom).Padding(2, 4).Radius(5)
				if b.Hovered() {
					b.Background(pal.rowHover)
				}
				b.Children(func() {
					ui.Text(c, w.ResumeFrom).Font("monospace").FontSize(11.5).TextColor(pal.accent).SingleLine()
				})
				if b.Clicked() {
					d.openWorkflow(w.ResumeFrom)
				}
			}
		})
	})
}

// summaryCell is a cell of the strip: a label, a value that value
// builds, and a line under it.
func summaryCell(c *ui.Context, pal *palette, label, sub string, value func()) {
	ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(5).Padding(14, 18, 13, 18).Children(func() {
		ui.Text(c, label).FontSize(11.5).TextColor(pal.textFaint).SingleLine()
		ui.Row(c).Height(22).Gap(7).AlignItems(ui.Center).Children(value)
		ui.Text(c, sub).FontSize(12).TextColor(pal.textMuted).FontFeatures("tnum").SingleLine().Ellipsis("…")
	})
}

// statusSub is the line under the status: the round that works, how far a
// stopped loop got, or the phases of a run that is done.
func statusSub(w protocol.Workflow, tl timeline) string {
	if r := tl.liveRound(); r != "" {
		return r
	}
	for _, ph := range tl.phases {
		for _, b := range ph.blocks {
			if b.inLoop && !b.cont && b.all().loopState() == "failed" {
				text, _ := loopChip(b)
				return text
			}
		}
	}
	if w.State == "waiting" {
		return locales.WorkflowWaitsForAnswer()
	}
	if n := len(tl.phases); n > 0 && tl.phases[0].title != "" {
		return locales.WorkflowPhaseCount(n)
	}
	return ""
}

// stepsSub is the line under the step count: the tasks, the failed and
// the loops.
func stepsSub(w protocol.Workflow, tl timeline) string {
	tasks, failed, working := 0, 0, 0
	for _, s := range w.Steps {
		if s.Kind == "task" {
			tasks++
		}
		switch {
		case s.State == "failed":
			failed++
		case s.State == "pending" && (s.Kind == "task" || s.Kind == "reply"):
			working++
		}
	}
	out := locales.WorkflowTaskCount(tasks)
	switch {
	case failed > 0:
		return out + " · " + locales.WorkflowFailedCount(failed)
	case tl.live && working > 0:
		return out + " · " + locales.WorkflowWorkingCount(working) // the status says the round
	}
	for _, ph := range tl.phases {
		for _, b := range ph.blocks {
			if b.inLoop && !b.cont {
				text, _ := loopChip(b)
				return out + " · " + loopName(b) + ": " + text
			}
		}
	}
	return out
}

// failureCallout shows why a run failed: the step that failed, its error,
// and buttons to open its task or run again from it.
func (d *desktop) failureCallout(c *ui.Context, w protocol.Workflow, tl timeline) {
	pal := paletteFor(c)
	var bad *tlStep
	for _, ph := range tl.phases {
		for i := range ph.steps {
			if ph.steps[i].state == "failed" {
				bad = &ph.steps[i]
			}
		}
	}
	title := locales.WorkflowRunFailed()
	if bad != nil {
		text, _ := d.stepText(bad.WorkflowStep)
		title = locales.WorkflowStepFailed(text)
		if bad.round > 0 {
			title = locales.WorkflowStepFailedIn(bad.round, text)
		}
	}
	ui.Row(c.Key("failure")).Gap(12).AlignItems(ui.Start).Padding(12, 14, 12, 16).Radius(12).
		Border(1, pal.danger.Alpha(0.35)).Background(pal.dangerBg).Children(func() {
		ui.Box(c).Padding(1, 0, 0, 0).Shrink(0).Children(func() { statusIcon(c, pal, "failed", true, 15) })
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, title).FontSize(13.5).FontWeight(600).TextColor(pal.text)
			if w.Error != "" {
				ui.Text(c, w.Error).Font("monospace").FontSize(12).LineHeight(1.55).TextColor(pal.danger).Selectable()
			}
		})
		ui.Row(c).Gap(8).Shrink(0).Children(func() {
			if bad != nil && (bad.Kind == "task" || bad.Kind == "reply") && bad.Target != "" {
				if button(c, "open-failed", locales.WorkflowOpenTask(), plainButton, false) {
					d.openTask(bad.Target)
				}
			}
			if button(c, "resume", locales.WorkflowResumeRun(), primaryButton, d.wf.busy) {
				d.resumeWorkflow(w)
			}
		})
	})
}

// resumeWorkflow starts a new run of the script of w that reuses its
// finished steps.
func (d *desktop) resumeWorkflow(w protocol.Workflow) {
	req := protocol.StartWorkflowRequest{Source: w.Source, Resume: w.ID}
	if req.Source == "" {
		req.Name = w.Name
	}
	d.startWorkflow(req)
}

// clock says a time of day: "14:02".
func clock(t time.Time) string { return t.Local().Format("15:04") }

// capital starts s with a capital letter.
func capital(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}
