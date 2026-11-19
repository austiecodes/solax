package main

import (
	"slices"
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/internal/actions"
	"github.com/austiecodes/solax/apps/desktop/locales"
	"github.com/austiecodes/solax/pkg/protocol"
)

// The command palette (⌘K), in the look of the Codex app's: one field
// that finds tasks, agents and actions as one types, and a footer with
// its keys. Each kind of item comes from one
// source function; a new kind is one more source in paletteItems.

type paletteState struct {
	open   bool
	query  string
	index  int  // the highlighted item
	reveal bool // the highlighted item scrolls into view, once
}

// paletteItem is one thing the palette offers.
type paletteItem struct {
	group  string // the heading it shows under before the user types
	title  string
	detail string
	keys   string  // the shortcut, as the menus show it
	icon   *ui.SVG // nil with a task, which shows its state
	task   *protocol.Task
	run    func()
	score  int
}

// recentTasks is how many tasks the palette shows before the user types.
const recentTasks = 6

func (d *desktop) openPalette() {
	d.pal = paletteState{open: true, reveal: true}
}

// paletteItems are all the items, in the order they show unfiltered.
func (d *desktop) paletteItems() []paletteItem {
	return slices.Concat(d.taskItems(), d.actionItems(), d.agentItems())
}

func (d *desktop) taskItems() []paletteItem {
	var out []paletteItem
	for i := range d.tasks {
		j := &d.tasks[i]
		id := j.ID
		out = append(out, paletteItem{group: locales.NavTasks(), title: j.Title, detail: j.Agent + " · " + stateWord(j.State) + " · " + ago(j.CreatedAt),
			task: j, run: func() { d.openTask(id) }})
	}
	return out
}

func (d *desktop) actionItems() []paletteItem {
	var out []paletteItem
	for _, a := range actions.All {
		if a.ID == "palette.open" {
			continue
		}
		id := a.ID
		icon := returnIcon
		for _, p := range navPages {
			if p.action == id {
				icon = p.icon
			}
		}
		out = append(out, paletteItem{group: locales.PaletteActions(), title: strings.TrimSuffix(title(id), "…"), detail: menuTitle(a.Menu),
			keys: actions.Glyphs(a.Keys), icon: icon, run: func() { d.run(id) }})
	}
	return out
}

func (d *desktop) agentItems() []paletteItem {
	var out []paletteItem
	for _, a := range d.agents {
		detail := string(a.Runtime) + " · " + a.Machine
		if a.Role == "super" {
			detail = locales.NavChat() + " · " + detail
		}
		out = append(out, paletteItem{group: locales.NavAgents(), title: a.Name, detail: detail, icon: agentIcon,
			run: func() { d.goTo(pageAgents) }})
	}
	return out
}

// matches filters and orders the items for the query. With no query it
// keeps the order, and only the recent tasks.
func matches(items []paletteItem, query string) []paletteItem {
	var out []paletteItem
	tasks := 0
	for _, it := range items {
		if query == "" {
			if it.task != nil {
				if tasks++; tasks > recentTasks {
					continue
				}
			}
			out = append(out, it)
			continue
		}
		sc := fuzzy(query, it.title+" "+it.detail)
		if t := fuzzy(query, it.title); t >= 0 {
			sc = max(sc, t+50) // a match in the title beats one in the detail
		}
		if sc < 0 {
			continue
		}
		it.score = sc
		out = append(out, it)
	}
	if query != "" {
		slices.SortStableFunc(out, func(x, y paletteItem) int { return y.score - x.score })
	}
	return out
}

// fuzzy scores how well s matches query, as letters in order: more for
// runs and word starts, less for a long s. -1 is no match.
func fuzzy(query, s string) int {
	if query == "" {
		return 0
	}
	q := []rune(strings.ToLower(query))
	r := []rune(strings.ToLower(s))
	score, qi, run := 0, 0, 0
	for i := 0; i < len(r) && qi < len(q); i++ {
		if q[qi] == ' ' {
			qi++
			continue
		}
		if r[i] == q[qi] {
			bonus := 1
			if i == 0 || !unicode.IsLetter(r[i-1]) && !unicode.IsDigit(r[i-1]) {
				bonus += 6
			}
			run++
			score += bonus + run*2
			qi++
		} else {
			run = 0
		}
	}
	for qi < len(q) && q[qi] == ' ' {
		qi++
	}
	if qi < len(q) {
		return -1
	}
	return score*10 - len(r)
}

func (d *desktop) paletteView(c *ui.Context, pal *palette) {
	if !d.pal.open {
		return
	}
	items := matches(d.paletteItems(), d.pal.query)
	d.pal.index = min(max(d.pal.index, 0), max(len(items)-1, 0))
	var chosen func()
	ui.DialogBase(c.Key("palette"), &d.pal.open, func(backdrop, panel ui.Element) {
		backdrop.Background(pal.overlay).Justify(ui.Start).Padding(96, 0, 0, 0)
		floating(panel.Width(560).MaxHeight(440).Radius(14).Background(pal.popover), pal).Clip().Label(locales.GoTo())
		ui.Row(c).Height(46).Shrink(0).Padding(0, 14).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, searchIcon).Size(15, 15).TextColor(pal.textFaint)
			in := ui.TextInputBase(c.Key("q"), &d.pal.query).Grow(1).FontSize(14).
				Placeholder(locales.PalettePlaceholder()).Label(locales.PaletteSearch()).AutoFocus()
			if in.Changed() {
				d.pal.index, d.pal.reveal = 0, true
			}
			if c.Shortcut(0, ui.KeyDown) || c.Shortcut(ui.Ctrl, ui.KeyN) {
				d.pal.index, d.pal.reveal = min(d.pal.index+1, len(items)-1), true
			}
			if c.Shortcut(0, ui.KeyUp) || c.Shortcut(ui.Ctrl, ui.KeyP) {
				d.pal.index, d.pal.reveal = max(d.pal.index-1, 0), true
			}
			if (in.Submitted() || c.Shortcut(0, ui.KeyEnter)) && len(items) > 0 {
				chosen = items[d.pal.index].run
			}
		})
		ui.Box(c).Height(1).Shrink(0).Background(pal.divider)
		reveal := d.pal.reveal
		d.pal.reveal = false
		ui.Scroll(c.Key("results")).Grow(1).MinHeight(0).Padding(6).Children(func() {
			if len(items) == 0 {
				ui.Text(c, locales.PaletteNoMatches()).FontSize(13).TextColor(pal.textFaint).Padding(12)
			}
			group := ""
			for i, it := range items {
				if d.pal.query == "" && it.group != group {
					group = it.group
					ui.Text(c, group).FontSize(12).FontWeight(500).TextColor(pal.textFaint).Padding(10, 10, 4, 10)
				}
				row := ui.Row(c.Key(i)).Height(34).Padding(0, 10).Gap(10).AlignItems(ui.Center).Radius(8).Label(it.title)
				if i == d.pal.index {
					row.Background(pal.rowSelected)
					if reveal {
						row.ScrollIntoView()
					}
				} else if row.Hovered() {
					row.Background(pal.rowHover)
				}
				if row.Clicked() {
					chosen = it.run
				}
				row.Children(func() {
					if it.task != nil {
						statusIcon(c, pal, it.task.State, d.unread[it.task.ID], 14)
					} else {
						ui.Icon(c, it.icon).Size(14, 14).TextColor(pal.textFaint)
					}
					ui.Text(c, it.title).FontSize(13).TextColor(pal.text).SingleLine().Ellipsis("…").Shrink(0).MaxWidth(300)
					ui.Text(c, it.detail).FontSize(12.5).TextColor(pal.textFaint).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
					if it.keys != "" {
						kbd(c, pal, it.keys)
					}
				})
			}
		})
		ui.Row(c).Height(34).Shrink(0).Padding(0, 14).Gap(14).AlignItems(ui.Center).
			BorderWidth(1, 0, 0, 0).BorderColor(pal.divider).Children(func() {
			for _, k := range [][2]string{{"↑↓", locales.PaletteMove()}, {"↩", locales.PaletteOpen()}, {"esc", locales.Close()}} {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					kbd(c, pal, k[0])
					ui.Text(c, k[1]).FontSize(12).TextColor(pal.textFaint).SingleLine()
				})
			}
		})
	})
	if chosen != nil {
		d.pal.open = false
		chosen()
	}
}

// kbd shows a shortcut as a key cap.
func kbd(c *ui.Context, pal *palette, keys string) {
	ui.Text(c, keys).FontSize(11).TextColor(pal.textMuted).Padding(1, 5).Radius(4).Background(pal.kbd).SingleLine().Shrink(0)
}
