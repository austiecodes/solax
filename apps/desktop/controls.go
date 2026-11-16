package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// The app's own controls, after the Codex app's: flat buttons with a
// hairline edge, the main one filled in the ink; a field with a hairline
// that darkens on focus; and the job status icon. Pages use these, not
// ui.Button or ui.TextInput, so the look changes in one place.

const controlH = 28

// face fills a control flat, with a hairline edge.
func face(e ui.Element, pal *palette, bg ui.Color) ui.Element {
	return e.Background(bg).InsetShadow(0, 0, 0, 1, pal.text.Alpha(0.1))
}

// buttonKind is the look of a button.
type buttonKind int

const (
	plainButton   buttonKind = iota // flat, in the control color
	primaryButton                   // filled in the ink: the main action of a view
	dangerButton                    // red: an action that cannot be undone
	removeButton                    // flat, red text: asks before it removes
)

// button is a button of a kind. A disabled one fades and takes no click.
// It reports a click.
func button(c *ui.Context, key, label string, kind buttonKind, disabled bool) bool {
	return buttonBase(c, key, label, kind, disabled).Clicked() && !disabled
}

// buttonBase is button, returning the element, as for AutoFocus.
func buttonBase(c *ui.Context, key, label string, kind buttonKind, disabled bool) ui.Element {
	pal := paletteFor(c)
	b := ui.ButtonBase(c.Key(key)).Height(controlH).Padding(0, 12).Radius(8).AlignItems(ui.Center).Shrink(0).
		Label(label).Disabled(disabled)
	bg, fg := pal.control, pal.text
	switch kind {
	case primaryButton:
		bg, fg = pal.text, pal.card
	case dangerButton:
		bg, fg = pal.danger, ui.Hex("#ffffff")
	case removeButton:
		fg = pal.danger
	}
	filled := kind == primaryButton || kind == dangerButton
	switch {
	case disabled:
		b.Opacity(0.4)
	case b.Pressed():
		bg = bg.Mix(pal.card, 0.2)
	case b.Hovered() && filled:
		bg = bg.Mix(pal.card, 0.12)
	case b.Hovered():
		bg = pal.controlHover
	}
	bg = b.AnimateColor("bg", bg, ui.Animation{Duration: 90 * time.Millisecond})
	if filled {
		b.Background(bg)
	} else {
		face(b, pal, bg)
	}
	b.TextColor(fg).Children(func() {
		ui.Text(c, label).FontSize(13).FontWeight(500).SingleLine()
	})
	return b
}

// textField edits *value in a rounded field. It returns the input, for
// Submitted and Password.
func textField(c *ui.Context, key string, value *string, placeholder string) ui.Element {
	var in ui.Element
	frame := ui.Row(c.Key(key)).Grow(1).MinWidth(0).Height(controlH + 2).AlignItems(ui.Center)
	frame.Children(func() {
		in = ui.TextInputBase(c.Key("in"), value).Grow(1).MinWidth(0).AlignSelf(ui.Stretch).Padding(0, 10).FontSize(13).
			Placeholder(placeholder).Label(placeholder)
	})
	return fieldFace(c, frame, in)
}

// textArea edits *value on a few lines, h tall, in the look of
// textField.
func textArea(c *ui.Context, key string, value *string, placeholder string, h float32) ui.Element {
	var in ui.Element
	frame := ui.Column(c.Key(key)).Grow(1).MinWidth(0)
	frame.Children(func() {
		in = ui.TextAreaBase(c.Key("in"), value).Height(h).Padding(8, 10).FontSize(13).LineHeight(1.45).
			Placeholder(placeholder).Label(placeholder)
	})
	return fieldFace(c, frame, in)
}

// fieldFace gives the frame of a text control a hairline edge, which
// darkens while in has the focus. It returns in.
func fieldFace(c *ui.Context, frame, in ui.Element) ui.Element {
	pal := paletteFor(c)
	focus := frame.Animate("focus", b2f(in.Focused()), 120*time.Millisecond)
	frame.Radius(8).Cursor(ui.CursorText).Background(pal.composer).InsetShadow(0, 0, 0, 1, pal.text.Alpha(0.12+0.2*focus))
	return in
}

// selectField chooses one of options into *value: a raised button with
// the choice and an arrow, which opens a list of the options. It returns
// the button, for Changed.
func selectField(c *ui.Context, key string, value *string, options []string) ui.Element {
	pal := paletteFor(c)
	sel := ui.SelectBase(c.Key(key), value)
	tr := sel.Trigger.Grow(1).MinWidth(0).Height(controlH).Padding(0, 10, 0, 12).Gap(8).Radius(8).AlignItems(ui.Center).Label(*value)
	bg := pal.control
	if tr.Hovered() {
		bg = pal.controlHover
	}
	face(tr, pal, tr.AnimateColor("bg", bg, ui.Animation{Duration: 90 * time.Millisecond})).TextColor(pal.text)
	tr.Children(func() {
		ui.Text(c, *value).FontSize(13).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		ui.Icon(c, chevronIcon).Size(9, 9).Rotate(90).TextColor(pal.textMuted).Shrink(0)
	})
	sel.Popup(func(panel ui.Element) {
		floating(panel.Margin(4, 0, 0, 0).Padding(4).Radius(10).Background(pal.popover), pal)
		for _, opt := range options {
			item := sel.Item(opt).Height(28).Padding(0, 10).Radius(6).AlignItems(ui.Center).TextColor(pal.text)
			switch {
			case item.Highlighted():
				item.Background(pal.rowSelected)
			case opt == *value:
				item.Background(pal.rowHover)
			}
			item.Children(func() { ui.Text(c, opt).FontSize(13).SingleLine() })
		}
	})
	return tr
}

// jobLive tells whether a job state still changes.
func jobLive(state string) bool { return state == "queued" || state == "running" }

// statusIcon draws a job state, as the sidebar shows it: a faint ring
// while queued, a turning ring while it runs, a check when done (filled
// while unread), a red disc when failed, a crossed ring when cancelled.
func statusIcon(c *ui.Context, pal *palette, state string, unread bool, size float32) ui.Element {
	box := ui.Box(c).Size(size, size).Shrink(0).Label(state)
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		rad := min(r.W, r.H)/2 - 1
		ring := func(w float32, col ui.Color) {
			var path ui.Path
			path.Circle(cx, cy, rad)
			p.StrokePath(&path, w, col)
		}
		disc := func(col ui.Color) {
			var path ui.Path
			path.Circle(cx, cy, rad+0.5)
			p.FillPath(&path, col)
		}
		switch state {
		case "queued":
			ring(1.5, pal.textFaint)
		case "running":
			ring(1.7, pal.text.Alpha(0.13))
			p.AnimationFrame()
			deg := float32(p.Now().UnixMilli()%1000) / 1000 * 360
			p.Transform(ui.Rotation(deg), cx, cy, func() {
				var arc ui.Path
				arc.Circle(cx, cy, rad)
				p.StrokePath(arc.Trim(0, 0.3), 1.7, pal.text)
			})
		case "failed":
			col := pal.danger
			if !unread {
				col = col.Alpha(0.75)
			}
			disc(col)
			p.Line(cx, cy-rad*0.45, cx, cy+rad*0.08, 1.7, pal.onDone)
			var dot ui.Path
			dot.Circle(cx, cy+rad*0.42, 1)
			p.FillPath(&dot, pal.onDone)
		case "cancelled":
			ring(1.5, pal.textFaint)
			k := rad * 0.38
			p.Line(cx-k, cy-k, cx+k, cy+k, 1.5, pal.textFaint)
			p.Line(cx+k, cy-k, cx-k, cy+k, 1.5, pal.textFaint)
		default: // done
			if unread {
				disc(pal.done)
				check := (&ui.Path{}).MoveTo(cx-rad*0.42, cy+0.2).LineTo(cx-rad*0.08, cy+rad*0.36).LineTo(cx+rad*0.46, cy-rad*0.34)
				p.StrokePath(check, 1.6, pal.onDone)
				return
			}
			check := (&ui.Path{}).MoveTo(cx-rad*0.62, cy+0.4).LineTo(cx-rad*0.12, cy+rad*0.5).LineTo(cx+rad*0.66, cy-rad*0.46)
			p.StrokePath(check, 1.5, pal.textFaint)
		}
	})
	return box
}
