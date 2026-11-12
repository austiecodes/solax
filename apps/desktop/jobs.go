package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// openJob shows a job on the jobs page and loads its events.
func (d *desktop) openJob(id string) {
	d.page, d.jobSel, d.followDraft = pageJobs, id, ""
	d.loadEvents(id)
}

// jobsPage lists jobs on the left and shows the chosen one on the right.
func (d *desktop) jobsPage(c *ui.Context) {
	t := c.Theme()
	header(c, "Jobs", fmt.Sprintf("%d jobs, %d running", len(d.jobs), d.runningJobs()), nil)
	ui.Row(c).Grow(1).Children(func() {
		ui.Scroll(c).Width(330).Padding(8).Gap(4).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Children(func() {
			if len(d.jobs) == 0 {
				ui.Text(c, "No jobs yet. The Super Agent creates them, or run: solax job send <agent> \"…\"").TextColor(t.TextMuted).Padding(8)
			}
			for _, j := range d.jobs {
				d.jobRow(c, j)
			}
		})
		ui.Column(c).Grow(1).Children(func() {
			j, ok := d.job(d.jobSel)
			if !ok {
				ui.Text(c, "Choose a job.").TextColor(t.TextMuted).Margin(ui.Auto)
				return
			}
			d.jobDetail(c, j)
		})
	})
}

func (d *desktop) jobRow(c *ui.Context, j protocol.Job) {
	t := c.Theme()
	row := ui.Column(c.Key(j.ID)).Gap(4).Padding(8, 10).Radius(8)
	if j.ID == d.jobSel {
		row.Background(t.Surface)
	} else if row.Hovered() {
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, j.Agent).Bold().Grow(1)
			stateBadge(c, j.State)
		})
		ui.Text(c, j.Title).FontSize(12).MaxLines(2)
		ui.Text(c, ago(j.CreatedAt)+" · by "+j.RequestedBy).FontSize(11).TextColor(t.TextMuted)
	})
	if row.Clicked() {
		d.openJob(j.ID)
	}
}

func (d *desktop) jobDetail(c *ui.Context, j protocol.Job) {
	t := c.Theme()
	ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(12, 20).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		ui.Text(c, j.Agent).Bold().FontSize(16)
		stateBadge(c, j.State)
		ui.Text(c, j.ID).FontSize(11).TextColor(t.TextMuted).Selectable().Grow(1)
		if j.State == "queued" || j.State == "running" {
			if ui.Button(c, "Cancel").Clicked() {
				id := j.ID
				d.do(func(ctx context.Context, c *client.Client) (func(), error) {
					nj, err := c.CancelJob(ctx, id)
					return func() { d.upsertJob(nj) }, err
				})
			}
		}
	})
	ui.Scroll(c).Grow(1).Padding(16, 20).Gap(12).Children(func() {
		section(c, "Prompt", func() {
			ui.Text(c, j.Prompt).Selectable()
		})
		if j.Error != "" {
			ui.Text(c, j.Error).TextColor(t.Danger).Selectable()
		}
		if j.Result != "" && (j.State == "done" || j.State == "failed") {
			section(c, "Result", func() {
				ui.Column(c).Selectable().Children(func() { markdown.View(c, j.Result, &d.md) })
			})
		}
		ui.Text(c, "Activity").Bold().Margin(8, 0, 0, 0)
		evs := d.events[j.SessionID]
		if len(evs) == 0 {
			ui.Text(c, "No events yet.").TextColor(t.TextMuted)
		}
		for _, e := range evs {
			d.eventRow(c, e)
		}
		for id, text := range d.live[j.SessionID] {
			ui.Column(c.Key("live-"+id)).Padding(8).Radius(8).Border(1, t.Border).Children(func() {
				markdown.Stream(c, text, &d.md)
			})
		}
	})
	ui.Row(c).Gap(8).Padding(12, 20).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		in := ui.TextInput(c, &d.followDraft).Placeholder("Send more input to this agent (follow-up)").Grow(1)
		send := ui.Button(c, "Send").Disabled(strings.TrimSpace(d.followDraft) == "" || j.State == "cancelled").Clicked()
		if (send || in.Submitted()) && strings.TrimSpace(d.followDraft) != "" {
			id, text := j.ID, strings.TrimSpace(d.followDraft)
			d.followDraft = ""
			d.do(func(ctx context.Context, c *client.Client) (func(), error) {
				nj, err := c.FollowUp(ctx, id, text)
				return func() { d.upsertJob(nj) }, err
			})
		}
	})
}

// eventRow shows one IR event as one compact block. The same code shows
// every driver: that is the point of the IR.
func (d *desktop) eventRow(c *ui.Context, e ir.Event) {
	t := c.Theme()
	key := c.Key(fmt.Sprint("ev-", e.Seq))
	muted := func(s string) { ui.Text(key, s).FontSize(12).TextColor(t.TextMuted) }
	switch e.Type {
	case ir.SessionStarted:
		muted(fmt.Sprintf("session started · %s %s", e.Session.Ref.Kind, e.Session.Model))
	case ir.SessionExitedEvent:
		muted("session ended " + e.Session.Reason)
	case ir.TurnStarted:
		muted("── turn started")
	case ir.TurnCompleted:
		s := "── turn " + string(e.Turn.Status)
		if e.Turn.Error != nil {
			s += ": " + e.Turn.Error.Message
		}
		if u := e.Turn.Usage; u != nil {
			s += fmt.Sprintf(" · %d in / %d out", u.Input+u.CacheRead, u.Output)
			if u.CostUSD > 0 {
				s += fmt.Sprintf(" · $%.4f", u.CostUSD)
			}
		}
		muted(s)
	case ir.NoticeEmitted:
		ui.Text(key, fmt.Sprintf("[%s] %s", e.Notice.Level, e.Notice.Message)).FontSize(12).TextColor(t.Warning)
	case ir.ItemCompleted:
		d.itemRow(key, e.Item)
	case ir.ItemStarted:
		if e.Item.Kind == ir.ItemToolCall {
			ui.Row(key).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, toolSummary(e.Item.Tool)).Font("monospace").FontSize(12).SingleLine()
			})
		}
	}
}

func (d *desktop) itemRow(c *ui.Context, it *ir.Item) {
	t := c.Theme()
	switch it.Kind {
	case ir.ItemUserMessage:
		ui.Column(c).Padding(8, 12).Radius(10).Background(t.Surface).Children(func() {
			ui.Text(c, "input").FontSize(11).TextColor(t.TextMuted)
			ui.Text(c, it.Text).Selectable().MaxLines(12)
		})
	case ir.ItemAgentMessage:
		ui.Column(c).Selectable().Children(func() { markdown.View(c, it.Text, &d.md) })
	case ir.ItemReasoning:
		if strings.TrimSpace(it.Text) != "" {
			ui.Text(c, it.Text).FontSize(12).Italic().TextColor(t.TextMuted).MaxLines(4)
		}
	case ir.ItemToolCall:
		mark, color := "✓", t.Success
		if it.Status == ir.StatusFailed || it.Status == ir.StatusDeclined {
			mark, color = "✗", t.Danger
		} else if it.Status == ir.StatusInterrupted {
			mark, color = "■", t.Warning
		}
		ui.Column(c).Gap(4).Padding(8, 10).Radius(8).Border(1, t.Border).Children(func() {
			ui.Row(c).Gap(6).Children(func() {
				ui.Text(c, mark).TextColor(color)
				ui.Text(c, toolSummary(it.Tool)).Font("monospace").FontSize(12).Grow(1).Selectable()
				ui.Text(c, string(it.Tool.Class)).FontSize(11).TextColor(t.TextMuted)
			})
			for _, f := range it.Tool.Files {
				ui.Text(c, f.Op+" "+f.Path).Font("monospace").FontSize(11).TextColor(t.TextMuted)
			}
			if out := strings.TrimSpace(it.Tool.Output); out != "" {
				ui.Text(c, out).Font("monospace").FontSize(11).TextColor(t.TextMuted).MaxLines(8).Selectable()
			}
		})
	case ir.ItemCompaction:
		ui.Text(c, "context compacted").FontSize(12).TextColor(t.TextMuted)
	}
}

// section is a titled block.
func section(c *ui.Context, title string, body func()) {
	t := c.Theme()
	ui.Column(c).Gap(6).Padding(10, 12).Radius(10).Background(t.Surface).Children(func() {
		ui.Text(c, title).FontSize(12).Bold().TextColor(t.TextMuted)
		body()
	})
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
