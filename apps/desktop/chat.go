package main

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/protocol"
)

// chatPage is the conversation with the Super Agent.
func (d *desktop) chatPage(c *ui.Context) {
	t := c.Theme()
	sub := "Talk to the router. It gives jobs to your agents and reports back."
	if d.superAgent() == nil {
		sub = "No Super Agent yet: add one on the Agents page (check \"Super Agent\")."
	}
	header(c, "Super Agent", sub, nil)
	d.activeJobs(c)

	d.chatList.Key = func(i int) any { return d.messages[i].ID }
	d.chatList.FollowEnd = true
	ui.List(c, &d.chatList, len(d.messages), func(i int) {
		d.message(c, d.messages[i])
	}).Grow(1).Padding(16, 24).Gap(14).Justify(ui.End).Children(func() {
		if len(d.messages) == 0 {
			ui.Text(c, "Ask for something, for example: \"Ask coder to add a README to the repo\".").TextColor(t.TextMuted).Margin(ui.Auto)
		}
	})

	if d.superBusy {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(6, 24).Children(func() {
			ui.Spinner(c)
			ui.Text(c, d.superNote).FontSize(12).TextColor(t.TextMuted).SingleLine()
		})
	}
	ui.Row(c).Gap(8).Padding(12, 20).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		in := ui.TextInput(c, &d.draft).Placeholder("Message the Super Agent").Grow(1).AutoFocus()
		send := ui.PrimaryButton(c, "Send").Disabled(d.sending || strings.TrimSpace(d.draft) == "").Clicked()
		if (send || in.Submitted()) && !d.sending && strings.TrimSpace(d.draft) != "" {
			d.send()
		}
	})
}

// send posts the draft. The reply comes on the stream.
func (d *desktop) send() {
	text := strings.TrimSpace(d.draft)
	d.sending = true
	d.do(func(ctx context.Context, c *client.Client) (func(), error) {
		m, err := c.SendToSuper(ctx, text)
		if err != nil {
			d.update(func() { d.sending = false })
			return nil, err
		}
		return func() {
			d.sending = false
			d.draft = ""
			d.addMessage(m)
		}, nil
	})
	if _, err := d.api(); err != nil {
		d.sending = false
	}
}

func (d *desktop) message(c *ui.Context, m protocol.Message) {
	t := c.Theme()
	switch m.Author.Kind {
	case "human":
		ui.Row(c).Children(func() {
			ui.Spacer(c)
			ui.Text(c, m.Text).Selectable().Padding(8, 12).Radius(14).Background(t.Surface).MaxWidth(560)
		})
	case "system":
		// A job note: "[job job_x] agent coder: done". A click opens the job.
		jobID := jobOf(m.Text)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "•").TextColor(t.TextMuted)
			ui.Text(c, strings.TrimPrefix(m.Text, "[job "+jobID+"] ")).FontSize(12).TextColor(t.TextMuted)
			if jobID != "" && ui.Button(c, "Open job").Clicked() {
				d.openJob(jobID)
			}
		})
	default:
		ui.Column(c).MaxWidth(780).Selectable().Children(func() {
			markdown.View(c, m.Text, &d.md)
		})
	}
}

// activeJobs is a strip of the jobs that run now.
func (d *desktop) activeJobs(c *ui.Context) {
	if d.runningJobs() == 0 {
		return
	}
	t := c.Theme()
	ui.Row(c).Gap(8).Padding(8, 20).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		ui.Text(c, "Running:").FontSize(12).TextColor(t.TextMuted)
		for _, j := range d.jobs {
			if j.State != "queued" && j.State != "running" {
				continue
			}
			if ui.Button(c.Key(j.ID), j.Agent+" · "+clip(j.Title, 32)).Clicked() {
				d.openJob(j.ID)
			}
		}
	})
}

func (d *desktop) superAgent() *protocol.Agent {
	for i := range d.agents {
		if d.agents[i].Role == "super" {
			return &d.agents[i]
		}
	}
	return nil
}

// jobOf returns the job ID of a "[job <id>] ..." note.
func jobOf(text string) string {
	rest, ok := strings.CutPrefix(text, "[job ")
	if !ok {
		return ""
	}
	id, _, ok := strings.Cut(rest, "]")
	if !ok {
		return ""
	}
	return id
}
