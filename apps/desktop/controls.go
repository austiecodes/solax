package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// The app's own controls, after kero's: raised buttons that stand out by
// their fill, a rounded field with an accent glow on focus, and the job
// status icon. Pages use these, not ui.Button or ui.TextInput, so the
// look changes in one place.

const controlH = 30

// face fills a control: on a soft shadow in the light, on a lit edge in
// the dark.
func face(e ui.Element, pal *palette, bg ui.Color) ui.Element {
	e.Background(bg)
	if pal.controlShadow {
		return e.Shadow(0, 0.5, 1.5, 0, ui.RGBA(0, 0, 0, 0.13)).Shadow(0, 0, 0, 0.5, ui.RGBA(0, 0, 0, 0.05))
	}
	return darkShadow(litEdge(e, pal))
}

// buttonKind is the look of a button.
type buttonKind int

const (
	plainButton   buttonKind = iota // raised, in the control color
	primaryButton                   // the accent: the main action of a view
	dangerButton                    // red: an action that cannot be undone
)

// button is a raised button of a kind. A disabled one fades and takes no
// click. It reports a click.
func button(c *ui.Context, key, label string, kind buttonKind, disabled bool) bool {
	return buttonBase(c, key, label, kind, disabled).Clicked() && !disabled
}

// buttonBase is button, returning the element, as for AutoFocus.
func buttonBase(c *ui.Context, key, label string, kind buttonKind, disabled bool) ui.Element {
	pal := paletteFor(c)
	b := ui.ButtonBase(c.Key(key)).Height(controlH).Padding(0, 14).Radius(8).AlignItems(ui.Center).Shrink(0).
		Label(label).Disabled(disabled)
	bg, fg := pal.control, pal.text
	switch kind {
	case primaryButton:
		bg, fg = pal.accent, ui.Hex("#ffffff")
	case dangerButton:
		bg, fg = pal.danger, ui.Hex("#ffffff")
	}
	primary := kind != plainButton
	switch {
	case disabled:
		b.Opacity(0.5)
	case b.Pressed():
		bg = bg.Mix(ui.Hex("#000000"), 0.08)
	case b.Hovered() && primary:
		bg = bg.Mix(ui.Hex("#ffffff"), 0.08)
	case b.Hovered():
		bg = pal.controlHover
	}
	face(b, pal, b.AnimateColor("bg", bg, ui.Animation{Duration: 90 * time.Millisecond})).TextColor(fg)
	b.Children(func() {
		ui.Text(c, label).FontSize(13).FontWeight(500).SingleLine()
	})
	return b
}

// textField edits *value in a rounded field. It returns the input, for
// Submitted.
func textField(c *ui.Context, key string, value *string, placeholder string) ui.Element {
	pal := paletteFor(c)
	var in ui.Element
	frame := ui.Row(c.Key(key)).Grow(1).MinWidth(0).Height(controlH + 2).Radius(8).AlignItems(ui.Center).Cursor(ui.CursorText)
	frame.Children(func() {
		in = ui.TextInputBase(c.Key("in"), value).Grow(1).MinWidth(0).AlignSelf(ui.Stretch).Padding(0, 10).FontSize(13).
			Placeholder(placeholder).Label(placeholder)
	})
	stroke, bg := ui.RGBA(0, 0, 0, 0.13), ui.Hex("#ffffff")
	if pal.dark {
		stroke, bg = ui.RGBA(255, 255, 255, 0.13), ui.RGBA(255, 255, 255, 0.06)
	}
	frame.Background(bg).InsetShadow(0, 0, 0, 1, stroke)
	if glow := frame.Animate("glow", b2f(in.Focused()), 120*time.Millisecond); glow > 0 {
		frame.Shadow(0, 0, 0, 3.5*glow, pal.accent.Alpha(0.32*glow)).InsetShadow(0, 0, 0, 1, pal.accent.Alpha(0.75*glow))
	}
	return in
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
