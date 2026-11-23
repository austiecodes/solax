package main

import (
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/locales"
	"github.com/austiecodes/solax/pkg/protocol"
)

// The sidebar: the traffic lights and the hide button, the pages, the tasks
// with their state (as kero lists its sessions), and a footer with the
// counts that need the user and the connection.

func (d *desktop) sidebar(c *ui.Context, pal *palette, bar ui.TitleBar) {
	// The top bar clears the traffic lights and moves the window.
	// No app title: the first page is Solax, as in Alma.
	ui.Row(c).Height(topBar).Shrink(0).AlignItems(ui.Center).Gap(6).Padding(0, 10, 0, max(bar.Left, 16)).DragWindow().Children(func() {
		ui.Spacer(c)
		iconButton(c, "hide-sidebar", sidebarIcon, tip(locales.HideSidebar(), "sidebar.toggle"), pal, func() { d.run("sidebar.toggle") })
	})
	ui.Column(c.Key("nav")).Shrink(0).Padding(0, 8).Gap(1).Children(func() {
		for _, p := range navPages {
			d.navRow(c, pal, p)
		}
	})
	// The space around the rows moves the window, as the top bar does.
	list := ui.Scroll(c.Key("tasks")).Grow(1).Padding(0, 8, 12, 8).Gap(1).DragWindow()
	list.Children(func() {
		if len(d.tasks) == 0 {
			return
		}
		d.tasksHead(c, pal)
		for _, g := range d.taskGroups() {
			d.groupView(c, pal, g)
		}
	})
	d.sidebarFooter(c, pal)
	c.After(20 * time.Second) // the "ago" times move on
}

// navRow is a page in the sidebar: its icon, its title, and what it has
// to say (a count, or that it works).
func (d *desktop) navRow(c *ui.Context, pal *palette, p navPage) {
	label := title(p.action)
	selected := d.page == p.id && (p.id != pageTasks || d.taskSel == "") && (p.id != pageWorkflows || d.wf.sel == "")
	row := sideRow(c, pal, p.id, selected).Label(label).Tooltip(tip(label, p.action))
	fg := pal.textMuted
	if selected {
		fg = pal.text
	}
	row.Children(func() {
		ui.Icon(c, p.icon).Size(14, 14).TextColor(fg).Shrink(0)
		ui.Text(c, label).FontSize(13).TextColor(pal.text).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		switch p.id {
		case pageChat:
			if d.superBusy {
				statusIcon(c, pal, "running", false, 12)
			}
		case pageTasks:
			if n := d.countTasks(func(j protocol.Task) bool { return taskLive(j.State) }); n > 0 {
				ui.Textf(c, "%d", n).FontSize(12).TextColor(pal.textFaint).FontFeatures("tnum")
			}
		case pageWorkflows:
			d.workflowCount(c, pal)
		}
	})
	if row.Clicked() && !selected {
		d.run(p.action)
	}
}

// tasksHead is the head of the tasks: its label, and a menu that chooses
// how to group them, as the Codex app's "Organize" menu.
func (d *desktop) tasksHead(c *ui.Context, pal *palette) {
	ui.Row(c.Key("tasks-head")).Height(28).Margin(14, 0, 2, 0).Padding(0, 2, 0, 8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.RecentTasks()).FontSize(12).FontWeight(500).TextColor(pal.textFaint).Grow(1)
		b := ui.ButtonBase(c.Key("organize")).Size(24, 22).Radius(6).Center().TextColor(pal.textFaint).
			Label(locales.GroupTasks()).Tooltip(locales.GroupTasks())
		if b.Hovered() {
			b.Background(pal.rowHover).TextColor(pal.text)
		}
		b.Children(func() { ui.Icon(c, organizeIcon).Size(14, 14) })
		b.Menu(func(m *ui.Menu) {
			for _, g := range groupings {
				if m.Item(g.menu()).Radio(d.prefs.TaskGroups == g.id).Chosen() {
					d.prefs.TaskGroups = g.id
					d.savePrefs()
				}
			}
		})
	})
}

// taskGroups are the groups of tasks the sidebar shows.
func (d *desktop) taskGroups() []taskGroup { return groupTasks(d.tasks, d.agents, d.prefs.TaskGroups) }

// groupView is a group of tasks: a head that opens and closes it, as a
// project folder in the Codex app, then its tasks. A group with no title
// (no grouping) is its tasks only.
func (d *desktop) groupView(c *ui.Context, pal *palette, g taskGroup) {
	if g.title == "" {
		for _, j := range g.tasks {
			d.taskRow(c, pal, j, false)
		}
		return
	}
	closed := d.groupClosed(g.key)
	head := sideRow(c, pal, "group-"+g.key, false).Label(g.title)
	if g.tip != "" {
		head.Tooltip(g.tip)
	}
	head.Children(func() {
		ui.Icon(c, g.icon).Size(14, 14).TextColor(pal.textMuted).Shrink(0)
		ui.Text(c, g.title).FontSize(13).FontWeight(500).TextColor(pal.textMuted).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		if closed || head.Hovered() {
			ui.Icon(c, chevronIcon).Size(8, 8).TextColor(pal.textFaint).Rotate(90 * b2f(!closed)).Shrink(0)
		}
	})
	if head.Clicked() {
		d.setGroupClosed(g.key, !closed)
	}
	if closed {
		return
	}
	for _, j := range g.tasks {
		d.taskRow(c, pal, j, true)
	}
}

// groupClosed tells whether the user closed the group of key.
func (d *desktop) groupClosed(key string) bool { return slices.Contains(d.prefs.ClosedGroups, key) }

func (d *desktop) setGroupClosed(key string, closed bool) {
	d.prefs.ClosedGroups = slices.DeleteFunc(d.prefs.ClosedGroups, func(k string) bool { return k == key })
	if closed {
		d.prefs.ClosedGroups = append(d.prefs.ClosedGroups, key)
	}
	d.savePrefs()
}

// openGroupOf opens the group of a task, so the sidebar shows it.
func (d *desktop) openGroupOf(id string) {
	if j, ok := d.task(id); ok && d.prefs.TaskGroups != "none" {
		if key := groupOf(j, d.agents, d.prefs.TaskGroups).key; d.groupClosed(key) {
			d.setGroupClosed(key, false)
		}
	}
}

// taskRow is a task in the sidebar, on one line, as a thread in the Codex
// app: its title, then its age, or its state while that needs the user
// (it runs, it failed, or it ended unread). An unread task is bold. The
// tooltip names its agent and its prompt. In a group, its title lines up
// with the group's.
func (d *desktop) taskRow(c *ui.Context, pal *palette, j protocol.Task, inGroup bool) {
	selected := d.page == pageTasks && d.taskSel == j.ID
	unread := d.unread[j.ID]
	row := sideRow(c, pal, j.ID, selected).Label(j.Title).Tooltip(j.Agent + "\n" + clip(j.Prompt, 300))
	if inGroup {
		row.Padding(0, 8, 0, 30)
	}
	row.Children(func() {
		name := ui.Text(c, j.Title).FontSize(13).TextColor(pal.text).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		if unread {
			name.FontWeight(600)
		}
		if taskLive(j.State) || j.State == "failed" || unread {
			ui.Box(c).Width(24).Shrink(0).AlignItems(ui.End).Children(func() { statusIcon(c, pal, j.State, unread, 12) })
		} else {
			ui.Text(c, since(j.CreatedAt)).FontSize(12).TextColor(pal.textFaint).FontFeatures("tnum").SingleLine().Shrink(0)
		}
	})
	if row.Clicked() && !selected {
		d.openTask(j.ID)
	}
}

// sideRow is a row of the sidebar: flat, lit when hovered, filled when
// selected.
func sideRow(c *ui.Context, pal *palette, key string, selected bool) ui.Element {
	row := ui.ButtonBase(c.Key(key)).Height(rowH).Padding(0, 8).Gap(8).AlignItems(ui.Center).Radius(7).Shrink(0)
	bg := ui.Transparent
	switch {
	case selected:
		bg = pal.rowSelected
	case row.Hovered():
		bg = pal.rowHover
	}
	return row.Background(row.AnimateColor("bg", bg, ui.Animation{Duration: 110 * time.Millisecond}))
}

// sidebarFooter has the reload and settings buttons, then what needs the
// user: counts of running, failed and finished tasks, each a jump to the
// next of them. Without a connection it says so instead.
func (d *desktop) sidebarFooter(c *ui.Context, pal *palette) {
	ui.Row(c).Height(44).Shrink(0).Padding(0, 8, 4, 8).Gap(2).AlignItems(ui.Center).DragWindow().Children(func() {
		gear := iconButton(c, "settings", gearIcon, tip(locales.Settings(), "settings.open"), pal, func() { d.run("settings.open") })
		if d.page == pageSettings {
			gear.Background(pal.rowSelected).TextColor(pal.text)
		}
		iconButton(c, "palette", searchIcon, tip(locales.GoTo(), "palette.open"), pal, func() { d.run("palette.open") })
		iconButton(c, "reload", reloadIcon, tip(locales.ActionReload(), "data.reload"), pal, func() { d.run("data.reload") })
		ui.Spacer(c)
		if d.conn.State != "connected" {
			d.connNote(c, pal)
			return
		}
		d.footerChip(c, pal, "running", false, locales.FooterLive, func(j protocol.Task) bool { return taskLive(j.State) })
		d.footerChip(c, pal, "failed", true, locales.FooterFailed, func(j protocol.Task) bool { return j.State == "failed" && d.unread[j.ID] })
		d.footerChip(c, pal, "done", true, locales.FooterDone, func(j protocol.Task) bool { return j.State == "done" && d.unread[j.ID] })
	})
}

// footerChip counts the tasks that match, with their state icon. A click
// opens the next of them. It shows nothing while none match.
func (d *desktop) footerChip(c *ui.Context, pal *palette, state string, unread bool, say func(int) string, is func(protocol.Task) bool) {
	n := d.countTasks(is)
	if n == 0 {
		return
	}
	words := say(n)
	b := ui.ButtonBase(c.Key("chip-"+state)).Height(24).Padding(0, 8, 0, 6).Gap(5).Radius(12).AlignItems(ui.Center).
		Label(words).Tooltip(locales.FooterOpenNext(words))
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
		ui.Text(c, stateWord(d.conn.State)).FontSize(12).TextColor(pal.textMuted).SingleLine().Ellipsis("…").MinWidth(0)
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

func (d *desktop) countTasks(is func(protocol.Task) bool) int {
	n := 0
	for _, j := range d.tasks {
		if is(j) {
			n++
		}
	}
	return n
}

// jumpTo opens the next task that matches, after the one open, in sidebar
// order, and wraps around.
func (d *desktop) jumpTo(is func(protocol.Task) bool) {
	tasks := sidebarTasks(d.taskGroups())
	start := 0
	for i, j := range tasks {
		if j.ID == d.taskSel {
			start = i + 1
		}
	}
	for k := range tasks {
		j := tasks[(start+k)%len(tasks)]
		if is(j) {
			d.openTask(j.ID)
			return
		}
	}
}
