package main

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/austiecodes/solax/pkg/ir"
)

// Drawing a transcript (transcript.go): the messages in a column of
// reading width, the steps folded into a group that opens and closes with
// an animation, and the composer under it.

const (
	readW  = 740 // the width of a transcript and of its composer
	stepsW = 640 // the width of the text in a group of steps
)

// fold is the open state of a group of steps, or of the output of a tool
// call. A group is open while the agent works on it and closes when it is
// done, until the user opens or closes it.
type fold struct {
	open bool
	set  bool // the user chose
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
		}).Grow(1).Padding(8, 24, 24, 24).Gap(16)
		if len(blocks) > 0 && !list.AtEnd() {
			// A button to go back to the end floats over the list.
			ui.Row(c.Key("to-end")).Absolute().Left(0).Right(0).Bottom(10).Justify(ui.Center).Children(func() {
				b := ui.ButtonBase(c.Key("end")).Size(32, 32).Radius(16).Center().Label("Scroll to the end").TextColor(pal.textMuted)
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
		ui.Row(c).Justify(ui.End).Padding(0, 0, 0, 80).Children(func() {
			// Kero's chip: a tint of the text, not the accent.
			ui.Text(c, b.text).Selectable().FontSize(14).LineHeight(1.45).TextColor(pal.text).
				Padding(9, 15).Radius(18).Background(pal.chip).MinWidth(0)
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
	}
}

// noteView is a line between messages: a job that ended, with a link to
// it, or an error.
func (d *desktop) noteView(c *ui.Context, pal *palette, b block) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		switch {
		case b.warn:
			statusIcon(c, pal, "failed", true, 14)
		case b.job != "":
			if j, ok := d.job(b.job); ok {
				statusIcon(c, pal, j.State, d.unread[j.ID], 14)
			}
		}
		col := pal.textMuted
		if b.warn {
			col = pal.danger
		}
		ui.Text(c, b.text).FontSize(12.5).TextColor(col).MinWidth(0).Selectable()
		if b.job != "" {
			l := ui.ButtonBase(c.Key("open")).Label("Open job").Padding(2, 6).Radius(6).TextColor(pal.textMuted)
			if l.Hovered() {
				l.Background(pal.rowHover).TextColor(pal.text)
			}
			l.Children(func() { ui.Text(c, "Open job").FontSize(12.5).FontWeight(500) })
			if l.Clicked() {
				d.openJob(b.job)
			}
		}
	})
}

// stepsView is a group of steps: a line that tells what the agent did,
// with the icons of its steps, which opens to show them all.
func (d *desktop) stepsView(c *ui.Context, pal *palette, b block) {
	f := d.foldFor(b.key, b.live)
	p := ui.CollapsibleBase(c.Key(b.key), &f.open)
	tr := p.Trigger.Gap(8).AlignItems(ui.Center).Padding(3, 10, 3, 3).Radius(14).AlignSelf(ui.Start).
		Label(stepsTitle(b)).Cursor(ui.CursorPointer)
	if tr.Hovered() {
		tr.Background(pal.rowHover)
	}
	tr.Children(func() {
		stepIcons(c, pal, b)
		t := ui.Text(c, stepsTitle(b)).FontSize(13.5).FontWeight(550).TextColor(pal.text).SingleLine().Ellipsis("…").MinWidth(0)
		if b.live {
			t.TextColor(pal.textMuted)
		}
		ui.Icon(c, chevronIcon).Size(9, 9).TextColor(pal.textFaint).Rotate(90 * p.Progress()).Shrink(0)
	})
	if tr.Changed() {
		f.set = true
	}
	if !f.open && p.Progress() == 0 {
		return
	}
	p.Panel(func() {
		ui.Column(c).Gap(10).Margin(8, 0, 2, 14).Padding(2, 0, 2, 16).MaxWidth(stepsW).
			BorderWidth(0, 0, 0, 1.5).BorderColor(pal.divider).Children(func() {
			for _, s := range b.steps {
				d.stepView(c, pal, s)
			}
		})
	})
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

// stepView is one step in an open group: what the agent thought, or the
// tool it called, which opens to show the output.
func (d *desktop) stepView(c *ui.Context, pal *palette, s step) {
	if s.kind == ir.ItemReasoning {
		ui.Column(c).Gap(3).Children(func() {
			head := thoughtFor(s.took)
			if s.running() {
				head = "Thinking…"
			}
			ui.Text(c, head).FontSize(13).FontWeight(600).TextColor(pal.text)
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
		ui.Text(c, toolVerb(t)).FontSize(13).FontWeight(600).TextColor(pal.text).SingleLine().Shrink(0)
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
		return "declined"
	case s.tool.ExitCode != nil && *s.tool.ExitCode != 0:
		return "exit " + strconv.Itoa(*s.tool.ExitCode)
	}
	return "failed"
}

// composer is the box to write to an agent, as chat apps have: the text
// grows to a few lines; Return sends it and Shift+Return breaks the line.
// info, when set, fills the left of its bottom row.
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
	ui.Row(c.Key(cm.key)).Justify(ui.Center).Padding(0, 24, 18, 24).Shrink(0).Children(func() {
		box := ui.Column(c).Grow(1).MinWidth(0).MaxWidth(readW).Radius(18).Cursor(ui.CursorText)
		var in ui.Element
		box.Children(func() {
			in = ui.TextAreaBase(c.Key("in"), cm.draft).MinHeight(46).MaxHeight(220).Padding(14, 16, 4, 16).
				FontSize(14).LineHeight(1.45).Placeholder(cm.placeholder).Label(cm.placeholder).
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
			ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(4, 8, 8, 14).Children(func() {
				if cm.info != nil {
					cm.info()
				}
				ui.Spacer(c)
				ui.Text(c, "Return to send, Shift+Return for a new line").FontSize(11.5).TextColor(pal.textFaint).SingleLine()
				b := ui.ButtonBase(c.Key("send")).Size(30, 30).Radius(15).Center().Label("Send").Disabled(!ready).Tooltip("Send")
				bg, fg := pal.chip, pal.textFaint
				if ready {
					bg, fg = pal.accent, ui.Hex("#ffffff")
					if b.Hovered() {
						bg = bg.Mix(ui.Hex("#ffffff"), 0.1)
					}
				}
				b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond})).TextColor(fg)
				b.Children(func() {
					if cm.busy {
						statusIcon(c, pal, "running", false, 14)
					} else {
						ui.Icon(c, sendIcon).Size(14, 14)
					}
				})
				if b.Clicked() && ready {
					submit(*cm.draft)
				}
			})
		})
		// Kero's text field: a hairline, and the accent glow on focus.
		stroke, bg := ui.RGBA(0, 0, 0, 0.13), ui.Hex("#ffffff")
		if pal.dark {
			stroke, bg = ui.RGBA(255, 255, 255, 0.13), ui.RGBA(255, 255, 255, 0.06)
		}
		box.Background(bg).InsetShadow(0, 0, 0, 1, stroke)
		if glow := box.Animate("glow", b2f(in.Focused()), 120*time.Millisecond); glow > 0 {
			box.Shadow(0, 0, 0, 3.5*glow, pal.accent.Alpha(0.32*glow)).InsetShadow(0, 0, 0, 1, pal.accent.Alpha(0.75*glow))
		}
	})
}
