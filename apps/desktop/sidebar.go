package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/pkg/protocol"
)

// The sidebar: the app name by the traffic lights, the pages, the jobs
// with their state (as kero lists its sessions), and a footer with the
// counts that need the user and the connection.

// trafficLightsEnd is where the traffic lights end: the title bar reports
// less than that on some systems.
const trafficLightsEnd = 72

func (d *desktop) sidebar(c *ui.Context, pal *palette, bar ui.TitleBar) {
	// The top bar clears the traffic lights and moves the window.
	ui.Row(c).Height(topBar).Shrink(0).AlignItems(ui.Center).Gap(6).Padding(0, 10, 0, max(bar.Left, trafficLightsEnd)+12).DragWindow().Children(func() {
		ui.Text(c, "Solax").FontSize(14).FontWeight(650).TextColor(pal.text).SingleLine()
		ui.Spacer(c)
		iconButton(c, "hide-sidebar", sidebarIcon, tip("Hide Sidebar", "sidebar.toggle"), pal, func() { d.run("sidebar.toggle") })
	})
	ui.Column(c.Key("nav")).Shrink(0).Padding(0, 10).Gap(1).Children(func() {
		for _, p := range navPages {
			d.navRow(c, pal, p)
		}
	})
	// The space around the rows moves the window, as the top bar does.
	list := ui.Scroll(c.Key("jobs")).Grow(1).Padding(0, 10, 12, 10).Gap(1).DragWindow()
	list.Children(func() {
		if len(d.jobs) == 0 {
			return
		}
		ui.Text(c, "Recent jobs").FontSize(11.5).FontWeight(600).TextColor(pal.textFaint).Padding(14, 10, 4, 10)
		for _, j := range d.jobs {
			d.jobRow(c, pal, j)
		}
	})
	d.sidebarFooter(c, pal)
	c.After(20 * time.Second) // the "ago" times move on
}

// navRow is a page in the sidebar: its icon, its title, and what it has
// to say (a count, or that it works).
func (d *desktop) navRow(c *ui.Context, pal *palette, p navPage) {
	label := title(p.action)
	selected := d.page == p.id && (p.id != pageJobs || d.jobSel == "")
	row := sideRow(c, pal, p.id, rowH, selected).Label(label).Tooltip(tip(label, p.action))
	fg := pal.textMuted
	if selected {
		fg = pal.text
	}
	row.Children(func() {
		ui.Icon(c, p.icon).Size(15, 15).TextColor(fg).Shrink(0)
		ui.Text(c, label).FontSize(13.5).TextColor(pal.text).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		switch p.id {
		case pageChat:
			if d.superBusy {
				statusIcon(c, pal, "running", false, 13)
			}
		case pageJobs:
			if n := d.countJobs(func(j protocol.Job) bool { return jobLive(j.State) }); n > 0 {
				ui.Textf(c, "%d", n).FontSize(12).FontWeight(500).TextColor(pal.textMuted).FontFeatures("tnum")
			}
		}
	})
	if row.Clicked() && !selected {
		d.run(p.action)
	}
}

// jobRow is a job in the sidebar, on two lines: its title and state, then
// its agent and age. An unread job is bold.
func (d *desktop) jobRow(c *ui.Context, pal *palette, j protocol.Job) {
	selected := d.page == pageJobs && d.jobSel == j.ID
	unread := d.unread[j.ID]
	row := sideRow(c, pal, j.ID, rowH2, selected).Label(j.Title).Tooltip(clip(j.Prompt, 300))
	row.Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				name := ui.Text(c, j.Title).FontSize(13.5).TextColor(pal.text).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				if unread {
					name.FontWeight(650)
				}
				ui.Spacer(c)
				statusIcon(c, pal, j.State, unread, 14)
			})
			ui.Text(c, j.Agent+" · "+ago(j.CreatedAt)).FontSize(12).TextColor(pal.textFaint).SingleLine().Ellipsis("…").MinWidth(0)
		})
	})
	if row.Clicked() && !selected {
		d.openJob(j.ID)
	}
}

// sideRow is a row of the sidebar: rounded, lit when hovered, lifted
// when selected.
func sideRow(c *ui.Context, pal *palette, key string, h float32, selected bool) ui.Element {
	row := ui.ButtonBase(c.Key(key)).Height(h).Padding(0, 10).Gap(9).AlignItems(ui.Center).Radius(8).Shrink(0)
	if pal.dark {
		row.Border(1, ui.Transparent) // room for the chosen row's edge: no text moves
	}
	bg := ui.Transparent
	switch {
	case selected:
		bg = pal.rowSelected
		chosen(row, pal)
	case row.Hovered():
		bg = pal.rowHover
	}
	return row.Background(row.AnimateColor("bg", bg, ui.Animation{Duration: 110 * time.Millisecond}))
}

// sidebarFooter has the reload and settings buttons, then what needs the
// user: counts of running, failed and finished jobs, each a jump to the
// next of them. Without a connection it says so instead.
func (d *desktop) sidebarFooter(c *ui.Context, pal *palette) {
	ui.Row(c).Height(40).Shrink(0).Padding(0, 10, 4, 10).Gap(2).AlignItems(ui.Center).DragWindow().Children(func() {
		gear := iconButton(c, "settings", gearIcon, tip("Settings", "settings.open"), pal, func() { d.run("settings.open") })
		if d.page == pageSettings {
			gear.Background(pal.rowSelected).TextColor(pal.text)
			chosen(gear, pal)
		}
		iconButton(c, "palette", searchIcon, tip("Go to", "palette.open"), pal, func() { d.run("palette.open") })
		iconButton(c, "reload", reloadIcon, tip("Reload", "data.reload"), pal, func() { d.run("data.reload") })
		ui.Spacer(c)
		if d.conn.State != "connected" {
			d.connNote(c, pal)
			return
		}
		d.footerChip(c, pal, "running", false, "%d queued or running", func(j protocol.Job) bool { return jobLive(j.State) })
		d.footerChip(c, pal, "failed", true, "%d failed", func(j protocol.Job) bool { return j.State == "failed" && d.unread[j.ID] })
		d.footerChip(c, pal, "done", true, "%d done, unread", func(j protocol.Job) bool { return j.State == "done" && d.unread[j.ID] })
	})
}

// footerChip counts the jobs that match, with their state icon. A click
// opens the next of them. It shows nothing while none match.
func (d *desktop) footerChip(c *ui.Context, pal *palette, state string, unread bool, say string, is func(protocol.Job) bool) {
	n := d.countJobs(is)
	if n == 0 {
		return
	}
	words := fmt.Sprintf(say, n)
	b := ui.ButtonBase(c.Key("chip-"+state)).Height(24).Padding(0, 8, 0, 6).Gap(5).Radius(12).AlignItems(ui.Center).
		Label(words).Tooltip(words + " · open the next")
	if b.Hovered() {
		b.Background(pal.rowHover)
	}
	b.Children(func() {
		statusIcon(c, pal, state, unread, 12)
		ui.Textf(c, "%d", n).FontSize(12).FontWeight(500).TextColor(pal.textMuted).FontFeatures("tnum")
	})
	if b.Clicked() {
		d.jumpTo(is)
	}
}

// connNote says how the stream to the server is, by a dot. The tooltip
// names the server and the last error.
func (d *desktop) connNote(c *ui.Context, pal *palette) {
	dot := pal.danger
	if d.conn.State == "connecting" {
		dot = pal.attention
	}
	tipText := d.conn.Server
	if d.conn.Error != "" {
		tipText += "\n" + d.conn.Error
	}
	note := ui.Row(c.Key("conn")).Gap(7).Padding(0, 6).AlignItems(ui.Center).MinWidth(0).Shrink(1)
	if tipText != "" {
		note.Tooltip(tipText)
	}
	note.Children(func() {
		ui.Box(c).Size(7, 7).Radius(3.5).Background(dot).Shrink(0)
		ui.Text(c, d.conn.State).FontSize(12).TextColor(pal.textMuted).SingleLine().Ellipsis("…").MinWidth(0)
	})
}

// resizeHandle is the edge of the sidebar, which a drag moves and a double
// click resets.
func (d *desktop) resizeHandle(c *ui.Context) {
	h := ui.Box(c.Key("resize")).Absolute().Left(-4).Top(0).Bottom(0).Width(9).Cursor(ui.CursorResizeColumn).KeepFocus()
	if dx, _, ok := h.Dragged(); ok {
		if d.sideDrag == 0 {
			d.sideDrag = d.prefs.SidebarWidth
		}
		d.sideDrag += dx
		d.prefs.SidebarWidth = min(max(d.sideDrag, minSidebar), maxSidebar)
	} else if d.sideDrag != 0 && !h.Pressed() {
		d.sideDrag = 0
		d.savePrefs()
	}
	if h.DoubleClicked() {
		d.prefs.SidebarWidth = defaultSidebar
		d.savePrefs()
	}
}

func (d *desktop) countJobs(is func(protocol.Job) bool) int {
	n := 0
	for _, j := range d.jobs {
		if is(j) {
			n++
		}
	}
	return n
}

// jumpTo opens the next job that matches, after the one open, in sidebar
// order, and wraps around.
func (d *desktop) jumpTo(is func(protocol.Job) bool) {
	start := 0
	for i, j := range d.jobs {
		if j.ID == d.jobSel {
			start = i + 1
		}
	}
	for k := range d.jobs {
		j := d.jobs[(start+k)%len(d.jobs)]
		if is(j) {
			d.openJob(j.ID)
			return
		}
	}
}
