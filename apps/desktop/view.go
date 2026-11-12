package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// view is the window: the sidebar on the left, the page on the right.
func (d *desktop) view(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Fill().Children(func() {
		ui.Column(c).Width(210).Padding(14, 10).Gap(10).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Children(func() {
			ui.Text(c, "Solax").Bold().FontSize(20).Padding(0, 6)
			page := d.page
			if ui.Sidebar(c, &page, func() {
				ui.SidebarItem(c, pageChat, nil, "Super Agent").Children(func() {
					if d.superBusy {
						ui.Badge(c, "working").Background(t.Accent).TextColor(t.AccentText)
					}
				})
				ui.SidebarItem(c, pageJobs, nil, "Jobs").Children(func() {
					if n := d.runningJobs(); n > 0 {
						ui.Badge(c, fmt.Sprint(n))
					}
				})
				ui.SidebarItem(c, pageAgents, nil, "Agents")
				ui.SidebarItem(c, pageMachines, nil, "Machines")
				ui.SidebarItem(c, pageSettings, nil, "Settings")
			}).Grow(1).Changed() {
				d.page, d.status = page, ""
			}
			d.connBadge(c)
		})
		ui.Column(c).Grow(1).Children(func() {
			if d.status != "" {
				ui.Row(c).Padding(8, 16).Background(t.Danger).Children(func() {
					ui.Text(c, d.status).TextColor(t.AccentText).Grow(1).Selectable()
					if ui.Button(c, "Dismiss").Clicked() {
						d.status = ""
					}
				})
			}
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
		})
	})
}

// connBadge shows the state of the server stream.
func (d *desktop) connBadge(c *ui.Context) {
	t := c.Theme()
	color := t.Danger
	switch d.conn.State {
	case "connected":
		color = t.Success
	case "connecting":
		color = t.Warning
	}
	ui.Column(c).Gap(2).Padding(0, 6).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Width(8).Height(8).Radius(4).Background(color)
			ui.Text(c, d.conn.State).FontSize(12)
		})
		if d.conn.Server != "" {
			ui.Text(c, d.conn.Server).FontSize(11).TextColor(t.TextMuted).SingleLine()
		}
		if d.conn.Error != "" && d.conn.State != "connected" {
			ui.Text(c, d.conn.Error).FontSize(11).TextColor(t.TextMuted).MaxLines(3)
		}
	})
}

func (d *desktop) runningJobs() int {
	n := 0
	for _, j := range d.jobs {
		if j.State == "queued" || j.State == "running" {
			n++
		}
	}
	return n
}

// header is the title bar of a page.
func header(c *ui.Context, title, sub string, actions func()) {
	t := c.Theme()
	ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(14, 20).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, title).Bold().FontSize(17)
			if sub != "" {
				ui.Text(c, sub).FontSize(12).TextColor(t.TextMuted)
			}
		})
		if actions != nil {
			actions()
		}
	})
}

// stateBadge is a colored pill for a job state.
func stateBadge(c *ui.Context, state string) {
	t := c.Theme()
	b := ui.Badge(c, state)
	switch state {
	case "done":
		b.Background(t.Success).TextColor(t.AccentText)
	case "failed":
		b.Background(t.Danger).TextColor(t.AccentText)
	case "running":
		b.Background(t.Accent).TextColor(t.AccentText)
	}
}
