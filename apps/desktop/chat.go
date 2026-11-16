package main

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/locales"
	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/protocol"
)

// chatPage is the conversation with the Super Agent: its messages, with
// what it did in between folded into groups, and the composer.
func (d *desktop) chatPage(c *ui.Context) {
	pal := paletteFor(c)
	sa := d.superAgent()
	busy := d.sending || d.superWaiting || d.superBusy
	sub := ""
	switch {
	case sa == nil:
		sub = locales.ChatNotSetUp()
	case busy:
		sub = locales.ChatWorking()
	case len(d.messages) > 0:
		sub = locales.ChatMessages(len(d.messages))
	}
	d.header(c, pageSpec{title: locales.NavChat(), sub: sub})

	blocks := withWorking(chatBlocks(d.messages, d.events[d.superSes], d.live[d.superSes], d.superBusy), busy)
	d.transcriptView(c, pal, &d.chatList, blocks, func() { d.chatEmpty(c, pal, sa != nil) })

	composer{
		key: "composer", placeholder: locales.ChatPlaceholder(), draft: &d.draft,
		disabled: sa == nil, busy: d.sending, focus: true, send: d.send,
		info: func() {
			if sa == nil {
				return
			}
			ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Children(func() {
				ui.Icon(c, sparklesIcon).Size(13, 13).TextColor(pal.textMuted)
				ui.Text(c, locales.ChatAgentOn(sa.Name, sa.Machine)).FontSize(12.5).TextColor(pal.textMuted).SingleLine().Ellipsis("…").MinWidth(0)
			})
		},
	}.view(c, pal)
}

// suggestion is a prompt that starts an empty chat, in the language
// shown.
type suggestion struct {
	icon *ui.SVG
	text func() string
}

var suggestions = []suggestion{
	{pencilIcon, locales.ChatSuggestionReadme},
	{agentIcon, locales.ChatSuggestionDoing},
	{jobsIcon, locales.ChatSuggestionFailed},
}

// chatEmpty fills the chat before the first message, as the Codex app's
// new thread: a large question in the middle, and the suggestions in a
// row of cards just over the composer.
func (d *desktop) chatEmpty(c *ui.Context, pal *palette, ready bool) {
	if !ready {
		emptyState(c, pal, emptySpec{
			icon: sparklesIcon, title: locales.ChatNoSolaxTitle(),
			detail: locales.ChatNoSolaxDetail(),
			action: locales.OpenAgents(), run: func() { d.goTo(pageAgents) },
		})
		return
	}
	ui.Column(c.Key("empty")).Fill().Padding(0, gutter, 14, gutter).Children(func() {
		ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
			ui.Icon(c, sparklesIcon).Size(28, 28).TextColor(pal.text).Margin(0, 0, 6, 0)
			ui.Text(c, locales.ChatEmptyTitle()).FontSize(26).FontWeight(600).TextColor(pal.text).TextAlign(ui.Center)
			ui.Text(c, locales.ChatEmptyDetail()).
				FontSize(14).TextColor(pal.textMuted).MaxWidth(440).TextAlign(ui.Center).LineHeight(1.45)
		})
		ui.Row(c).Justify(ui.Center).Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).MaxWidth(readW).Gap(10).AlignItems(ui.Stretch).Children(func() {
				for i, s := range suggestions {
					d.suggestionCard(c, pal, i, s)
				}
			})
		})
	})
}

// suggestionCard is a prompt to start with. A click puts it in the
// composer, to change before it goes.
func (d *desktop) suggestionCard(c *ui.Context, pal *palette, i int, s suggestion) {
	text := s.text()
	b := ui.ButtonBase(c.Key(i)).Grow(1).Basis(0).MinWidth(0).Column().AlignItems(ui.Start).Justify(ui.Start).Padding(12, 14).Gap(8).Radius(14).Label(text).
		Border(1, pal.divider)
	if b.Hovered() {
		b.Background(pal.rowHover)
	}
	b.Children(func() {
		ui.Icon(c, s.icon).Size(14, 14).TextColor(pal.textMuted)
		ui.Text(c, text).FontSize(13).LineHeight(1.4).TextColor(pal.text).TextAlign(ui.Start).MaxLines(2).Ellipsis("…")
	})
	if b.Clicked() {
		d.draft = text
	}
}

// send posts text to the Super Agent. The draft stays until the server
// has it; the reply comes on the stream.
func (d *desktop) send(text string) {
	d.sending, d.superWaiting = true, true
	d.do(func(ctx context.Context, c *client.Client) (func(), error) {
		m, err := c.SendToSuper(ctx, text)
		if err != nil {
			d.update(func() { d.sending, d.superWaiting = false, false })
			return nil, err
		}
		return func() {
			d.sending = false
			d.draft = ""
			d.addMessage(m)
		}, nil
	})
	if _, err := d.api(); err != nil {
		d.sending, d.superWaiting = false, false
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
