package main

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/locales"
	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/protocol"
)

// openJob shows a job on the jobs page, marks it read and loads its
// events.
func (d *desktop) openJob(id string) {
	d.page, d.jobSel, d.followDraft, d.status = pageJobs, id, "", ""
	d.markRead(id)
	d.openGroupOf(id)
	d.loadEvents(id)
}

// jobsPage shows the open job, or the list of all jobs when none is
// open. The sidebar lists the recent jobs too, so the job takes the
// whole card.
func (d *desktop) jobsPage(c *ui.Context) {
	if j, ok := d.job(d.jobSel); ok {
		d.jobDetail(c, j)
		return
	}
	pal := paletteFor(c)
	live := d.countJobs(func(j protocol.Job) bool { return jobLive(j.State) })
	spec := pageSpec{icon: jobsIcon, title: locales.NavJobs()}
	if len(d.jobs) > 0 {
		spec.sub = locales.JobsCount(len(d.jobs), live)
	}
	if len(d.jobs) == 0 {
		d.header(c, spec)
		emptyState(c, pal, emptySpec{
			icon: jobsIcon, title: locales.JobsEmptyTitle(),
			detail:  locales.JobsEmptyDetail(),
			command: `solax job send coder "add a README"`,
			action:  locales.ChatPlaceholder(), run: func() { d.goTo(pageChat) },
		})
		return
	}
	d.showPage(c, spec, func() {
		section(c, "jobs", sectionSpec{title: locales.AllJobs(), count: countText(len(d.jobs)), rows: listRows}, func() {
			for _, j := range d.jobs {
				d.jobListRow(c, pal, j)
			}
		})
	})
}

// jobListRow is a job in the list of all jobs, on one line.
func (d *desktop) jobListRow(c *ui.Context, pal *palette, j protocol.Job) {
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
		d.openJob(j.ID)
	}
}

func (d *desktop) jobDetail(c *ui.Context, j protocol.Job) {
	pal := paletteFor(c)
	d.header(c, pageSpec{title: j.Title, sub: j.Agent + " · " + stateWord(j.State), actions: func() {
		if jobLive(j.State) && button(c, "cancel", locales.JobCancel(), plainButton, false) {
			id := j.ID
			d.ask(locales.JobCancelQuestion(), locales.JobCancelDetail(j.Agent, j.Title), locales.JobCancel(), true, func() {
				d.do(func(ctx context.Context, c *client.Client) (func(), error) {
					nj, err := c.CancelJob(ctx, id)
					return func() { d.upsertJob(nj) }, err
				})
			}).keep = locales.JobKeep()
		}
	}})
	blocks := withWorking(jobBlocks(j, d.events[j.SessionID], d.live[j.SessionID]), jobLive(j.State))
	d.transcriptView(c, pal, &d.jobList, blocks, nil)
	closed := j.State == "cancelled"
	placeholder := locales.JobFollowUp(j.Agent)
	if closed {
		placeholder = locales.JobWasCancelled()
	}
	composer{
		key: "follow-up", placeholder: placeholder, draft: &d.followDraft, disabled: closed,
		info: func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Children(func() {
				ui.Icon(c, agentIcon).Size(13, 13).TextColor(pal.textMuted)
				ui.Text(c, j.Agent).FontSize(12.5).TextColor(pal.textMuted).SingleLine()
			})
		},
		send: func(text string) {
			id := j.ID
			d.followDraft = ""
			d.do(func(ctx context.Context, c *client.Client) (func(), error) {
				nj, err := c.FollowUp(ctx, id, text)
				return func() { d.upsertJob(nj) }, err
			})
		},
	}.view(c, pal)
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
