package main

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/locales"
)

// The page kit, after the Codex app's settings. A page is a header over a
// column of sections. A section is a title and a line of help, over a
// box with a hairline edge that holds its rows, split by hairlines. A row is a label with its help at the left and its
// control at the right. The pages use this kit, not raw boxes, so the
// spacing is the same on all pages and changes in one place.

const (
	gutter     = 20  // the inset of a page's header and content from the page's edge
	pageW      = 760 // the widest that a page's column grows
	sectionPad = 16  // the inset of a row in a section's box
	fieldW     = 260 // the width of a control in a row
	itemH      = 44  // a row of a list in a section
)

// pageSpec is the header of a page.
type pageSpec struct {
	icon    *ui.SVG // nil for none
	title   string
	sub     string // muted, after the title
	actions func() // the buttons at the right
}

// showPage draws the header and, under it, body: the sections of the page in
// a column that scrolls.
func (d *desktop) showPage(c *ui.Context, p pageSpec, body func()) {
	d.header(c, p)
	ui.Scroll(c.Key("page")).Grow(1).MinHeight(0).Children(func() {
		ui.Row(c).Justify(ui.Center).Padding(8, gutter, 32, gutter).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).MaxWidth(pageW).Gap(28).Children(body)
		})
	})
}

// header is the top of a page, as the Codex app's: its icon, title and
// subtitle, centered on the line of the traffic lights, and its buttons.
// No hairline: the page scrolls under it and fades out. With the
// sidebar hidden it starts after the traffic lights, with a button to
// show the sidebar again.
func (d *desktop) header(c *ui.Context, p pageSpec) {
	pal := paletteFor(c)
	bar := c.TitleBar()
	ui.Row(c.Key("header")).Height(headerH).Shrink(0).Gap(8).AlignItems(ui.Center).Padding(0, 12, 0, gutter).
		DragWindow().Children(func() {
		if d.shown < 0.5 {
			if bar.Left > 0 {
				ui.Box(c).Width(max(bar.Left-gutter, 0)).Shrink(0)
			}
			iconButton(c, "show-sidebar", sidebarIcon, tip(locales.ShowSidebar(), "sidebar.toggle"), pal, func() { d.run("sidebar.toggle") })
		}
		if p.icon != nil {
			ui.Icon(c, p.icon).Size(14, 14).TextColor(pal.textMuted).Shrink(0)
		}
		ui.Text(c, p.title).FontSize(13).FontWeight(600).TextColor(pal.text).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		ui.Text(c, p.sub).FontSize(13).TextColor(pal.textFaint).SingleLine().Ellipsis("…").Grow(1).Shrink(2).MinWidth(0)
		if p.actions != nil {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Children(p.actions)
		}
	})
}

// --- sections ---

// sectionSpec is the head of a section. All fields are optional.
type sectionSpec struct {
	title   string
	count   string  // faint, after the title, as "3"
	desc    string  // a line of help under the title
	lead    func()  // before the title, as an avatar
	actions func()  // at the right of the title
	footer  func()  // the buttons under the rows, at the right
	rows    rowKind // how the rows show
}

// rowKind is how the rows of a section show.
type rowKind int

const (
	settingRows rowKind = iota // settings: a label at the left, a control at the right
	listRows                   // a list: each row lit when hovered, filled when chosen
)

// section draws a head (its title, its count, its buttons and a line of
// help), then a box with the rows that body builds (row, stackRow,
// listItem, note), split by hairlines, then the footer's buttons.
func section(c *ui.Context, key string, s sectionSpec, body func()) {
	pal := paletteFor(c)
	ui.Column(c.Key(key)).Gap(10).Children(func() {
		if s.title != "" || s.actions != nil || s.desc != "" {
			ui.Column(c).Gap(2).Padding(0, 2).Children(func() {
				if s.title != "" || s.actions != nil {
					ui.Row(c).Gap(8).AlignItems(ui.Center).MinHeight(28).Children(func() {
						if s.lead != nil {
							s.lead()
						}
						ui.Text(c, s.title).FontSize(14).FontWeight(600).TextColor(pal.text).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
						if s.count != "" {
							ui.Text(c, s.count).FontSize(13).TextColor(pal.textFaint).FontFeatures("tnum").SingleLine().Shrink(0)
						}
						ui.Spacer(c)
						if s.actions != nil {
							ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Children(s.actions)
						}
					})
				}
				if s.desc != "" {
					ui.Text(c, s.desc).FontSize(12.5).LineHeight(1.45).TextColor(pal.textMuted)
				}
			})
		}
		ui.Column(c).Radius(12).Border(1, pal.divider).Background(pal.panel).Clip().Dividers(1, pal.divider).Children(body)
		if s.footer != nil {
			ui.Row(c).Gap(8).Justify(ui.End).AlignItems(ui.Center).Children(s.footer)
		}
	})
}

// row is a setting: label and help at the left, control at the right.
func row(c *ui.Context, key, label, help string, control func()) {
	pal := paletteFor(c)
	ui.Row(c.Key(key)).Gap(20).AlignItems(ui.Center).MinHeight(52).Padding(10, sectionPad).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, label).FontSize(13).TextColor(pal.text)
			if help != "" {
				ui.Text(c, help).FontSize(12).LineHeight(1.4).TextColor(pal.textMuted)
			}
		})
		if control != nil {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(0).Children(control)
		}
	})
}

// stackRow is a setting whose control takes the whole width, under its
// label: a text area.
func stackRow(c *ui.Context, key, label, help string, control func()) {
	pal := paletteFor(c)
	ui.Column(c.Key(key)).Gap(8).Padding(12, sectionPad).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, label).FontSize(13).TextColor(pal.text)
			if help != "" {
				ui.Text(c, help).FontSize(12).LineHeight(1.4).TextColor(pal.textMuted)
			}
		})
		control()
	})
}

// valueText is the value of a read-only row, at its right.
func valueText(c *ui.Context, text string, mono bool) ui.Element {
	pal := paletteFor(c)
	t := ui.Text(c, text).FontSize(13).TextColor(pal.textMuted).SingleLine().Ellipsis("…").MaxWidth(380).Selectable()
	if mono {
		t.Font("monospace").FontSize(12)
	}
	return t
}

// listItem is a row of a list section: lit when hovered, filled when
// chosen. Build its content in fn; check Clicked on the result.
func listItem(c *ui.Context, key string, selected bool, label string, fn func()) ui.Element {
	pal := paletteFor(c)
	r := ui.ButtonBase(c.Key(key)).MinHeight(itemH).Padding(0, sectionPad).Gap(10).AlignItems(ui.Center).Label(label)
	bg := ui.Transparent
	switch {
	case selected:
		bg = pal.rowSelected
	case r.Hovered():
		bg = pal.rowHover
	}
	r.Background(r.AnimateColor("bg", bg, ui.Animation{Duration: 110 * time.Millisecond}))
	r.Children(fn)
	return r
}

// note is a quiet line in a section, as when a list is empty.
func note(c *ui.Context, text string) {
	pal := paletteFor(c)
	ui.Text(c, text).FontSize(13).LineHeight(1.45).TextColor(pal.textMuted).Padding(14, sectionPad)
}

// --- small pieces ---

// mark is the color of a chip or a dot.
type mark int

const (
	markQuiet mark = iota
	markAccent
	markGood
	markWarn
	markBad
)

func (m mark) color(pal *palette) ui.Color {
	switch m {
	case markAccent:
		return pal.accent
	case markGood:
		return pal.success
	case markWarn:
		return pal.attention
	case markBad:
		return pal.danger
	}
	return pal.textMuted
}

// chip is a small tag: a driver, a role, a state. It is a tint of its
// mark, not a solid fill, so a row of chips stays quiet.
func chip(c *ui.Context, text string, m mark) ui.Element {
	pal := paletteFor(c)
	col := m.color(pal)
	bg := col.Alpha(0.14)
	if m == markQuiet {
		bg = pal.chip
	}
	return ui.Text(c, text).FontSize(11.5).FontWeight(500).TextColor(col).SingleLine().Shrink(0).
		Padding(2, 7).Radius(6).Background(bg)
}

// dot is a small disc in a mark: online, offline.
func dot(c *ui.Context, m mark) ui.Element {
	pal := paletteFor(c)
	col := m.color(pal)
	if m == markQuiet {
		col = pal.textFaint
	}
	return ui.Box(c).Size(8, 8).Radius(4).Shrink(0).Background(col)
}

// avatar is the badge before the title of a detail section: an icon on a
// tint of the mark.
func avatar(c *ui.Context, svg *ui.SVG, m mark) {
	pal := paletteFor(c)
	col := m.color(pal)
	ui.Box(c).Size(24, 24).Radius(7).Center().Shrink(0).Background(col.Alpha(0.14)).TextColor(col).Children(func() {
		ui.Icon(c, svg).Size(13, 13)
	})
}

// onOff is the mark and the word of an online state.
func onOff(online bool) (mark, string) {
	if online {
		return markGood, locales.Online()
	}
	return markQuiet, locales.Offline()
}
