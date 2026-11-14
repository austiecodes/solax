package main

import "github.com/egoist/mygo/ui"

// confirmation is a question the app asks before an action it cannot
// undo. Pages call ask; the window shows the dialog over everything.
type confirmation struct {
	open    bool
	title   string
	detail  string
	action  string // the label of the button that runs it
	keep    string // the label of the button that closes it, "Cancel" by default
	destroy bool   // the button is red
	run     func()
}

// ask asks a question before run. Return confirms; Escape, Cancel or a
// click outside closes it. It returns the question, to set more of it.
func (d *desktop) ask(title, detail, action string, destroy bool, run func()) *confirmation {
	d.confirm = &confirmation{open: true, title: title, detail: detail, action: action, destroy: destroy, run: run}
	return d.confirm
}

func (d *desktop) confirmView(c *ui.Context, pal *palette) {
	cf := d.confirm
	if cf == nil {
		return
	}
	run := false
	ui.DialogBase(c.Key("confirm"), &cf.open, func(backdrop, panel ui.Element) {
		backdrop.Background(pal.overlay)
		floating(panel.Width(380).Padding(22).Gap(8).Radius(14).Background(pal.popover), pal).Label(cf.title)
		ui.Text(c, cf.title).FontSize(15).FontWeight(650).TextColor(pal.text)
		ui.Text(c, cf.detail).FontSize(13).TextColor(pal.textMuted).LineHeight(1.4)
		ui.Row(c).Gap(8).Justify(ui.End).Margin(14, 0, 0, 0).Children(func() {
			keep := cf.keep
			if keep == "" {
				keep = "Cancel"
			}
			if button(c, "cancel", keep, plainButton, false) {
				cf.open = false
			}
			kind := primaryButton
			if cf.destroy {
				kind = dangerButton
			}
			// The answer takes the focus as the dialog opens, so Return
			// runs it, not a button that had the focus before.
			if buttonBase(c, "ok", cf.action, kind, false).AutoFocus().Clicked() || c.Shortcut(0, ui.KeyEnter) {
				run = true
			}
		})
	})
	if run {
		cf.open = false
		cf.run()
	}
	if !cf.open {
		d.confirm = nil
	}
}
