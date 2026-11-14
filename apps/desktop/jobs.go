package main

import (
	"context"
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/protocol"
)

// openJob shows a job on the jobs page, marks it read and loads its
// events.
func (d *desktop) openJob(id string) {
	d.page, d.jobSel, d.followDraft, d.status = pageJobs, id, "", ""
	d.markRead(id)
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
	d.header(c, "Jobs", plural(len(d.jobs), "job")+fmt.Sprintf(", %d running", live), nil)
	ui.Scroll(c).Grow(1).Padding(12, 16).Gap(4).Children(func() {
		if len(d.jobs) == 0 {
			emptyState(c, pal, jobsIcon, "No jobs yet",
				"The Super Agent gives jobs to your agents. You can also send one from a shell:",
				`solax job send coder "add a README"`)
			return
		}
		for _, j := range d.jobs {
			d.jobListRow(c, pal, j)
		}
	})
}

// jobListRow is a job in the list of all jobs, on one line.
func (d *desktop) jobListRow(c *ui.Context, pal *palette, j protocol.Job) {
	row := ui.ButtonBase(c.Key(j.ID)).Height(40).Padding(0, 12).Gap(12).Radius(8).AlignItems(ui.Center).Label(j.Title)
	if row.Hovered() {
		row.Background(pal.rowHover)
	}
	row.Children(func() {
		statusIcon(c, pal, j.State, d.unread[j.ID], 15)
		t := ui.Text(c, j.Title).FontSize(13.5).TextColor(pal.text).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		if d.unread[j.ID] {
			t.FontWeight(650)
		}
		ui.Text(c, j.Agent).FontSize(12.5).TextColor(pal.textMuted).SingleLine().Shrink(0)
		ui.Text(c, ago(j.CreatedAt)).FontSize(12).TextColor(pal.textFaint).FontFeatures("tnum").SingleLine().Width(90).Shrink(0)
	})
	if row.Clicked() {
		d.openJob(j.ID)
	}
}

func (d *desktop) jobDetail(c *ui.Context, j protocol.Job) {
	pal := paletteFor(c)
	d.header(c, j.Title, j.Agent+" · "+j.State+" · "+j.ID, func() {
		if jobLive(j.State) && button(c, "cancel", "Cancel job", plainButton, false) {
			id := j.ID
			d.ask("Cancel this job?", fmt.Sprintf("%s stops work on %q. You cannot resume it.", j.Agent, j.Title), "Cancel job", true, func() {
				d.do(func(ctx context.Context, c *client.Client) (func(), error) {
					nj, err := c.CancelJob(ctx, id)
					return func() { d.upsertJob(nj) }, err
				})
			}).keep = "Keep job"
		}
	})
	blocks := jobBlocks(j, d.events[j.SessionID], d.live[j.SessionID])
	d.transcriptView(c, pal, &d.jobList, blocks, nil)
	closed := j.State == "cancelled"
	placeholder := "Send more input to " + j.Agent
	if closed {
		placeholder = "The job was cancelled"
	}
	composer{
		key: "follow-up", placeholder: placeholder, draft: &d.followDraft, disabled: closed,
		info: func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Children(func() {
				ui.Icon(c, agentIcon).Size(12, 12).TextColor(pal.textMuted)
				ui.Text(c, j.Agent).FontSize(12).TextColor(pal.textMuted).SingleLine()
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

// plural writes a count with its noun: "1 job", "2 jobs".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func ago(at time.Time) string {
	d := time.Since(at)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	}
	return at.Local().Format("Jan 2 15:04")
}
