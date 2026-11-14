package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// Layout. The title bar has no frame: the traffic lights sit over the
// sidebar, and the content centers on barMid, the line through them.
const (
	barMid  = 30
	topBar  = 2 * barMid             // the sidebar's top bar
	cardGap = 8                      // the backdrop around the card
	headerH = 2 * (barMid - cardGap) // a page header, centered on barMid
	rowH    = 34                     // a sidebar row
	rowH2   = 48                     // a sidebar row of two lines: a job
	cardR   = 14                     // the card's corners
)

var sidebarMotion = ui.Animation{Duration: 260 * time.Millisecond, Ease: ui.CubicBezier(0.25, 1, 0.5, 1)}

// view is the window: the sidebar on the backdrop at the left, and the
// page on a card at the right.
func (d *desktop) view(c *ui.Context) {
	setTheme(d.prefs.Theme)
	pal := paletteFor(c)
	c.SetTheme(themed(c, pal))
	bar := c.TitleBar()

	row := ui.Row(c).Fill().AlignItems(ui.Stretch)
	backdrop(c, row)
	// A curve, not a spring: whole pixels on a long spring tail shake.
	progress := min(max(row.AnimateTo("sidebar", b2f(!d.prefs.SidebarHidden), sidebarMotion), 0), 1)
	width := d.prefs.SidebarWidth
	sideW := round(width * progress)
	d.shown = sideW / width
	mainPad := round(cardGap * (1 - d.shown)) // the card meets the sidebar when it shows
	row.Children(func() {
		// The sidebar keeps its width and slides out under a clip.
		ui.Row(c.Key("sidebar")).Width(sideW).Shrink(0).AlignItems(ui.Stretch).ClipX().Children(func() {
			if sideW == 0 {
				return // hidden: nothing to build
			}
			ui.Column(c).Width(width).Shrink(0).Margin(0, 0, 0, sideW-width).Opacity(0.4 + 0.6*d.shown).
				Children(func() { d.sidebar(c, pal, bar) })
		})
		ui.Column(c.Key("main")).Grow(1).MinWidth(0).Padding(cardGap, cardGap, cardGap, mainPad).Children(func() {
			if d.shown > 0.01 {
				d.resizeHandle(c)
			}
			card := paned(ui.Column(c.Key("card")).Grow(1).MinHeight(0).Radius(cardR).Background(pal.card), pal).Clip()
			card.Children(func() {
				d.statusBanner(c, pal)
				d.pageView(c)
			})
		})
	})
	d.paletteView(c, pal)
	d.confirmView(c, pal)
}

// pageView is the page in view.
func (d *desktop) pageView(c *ui.Context) {
	switch d.page {
	case pageJobs:
		d.jobsPage(c)
	case pageAgents:
		d.agentsPage(c)
	case pageMachines:
		d.machinesPage(c)
	case pageSettings:
		d.settingsPage(c)
	default:
		d.chatPage(c)
	}
}

// statusBanner shows the last error until the next action or a dismiss.
func (d *desktop) statusBanner(c *ui.Context, pal *palette) {
	if d.status == "" {
		return
	}
	ui.Row(c.Key("status")).Gap(10).Padding(8, 12, 8, 16).AlignItems(ui.Center).Background(pal.dangerBg).Children(func() {
		ui.Text(c, d.status).FontSize(12.5).TextColor(pal.danger).Grow(1).Selectable()
		if button(c, "dismiss", "Dismiss", plainButton, false) {
			d.status = ""
		}
	})
}

// --- shared pieces ---

// header is the top of a page: its title and subtitle, on the line of
// the traffic lights, and its buttons. With the sidebar hidden it starts
// after the traffic lights, with a button to show the sidebar again.
func (d *desktop) header(c *ui.Context, title, sub string, actions func()) {
	pal := paletteFor(c)
	bar := c.TitleBar()
	left := float32(16)
	ui.Row(c.Key("header")).Height(headerH).Shrink(0).Gap(10).AlignItems(ui.Center).Padding(0, 12, 0, left).DragWindow().Children(func() {
		if d.shown < 0.5 {
			if bar.Left > 0 {
				ui.Box(c).Width(max(bar.Left-cardGap-left, 0)).Shrink(0)
			}
			iconButton(c, "show-sidebar", sidebarIcon, tip("Show Sidebar", "sidebar.toggle"), pal, func() { d.run("sidebar.toggle") })
		}
		ui.Text(c, title).FontSize(14).FontWeight(600).TextColor(pal.text).SingleLine().Shrink(0)
		ui.Text(c, sub).FontSize(12).TextColor(pal.textMuted).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		if actions != nil {
			actions()
		}
	})
}

// emptyState fills a page that has nothing to show: an icon, a title, a
// line of help and, if given, a command to copy.
func emptyState(c *ui.Context, pal *palette, icon *ui.SVG, title, detail, command string) {
	ui.Column(c.Key("empty")).Fill().Center().Padding(24).Children(func() {
		ui.Box(c).Size(52, 52).Radius(14).Center().Background(pal.chip).TextColor(pal.textMuted).Margin(0, 0, 18, 0).Children(func() {
			ui.Icon(c, icon).Size(26, 26)
		})
		ui.Text(c, title).FontSize(19).FontWeight(650).TextColor(pal.text)
		ui.Text(c, detail).FontSize(13.5).TextColor(pal.textMuted).MaxWidth(400).TextAlign(ui.Center).LineHeight(1.45).Margin(8, 0, 0, 0)
		if command != "" {
			ui.Text(c, command).Font("monospace").FontSize(12).TextColor(pal.text).Selectable().
				Padding(10, 14).Radius(10).Background(pal.chip).Margin(18, 0, 0, 0)
		}
	})
}

// iconButton is a quiet square button with an icon, as in a toolbar.
func iconButton(c *ui.Context, key string, svg *ui.SVG, label string, pal *palette, fn func()) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Size(28, 26).Radius(6).Center().TextColor(pal.textMuted).Label(label).Tooltip(label)
	if b.Pressed() {
		b.Background(pal.rowHover.Alpha(0.12))
	} else if b.Hovered() {
		b.Background(pal.rowHover).TextColor(pal.text)
	}
	b.Children(func() { ui.Icon(c, svg).Size(16, 16) })
	if b.Clicked() {
		fn()
	}
	return b
}

// stateBadge is a colored pill for a job state.
func stateBadge(c *ui.Context, state string) {
	pal := paletteFor(c)
	b := ui.Badge(c, state)
	switch state {
	case "done":
		b.Background(pal.success).TextColor(pal.onAccent)
	case "failed":
		b.Background(pal.danger).TextColor(pal.onAccent)
	case "running":
		b.Background(pal.accent).TextColor(pal.onAccent)
	}
}

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// round rounds to whole pixels, so edges stay crisp while they move.
func round(v float32) float32 { return float32(math.Round(float64(v))) }
