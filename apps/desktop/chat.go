package main

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/protocol"
)

// chatPage is the conversation with the Super Agent: its messages, with
// what it did in between folded into groups, and the composer.
func (d *desktop) chatPage(c *ui.Context) {
	pal := paletteFor(c)
	sa := d.superAgent()
	sub := ""
	switch {
	case sa == nil:
		sub = "No Super Agent yet"
	case d.superBusy:
		sub = "Working…"
	case len(d.messages) > 0:
		sub = plural(len(d.messages), "message")
	}
	d.header(c, "Super Agent", sub, nil)

	blocks := chatBlocks(d.messages, d.events[d.superSes], d.live[d.superSes], d.superBusy)
	d.transcriptView(c, pal, &d.chatList, blocks, func() { d.chatEmpty(c, pal, sa != nil) })

	composer{
		key: "composer", placeholder: "Message the Super Agent", draft: &d.draft,
		disabled: sa == nil, busy: d.sending, focus: true, send: d.send,
		info: func() {
			if sa == nil {
				return
			}
			ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Children(func() {
				ui.Icon(c, sparklesIcon).Size(12, 12).TextColor(pal.textMuted)
				ui.Text(c, sa.Name+" on "+sa.Machine).FontSize(12).TextColor(pal.textMuted).SingleLine().Ellipsis("…").MinWidth(0)
			})
		},
	}.view(c, pal)
}

// suggestions start an empty chat.
var suggestions = []string{
	"Ask coder to add a README to the repo",
	"What are my agents doing?",
	"Which jobs failed today?",
}

// chatEmpty fills the chat before the first message.
func (d *desktop) chatEmpty(c *ui.Context, pal *palette, ready bool) {
	if !ready {
		emptyState(c, pal, sparklesIcon, "No Super Agent yet",
			"The Super Agent takes your requests and gives jobs to your agents. Add one on the Agents page, and check \"Super Agent\".", "")
		return
	}
	ui.Column(c.Key("empty")).Fill().Center().Padding(24).Gap(8).Children(func() {
		ui.Box(c).Size(52, 52).Radius(14).Center().Background(pal.chip).TextColor(pal.textMuted).Margin(0, 0, 10, 0).Children(func() {
			ui.Icon(c, sparklesIcon).Size(26, 26)
		})
		ui.Text(c, "What should your agents do?").FontSize(19).FontWeight(650).TextColor(pal.text)
		ui.Text(c, "The Super Agent gives jobs to your agents and tells you when they are done.").
			FontSize(13.5).TextColor(pal.textMuted).MaxWidth(420).TextAlign(ui.Center).LineHeight(1.45)
		ui.Row(c).Gap(8).Wrap().Justify(ui.Center).MaxWidth(560).Margin(14, 0, 0, 0).Children(func() {
			for i, s := range suggestions {
				b := ui.ButtonBase(c.Key(i)).Padding(6, 12).Radius(15).Label(s).Background(pal.chip).TextColor(pal.textMuted)
				if b.Hovered() {
					b.Background(pal.rowHover).TextColor(pal.text)
				}
				b.Children(func() { ui.Text(c, s).FontSize(12.5).SingleLine() })
				if b.Clicked() {
					d.draft = s
				}
			}
		})
	})
}

// send posts text to the Super Agent. The draft stays until the server
// has it; the reply comes on the stream.
func (d *desktop) send(text string) {
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
