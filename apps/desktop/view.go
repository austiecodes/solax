package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/locales"
)

// Layout. The title bar has no frame: the traffic lights sit over the
// sidebar, and the content centers on barMid, the line through them.
const (
	barMid  = 26         // the line through the traffic lights
	topBar  = 2 * barMid // the sidebar's top bar
	headerH = topBar     // a page header, centered on barMid
	rowH    = 30         // a sidebar row
)

var sidebarMotion = ui.Animation{Duration: 260 * time.Millisecond, Ease: ui.CubicBezier(0.25, 1, 0.5, 1)}

// view is the window, flat as the Codex app: the sidebar at the left,
// the page at the right, and a hairline between them.
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
	row.Children(func() {
		// The sidebar keeps its width and slides out under a clip.
		ui.Row(c.Key("sidebar")).Width(sideW).Shrink(0).AlignItems(ui.Stretch).ClipX().Children(func() {
			if sideW == 0 {
				return // hidden: nothing to build
			}
			ui.Column(c).Width(width).Shrink(0).Margin(0, 0, 0, sideW-width).Opacity(0.4 + 0.6*d.shown).
				Children(func() { d.sidebar(c, pal, bar) })
		})
		ui.Column(c.Key("main")).Grow(1).MinWidth(0).Children(func() {
			if d.shown > 0.01 {
				d.resizeHandle(c)
			}
			page := ui.Column(c.Key("pane")).Grow(1).MinHeight(0).Background(pal.card).Clip()
			if sideW > 0 {
				page.BorderWidth(0, 0, 0, 1).BorderColor(pal.divider)
			}
			page.Children(func() {
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
	case pageTasks:
		d.tasksPage(c)
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
		if button(c, "dismiss", locales.Dismiss(), plainButton, false) {
			d.status = ""
		}
	})
}

// --- shared pieces ---

// emptySpec is what a page shows while it has nothing to show: an icon,
// a title, a line of help and, if given, a command to copy or a button
// that takes the next step.
type emptySpec struct {
	icon          *ui.SVG
	title, detail string
	command       string // a shell command, in a box to copy from
	action        string // the label of the button, "" for none
	run           func()
}

// emptyState fills a page that has nothing to show, centered and quiet,
// as the Codex app's.
func emptyState(c *ui.Context, pal *palette, e emptySpec) {
	ui.Column(c.Key("empty")).Fill().Center().Padding(24).Gap(8).Children(func() {
		ui.Icon(c, e.icon).Size(28, 28).TextColor(pal.textFaint).Margin(0, 0, 8, 0)
		ui.Text(c, e.title).FontSize(17).FontWeight(600).TextColor(pal.text).TextAlign(ui.Center)
		ui.Text(c, e.detail).FontSize(13).TextColor(pal.textMuted).MaxWidth(400).TextAlign(ui.Center).LineHeight(1.45)
		if e.command != "" {
			ui.Text(c, e.command).Font("monospace").FontSize(12).TextColor(pal.text).Selectable().
				Padding(8, 12).Radius(8).Background(pal.panel).Border(1, pal.divider).Margin(10, 0, 0, 0)
		}
		if e.action != "" {
			ui.Row(c).Margin(10, 0, 0, 0).Children(func() {
				if button(c, "empty-action", e.action, primaryButton, false) {
					e.run()
				}
			})
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

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// round rounds to whole pixels, so edges stay crisp while they move.
func round(v float32) float32 { return float32(math.Round(float64(v))) }
