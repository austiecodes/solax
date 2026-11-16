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
	spec := pageSpec{icon: jobsIcon, title: "Jobs"}
	if len(d.jobs) > 0 {
		spec.sub = plural(len(d.jobs), "job") + fmt.Sprintf(", %d running", live)
	}
	if len(d.jobs) == 0 {
		d.header(c, spec)
		emptyState(c, pal, emptySpec{
			icon: jobsIcon, title: "No jobs yet",
			detail:  "Ask Solax for work, and it gives jobs to your agents. You can also send a job from a shell:",
			command: `solax job send coder "add a README"`,
			action:  "Message Solax", run: func() { d.goTo(pageChat) },
		})
		return
	}
	d.showPage(c, spec, func() {
		section(c, "jobs", sectionSpec{title: "All jobs", count: countText(len(d.jobs)), rows: listRows}, func() {
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
	d.header(c, pageSpec{title: j.Title, sub: j.Agent + " · " + j.State, actions: func() {
		if jobLive(j.State) && button(c, "cancel", "Cancel job", plainButton, false) {
			id := j.ID
			d.ask("Cancel this job?", fmt.Sprintf("%s stops work on %q. You cannot resume it.", j.Agent, j.Title), "Cancel job", true, func() {
				d.do(func(ctx context.Context, c *client.Client) (func(), error) {
					nj, err := c.CancelJob(ctx, id)
					return func() { d.upsertJob(nj) }, err
				})
			}).keep = "Keep job"
		}
	}})
	blocks := withWorking(jobBlocks(j, d.events[j.SessionID], d.live[j.SessionID]), jobLive(j.State))
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

// since is ago in short, for a narrow column: "now", "5m", "3h", "2d".
func since(at time.Time) string {
	d := time.Since(at)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return at.Local().Format("Jan 2")
}
