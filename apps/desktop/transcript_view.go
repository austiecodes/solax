package main

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/austiecodes/solax/apps/desktop/locales"
	"github.com/austiecodes/solax/pkg/ir"
)

// Drawing a transcript (transcript.go): the messages in a column of
// reading width, the steps folded into a group that opens and closes with
// an animation, and the composer under it.

const (
	readW  = 720 // the width of a transcript and of its composer
	stepsW = 640 // the width of the text in a group of steps
	stepsH = 240 // the most height an open group of steps takes; it scrolls past it
	fadeH  = 24  // the height of the fade at an end of a group that scrolls
	eventW = 520 // the width of the card of a task or a workflow in the chat
)

// fadeEnds fades a transcript out at its ends, so text that scrolls
// under the header or the composer does not end on a hard cut.
var fadeEnds = ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{
	{Color: ui.Transparent, At: 0}, {Color: ui.RGB(0, 0, 0), At: 0.025},
	{Color: ui.RGB(0, 0, 0), At: 0.95}, {Color: ui.Transparent, At: 1},
}}

// fold is the open state of a group of steps, or of the output of a tool
// call. A group is open while the agent works on it and closes when it is
// done, until the user opens or closes it.
type fold struct {
	open bool
	set  bool // the user chose

	// steps is the window of an open group of steps, which follows its
	// end while the agent works.
	steps ui.ListState
}

// foldFor returns the fold of key, open or closed as auto says unless the
// user chose.
func (d *desktop) foldFor(key string, auto bool) *fold {
	f := d.folds[key]
	if f == nil {
		f = &fold{}
		d.folds[key] = f
	}
	if !f.set {
		f.open = auto
	}
	return f
}

// transcriptView draws blocks in a list that follows its end. empty,
// when set, fills its place while it has no blocks.
func (d *desktop) transcriptView(c *ui.Context, pal *palette, list *ui.ListState, blocks []block, empty func()) {
	list.Key = func(i int) any { return blocks[i].key }
	list.FollowEnd = true
	list.FollowAnimation = &ui.Smooth
	if len(blocks) == 0 && empty != nil {
		ui.Column(c.Key("transcript")).Grow(1).MinHeight(0).Children(empty)
		return
	}
	ui.Column(c.Key("transcript")).Grow(1).MinHeight(0).Children(func() {
		ui.List(c, list, len(blocks), func(i int) {
			ui.Row(c).Justify(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).MaxWidth(readW).Children(func() {
					d.blockView(c, pal, blocks[i])
				})
			})
		}).Grow(1).Padding(12, gutter, 28, gutter).Gap(18).MaskLinearGradient(fadeEnds)
		if len(blocks) > 0 && !list.AtEnd() {
			// A button to go back to the end floats over the list.
			ui.Row(c.Key("to-end")).Absolute().Left(0).Right(0).Bottom(10).Justify(ui.Center).Children(func() {
				b := ui.ButtonBase(c.Key("end")).Size(32, 32).Radius(16).Center().Label(locales.ScrollToEnd()).TextColor(pal.textMuted)
				floating(b.Background(pal.popover), pal)
				if b.Hovered() {
					b.TextColor(pal.text)
				}
				b.Children(func() { ui.Icon(c, arrowDownIcon).Size(14, 14) })
				if b.Clicked() {
					list.ScrollToEnd()
				}
			})
		}
	})
}

func (d *desktop) blockView(c *ui.Context, pal *palette, b block) {
	switch b.kind {
	case blockUser:
		ui.Row(c).Justify(ui.End).Padding(0, 0, 0, 96).Children(func() {
			// A tint of the text, not the accent, as the Codex app.
			ui.Text(c, b.text).Selectable().FontSize(14).LineHeight(1.5).TextColor(pal.text).
				Padding(8, 14).Radius(16).Background(pal.chip).MinWidth(0)
		})
	case blockReply:
		ui.Column(c).Selectable().Children(func() {
			if b.live {
				markdown.Stream(c, b.text, &d.md)
			} else {
				markdown.View(c, b.text, &d.md)
			}
		})
	case blockSteps:
		d.stepsView(c, pal, b)
	case blockNote:
		d.noteView(c, pal, b)
	case blockEvent:
		d.eventView(c, pal, b)
	case blockWorking:
		workingView(c, pal)
	}
}

// noteView is a line between messages: an error, or a compaction.
func (d *desktop) noteView(c *ui.Context, pal *palette, b block) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		col := pal.textMuted
		if b.warn {
			statusIcon(c, pal, "failed", true, 14)
			col = pal.danger
		}
		ui.Text(c, b.text).FontSize(12.5).TextColor(col).MinWidth(0).Selectable()
	})
}

// eventView is a card for a task or a workflow that the chat tells of:
// its icon, its agent or title with its state, a line on it, and what a
// click does, which opens it.
func (d *desktop) eventView(c *ui.Context, pal *palette, b block) {
	ev := b.ev
	icon, state, detail, action := agentIcon, ev.state, "", locales.OpenTask()
	open := func() { d.openTask(ev.id) }
	switch ev.kind {
	case "task":
		if j, ok := d.task(ev.id); ok {
			detail = j.Title
		}
	case "workflow":
		icon, action = workflowIcon, locales.OpenWorkflow()
		open = func() { d.openWorkflow(ev.id) }
		w, known := d.workflowRun(ev.id)
		switch {
		case ev.question != "":
			icon, detail = questionIcon, ev.question
			if !known || w.State == "waiting" {
				action = locales.WorkflowAnswer()
			} else {
				state = "" // it has its answer
			}
		case ev.state == "failed" && w.Error != "":
			detail = w.Error
		}
	}
	m := runMark(state)
	card := ui.ButtonBase(c.Key("event")).AlignSelf(ui.Stretch).MaxWidth(eventW).
		Padding(10, 14, 10, 10).Gap(10).AlignItems(ui.Center).Radius(12).Border(1, pal.divider).
		Label(ev.name + " " + stateWord(state) + " " + action)
	if card.Hovered() {
		card.Background(pal.rowHover)
	}
	card.Children(func() {
		avatar(c, icon, m)
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, ev.name).FontSize(13.5).FontWeight(600).TextColor(pal.text).SingleLine().Ellipsis("…").MinWidth(0)
				if state != "" {
					chip(c, stateWord(state), m)
				}
			})
			if detail != "" {
				// A question wraps: it is what the user answers.
				t := ui.Text(c, detail).FontSize(12.5).LineHeight(1.4).TextColor(pal.textMuted).MaxLines(1).Ellipsis("…").MinWidth(0)
				if ev.question != "" {
					t.MaxLines(2)
				}
			}
		})
		fg := pal.textMuted
		if card.Hovered() {
			fg = pal.text
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Margin(0, 0, 0, 16).Children(func() {
			ui.Text(c, action).FontSize(12.5).FontWeight(500).TextColor(fg).SingleLine()
			ui.Icon(c, chevronIcon).Size(8, 8).TextColor(pal.textFaint)
		})
	})
	if card.Clicked() {
		open()
	}
}

// runMark is the color of the state of a task or a workflow.
func runMark(state string) mark {
	switch state {
	case "done":
		return markGood
	case "failed":
		return markBad
	case "waiting":
		return markWarn
	case "running", "queued":
		return markAccent
	}
	return markQuiet
}

// stepsView is a group of steps: the icons of its steps and a quiet line
// that tells what the agent did, which opens to show them all.
func (d *desktop) stepsView(c *ui.Context, pal *palette, b block) {
	f := d.foldFor(b.key, b.live)
	p := ui.CollapsibleBase(c.Key(b.key), &f.open)
	tr := p.Trigger.Gap(8).AlignItems(ui.Center).Padding(2, 0).AlignSelf(ui.Start).
		Label(stepsTitle(b)).Cursor(ui.CursorPointer)
	fg := pal.textMuted
	if tr.Hovered() {
		fg = pal.text
	}
	tr.Children(func() {
		stepIcons(c, pal, b)
		ui.Text(c, stepsTitle(b)).FontSize(13).TextColor(fg).SingleLine().Ellipsis("…").MinWidth(0)
		ui.Icon(c, chevronIcon).Size(8, 8).TextColor(pal.textFaint).Rotate(90 * p.Progress()).Shrink(0)
	})
	if tr.Changed() {
		f.set = true
	}
	if !f.open && p.Progress() == 0 {
		return
	}
	p.Panel(func() {
		// A window of at most stepsH: it slides to the newest step while
		// the agent works, unless the user scrolls up from the end.
		w := &f.steps
		w.Key = func(i int) any { return b.steps[i].id }
		w.FollowEnd = b.live
		w.FollowAnimation = &ui.Smooth
		ui.Column(c).Margin(8, 0, 2, 5).MaxWidth(stepsW).BorderWidth(0, 0, 0, 1).BorderColor(pal.divider).Children(func() {
			l := ui.List(c, w, len(b.steps), func(i int) {
				ui.Column(c).Padding(5, 12, 5, 14).Children(func() { d.stepView(c, pal, b.steps[i]) })
			}).MaxHeight(stepsH).Shrink(0)
			first, _ := w.Visible()
			if m, ok := windowFade(first > 0, !w.AtEnd()); ok {
				l.MaskLinearGradient(m)
			}
		})
	})
}

// windowFade returns the mask that fades the window of a group of steps
// at the ends where more is hidden (top, bottom), and false when nothing
// is.
func windowFade(top, bottom bool) (ui.LinearGradient, bool) {
	if !top && !bottom {
		return ui.LinearGradient{}, false
	}
	// Steps hide only when the window is stepsH high.
	at := float32(fadeH) / stepsH
	opaque := ui.RGB(0, 0, 0)
	g := ui.LinearGradient{Angle: 180}
	if top {
		g.Stops = append(g.Stops, ui.GradientStop{Color: ui.Transparent, At: 0}, ui.GradientStop{Color: opaque, At: at})
	} else {
		g.Stops = append(g.Stops, ui.GradientStop{Color: opaque, At: 0})
	}
	if bottom {
		g.Stops = append(g.Stops, ui.GradientStop{Color: opaque, At: 1 - at}, ui.GradientStop{Color: ui.Transparent, At: 1})
	} else {
		g.Stops = append(g.Stops, ui.GradientStop{Color: opaque, At: 1})
	}
	return g, true
}

// stepIcons stacks the icons of the kinds of steps of a group, as coins:
// a turning ring first while the agent works.
func stepIcons(c *ui.Context, pal *palette, b block) {
	var icons []*ui.SVG
	for _, s := range b.steps {
		i := stepIcon(s)
		if len(icons) == 0 || icons[len(icons)-1] != i {
			icons = append(icons, i)
		}
	}
	if len(icons) > 4 {
		icons = icons[len(icons)-4:]
	}
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Children(func() {
		coin := func(i int) ui.Element {
			e := ui.Box(c).Size(24, 24).Radius(12).Center().Shrink(0).Background(pal.card).
				Border(1, pal.divider).TextColor(pal.textMuted)
			if i > 0 {
				e.Margin(0, 0, 0, -10)
			}
			return e
		}
		if b.live {
			coin(0).Children(func() { statusIcon(c, pal, "running", false, 12) })
		}
		for i, svg := range icons {
			n := i
			if b.live {
				n++
			}
			coin(n).Children(func() { ui.Icon(c, svg).Size(12, 12) })
		}
	})
}

func stepIcon(s step) *ui.SVG {
	if s.kind == ir.ItemReasoning {
		return thoughtIcon
	}
	switch s.tool.Class {
	case ir.ToolShell, ir.ToolScript:
		return agentIcon
	case ir.ToolFileRead:
		return docIcon
	case ir.ToolFileSearch:
		return searchIcon
	case ir.ToolFileChange:
		return pencilIcon
	case ir.ToolWeb:
		return globeIcon
	case ir.ToolSubagent:
		return sparklesIcon
	}
	return toolIcon
}

// workingView tells that the agent works while nothing else on screen
// shows it: the message is on its way, or the turn has not started yet.
func workingView(c *ui.Context, pal *palette) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(2, 0).Children(func() {
		statusIcon(c, pal, "running", false, 12)
		ui.Text(c, locales.ChatWorking()).FontSize(13).TextColor(pal.textMuted).SingleLine()
	})
}

// stepView is one step in an open group: what the agent thought, or the
// tool it called, which opens to show the output.
func (d *desktop) stepView(c *ui.Context, pal *palette, s step) {
	if s.kind == ir.ItemReasoning {
		ui.Column(c).Gap(3).Children(func() {
			head := thoughtFor(s.took)
			if s.running() {
				head = locales.ThinkingNow()
			}
			ui.Text(c, head).FontSize(13).FontWeight(500).TextColor(pal.text)
			if s.text != "" {
				ui.Text(c, s.text).FontSize(13).LineHeight(1.5).TextColor(pal.textMuted).Selectable()
			}
		})
		return
	}
	t := s.tool
	out := strings.TrimSpace(t.Output)
	more := out != "" || len(t.Files) > 0
	head := func(progress float32) {
		ui.Text(c, toolVerb(t)).FontSize(13).FontWeight(500).TextColor(pal.text).SingleLine().Shrink(0)
		ui.Text(c, toolArg(t)).FontSize(13).TextColor(pal.textMuted).SingleLine().Ellipsis("…").MinWidth(0)
		switch {
		case s.running():
			statusIcon(c, pal, "running", false, 11)
		case s.status == ir.StatusFailed || s.status == ir.StatusDeclined || exitFailed(t):
			ui.Text(c, failure(s)).FontSize(11.5).TextColor(pal.danger).SingleLine().Shrink(0)
		}
		if s.took >= time.Second && !s.running() {
			ui.Text(c, seconds(s.took)).FontSize(11.5).TextColor(pal.textFaint).FontFeatures("tnum").SingleLine().Shrink(0)
		}
		if more {
			ui.Icon(c, chevronIcon).Size(8, 8).TextColor(pal.textFaint).Rotate(90 * progress).Shrink(0)
		}
	}
	if !more {
		ui.Row(c).Gap(6).AlignItems(ui.Center).MinHeight(20).Children(func() { head(0) })
		return
	}
	f := d.foldFor("step-"+s.id, false)
	p := ui.CollapsibleBase(c.Key("step-"+s.id), &f.open)
	tr := p.Trigger.Gap(6).AlignItems(ui.Center).MinHeight(20).Label(toolVerb(t) + " " + toolArg(t)).Cursor(ui.CursorPointer)
	tr.Children(func() { head(p.Progress()) })
	if tr.Changed() {
		f.set = true
	}
	if !f.open && p.Progress() == 0 {
		return
	}
	p.Panel(func() {
		ui.Column(c).Gap(4).Margin(6, 0, 0, 0).Padding(8, 10).Radius(8).Background(pal.chip).Children(func() {
			for _, fc := range t.Files {
				ui.Text(c, fc.Op+" "+fc.Path).Font("monospace").FontSize(11.5).TextColor(pal.textMuted).Selectable()
			}
			if out != "" {
				ui.Text(c, out).Font("monospace").FontSize(11.5).LineHeight(1.4).TextColor(pal.text).MaxLines(24).Selectable()
			}
		})
	})
}

func exitFailed(t *ir.ToolCall) bool { return t.ExitCode != nil && *t.ExitCode != 0 }

func failure(s step) string {
	switch {
	case s.status == ir.StatusDeclined:
		return locales.StepDeclined()
	case s.tool.ExitCode != nil && *s.tool.ExitCode != 0:
		return locales.StepExit(*s.tool.ExitCode)
	}
	return locales.StateFailed()
}

// composer is the box to write to an agent, as the Codex app's: a
// rounded box that floats a little over the page, the text on top and a
// toolbar row under it, with the send button at its right. The text grows
// to a few lines; Return sends it and Shift+Return breaks the line. info,
// when set, fills the left of the toolbar row.
type composer struct {
	key, placeholder string
	draft            *string
	disabled         bool // nothing can take the text now
	busy             bool // the last text is on its way
	focus            bool // take the focus as it shows
	info             func()
	send             func(text string)
}

func (cm composer) view(c *ui.Context, pal *palette) {
	ready := !cm.disabled && !cm.busy && strings.TrimSpace(*cm.draft) != ""
	submit := func(text string) {
		if text = strings.TrimSpace(text); text != "" && !cm.disabled && !cm.busy {
			cm.send(text)
		}
	}
	ui.Row(c.Key(cm.key)).Justify(ui.Center).Padding(0, gutter, 16, gutter).Shrink(0).Children(func() {
		box := ui.Column(c).Grow(1).MinWidth(0).MaxWidth(readW).Radius(20).Cursor(ui.CursorText)
		var in ui.Element
		box.Children(func() {
			in = ui.TextAreaBase(c.Key("in"), cm.draft).Lines(1, 9).Padding(14, 16, 6, 16).
				FontSize(14).LineHeight(1.5).Placeholder(cm.placeholder).Label(cm.placeholder).
				OnTextKey(func(k ui.TextKey) bool {
					if k.Key != ui.KeyEnter || k.Mods != 0 {
						return false
					}
					submit(k.Text())
					return true
				})
			if cm.focus {
				in.AutoFocus()
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(2, 8, 8, 12).Children(func() {
				if cm.info != nil {
					cm.info()
				}
				ui.Spacer(c)
				sendButton(c, pal, ready, cm.busy, func() { submit(*cm.draft) })
			})
		})
		// A hairline that darkens on focus, and a soft shadow in the
		// light: no accent glow.
		focus := box.Animate("focus", b2f(in.Focused()), 120*time.Millisecond)
		box.Background(pal.composer).InsetShadow(0, 0, 0, 1, pal.text.Alpha(0.1+0.08*focus))
		if !pal.dark {
			box.Shadow(0, 4, 16, 0, ui.RGBA(0, 0, 0, 0.05)).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.03))
		}
	})
}

// sendButton is the round button at the right of the composer: filled in
// the ink when there is text to send, faint when not, a turning ring
// while the text is on its way.
func sendButton(c *ui.Context, pal *palette, ready, busy bool, send func()) {
	b := ui.ButtonBase(c.Key("send")).Size(28, 28).Radius(14).Center().Label(locales.Send()).Disabled(!ready).
		Tooltip(locales.SendTip())
	bg, fg := pal.chip, pal.textFaint
	if ready {
		bg, fg = pal.text, pal.card
		if b.Hovered() {
			bg = bg.Mix(pal.card, 0.15)
		}
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond})).TextColor(fg)
	b.Children(func() {
		if busy {
			statusIcon(c, pal, "running", false, 13)
		} else {
			ui.Icon(c, sendIcon).Size(13, 13)
		}
	})
	if b.Clicked() && ready {
		send()
	}
}
