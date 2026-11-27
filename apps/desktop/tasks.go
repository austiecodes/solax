package main

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/locales"
	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/protocol"
)

// openTask shows a task on the tasks page, marks it read and loads its
// events.
func (d *desktop) openTask(id string) {
	d.page, d.taskSel, d.followDraft, d.status = pageTasks, id, "", ""
	d.markRead(id)
	d.openGroupOf(id)
	d.loadEvents(id)
}

// tasksPage shows the open task, or the list of all tasks when none is
// open. The sidebar lists the recent tasks too, so the task takes the
// whole card.
func (d *desktop) tasksPage(c *ui.Context) {
	if j, ok := d.task(d.taskSel); ok {
		d.taskDetail(c, j)
		return
	}
	pal := paletteFor(c)
	live := d.countTasks(func(j protocol.Task) bool { return taskLive(j.State) })
	spec := pageSpec{icon: tasksIcon, title: locales.NavTasks()}
	if len(d.tasks) > 0 {
		spec.sub = locales.TasksCount(len(d.tasks), live)
	}
	if len(d.tasks) == 0 {
		d.header(c, spec)
		emptyState(c, pal, emptySpec{
			icon: tasksIcon, title: locales.TasksEmptyTitle(),
			detail:  locales.TasksEmptyDetail(),
			command: `solax task send coder "add a README"`,
			action:  locales.ChatPlaceholder(), run: func() { d.goTo(pageChat) },
		})
		return
	}
	d.showPage(c, spec, func() {
		section(c, "tasks", sectionSpec{title: locales.AllTasks(), count: countText(len(d.tasks)), rows: listRows}, func() {
			for _, j := range d.tasks {
				d.taskListRow(c, pal, j)
			}
		})
	})
}

// taskListRow is a task in the list of all tasks, on one line.
func (d *desktop) taskListRow(c *ui.Context, pal *palette, j protocol.Task) {
	row := listItem(c, j.ID, false, j.Title, func() {
		statusIcon(c, pal, j.State, d.unread[j.ID], 15)
		t := ui.Text(c, j.Title).FontSize(13.5).TextColor(pal.text).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		if d.unread[j.ID] {
			t.FontWeight(650)
		}
		chip(c, j.Agent, markQuiet)
		ui.Text(c, ago(j.CreatedAt)).FontSize(12).TextColor(pal.textFaint).FontFeatures("tnum").SingleLine().TextAlign(ui.End).Width(80).Shrink(0)
	})
	if row.Clicked() {
		d.openTask(j.ID)
	}
}

func (d *desktop) taskDetail(c *ui.Context, j protocol.Task) {
	pal := paletteFor(c)
	d.header(c, pageSpec{title: j.Title, sub: j.Agent + " · " + stateWord(j.State), actions: func() {
		if taskLive(j.State) && button(c, "cancel", locales.TaskCancel(), plainButton, false) {
			id := j.ID
			d.ask(locales.TaskCancelQuestion(), locales.TaskCancelDetail(j.Agent, j.Title), locales.TaskCancel(), true, func() {
				d.do(func(ctx context.Context, c *client.Client) (func(), error) {
					nj, err := c.CancelTask(ctx, id)
					return func() { d.upsertTask(nj) }, err
				})
			}).keep = locales.TaskKeep()
		}
	}})
	blocks := withWorking(taskBlocks(j, d.events[j.SessionID], d.live[j.SessionID]), taskLive(j.State))
	d.transcriptView(c, pal, &d.taskList, blocks, nil)
	closed := j.State == "cancelled"
	// While a turn runs, input steers it; otherwise it starts a new turn.
	steer := j.State == "running"
	placeholder := locales.TaskFollowUp(j.Agent)
	switch {
	case closed:
		placeholder = locales.TaskWasCancelled()
	case steer:
		placeholder = locales.TaskSteer(j.Agent)
	}
	cm := composer{
		key: "follow-up", placeholder: placeholder, draft: &d.followDraft, disabled: closed,
		info: func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Children(func() {
				d.taskPlace(c, pal, j)
			})
		},
		send: func(text string) {
			id := j.ID
			d.followDraft = ""
			d.do(func(ctx context.Context, c *client.Client) (func(), error) {
				send := c.FollowUp
				if steer {
					send = c.Steer
				}
				nj, err := send(ctx, id, text)
				return func() { d.upsertTask(nj) }, err
			})
		},
	}
	if steer {
		id := j.ID
		cm.stop = func() { d.interruptTask(id) }
	}
	cm.view(c, pal)
}

// interruptTask stops the running turn of a task. The task fails as
// interrupted, on the stream; more input starts it again.
func (d *desktop) interruptTask(id string) {
	if d.stopping {
		return
	}
	d.stopping = true
	d.do(func(ctx context.Context, c *client.Client) (func(), error) {
		nj, err := c.InterruptTask(ctx, id)
		if err != nil {
			d.update(func() { d.stopping = false })
			return nil, err
		}
		return func() {
			d.stopping = false
			d.upsertTask(nj)
		}, nil
	})
	if _, err := d.api(); err != nil {
		d.stopping = false
	}
}

// ago says how long ago at was: "just now", "5 min ago", "2 h ago", then
// the day and time.
func ago(at time.Time) string {
	d := time.Since(at)
	switch {
	case d < time.Minute:
		return locales.AgoNow()
	case d < time.Hour:
		return locales.AgoMinutes(int(d.Minutes()))
	case d < 24*time.Hour:
		return locales.AgoHours(int(d.Hours()))
	}
	return at.Local().Format(locales.AgoLayout())
}

// since is ago in short, for a narrow column: "now", "5m", "3h", "2d".
func since(at time.Time) string {
	d := time.Since(at)
	switch {
	case d < time.Minute:
		return locales.SinceNow()
	case d < time.Hour:
		return locales.SinceMinutes(int(d.Minutes()))
	case d < 24*time.Hour:
		return locales.SinceHours(int(d.Hours()))
	case d < 7*24*time.Hour:
		return locales.SinceDays(int(d.Hours() / 24))
	}
	return at.Local().Format(locales.SinceLayout())
}

// taskPlace says where a task works: its agent, its project and the
// machine of its agent. A task without a project shows no project.
func (d *desktop) taskPlace(c *ui.Context, pal *palette, j protocol.Task) {
	part := func(icon *ui.SVG, text string) {
		ui.Icon(c, icon).Size(13, 13).TextColor(pal.textMuted).Shrink(0)
		ui.Text(c, text).FontSize(12.5).TextColor(pal.textMuted).SingleLine().Ellipsis("…").MinWidth(0)
	}
	part(agentIcon, j.Agent)
	if j.Project != "" {
		part(folderIcon, j.Project)
	}
	if a, ok := agentOf(j, d.agents); ok && a.Machine != "" {
		part(machineIcon, a.Machine)
	}
}
