package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// agentsPage lists the agents, as Alma lists its crew: a roster, and
// under it the chosen agent, or the form for a new one. Solax routes by
// the description, so the roster shows it.
func (d *desktop) agentsPage(c *ui.Context) {
	pal := paletteFor(c)
	d.showPage(c, pageSpec{icon: agentIcon, title: "Agents", sub: "Coding agents on your machines"}, func() {
		section(c, "roster", sectionSpec{
			title: "Roster", count: countText(len(d.agents)), rows: listRows,
			desc: "Solax gives each job to the agent whose description fits it best.",
			actions: func() {
				iconButton(c, "refresh", reloadIcon, "Refresh", pal, d.reloadAgents)
				iconButton(c, "new", plusIcon, "New agent", pal, func() { d.form.open = true })
			},
		}, func() {
			if len(d.agents) == 0 {
				note(c, "No agents yet. Add one below.")
			}
			for _, a := range d.agents {
				d.agentItem(c, pal, a)
			}
		})
		if a, ok := d.chosenAgent(); ok && !d.form.open {
			d.agentDetail(c, a)
		} else {
			d.agentFormView(c)
		}
	})
}

// countText is a count for a section head, or nothing for none.
func countText(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprint(n)
}

// chosenAgent is the agent the page shows: the one the user chose, else
// the first.
func (d *desktop) chosenAgent() (protocol.Agent, bool) {
	for _, a := range d.agents {
		if a.Name == d.agentSel {
			return a, true
		}
	}
	if len(d.agents) > 0 {
		return d.agents[0], true
	}
	return protocol.Agent{}, false
}

// agentItem is an agent in the roster, on one line: whether it is online,
// its name, its role, its description and its driver.
func (d *desktop) agentItem(c *ui.Context, pal *palette, a protocol.Agent) {
	cur, _ := d.chosenAgent()
	selected := !d.form.open && a.Name == cur.Name
	m, _ := onOff(a.Online)
	it := listItem(c, a.ID, selected, a.Name, func() {
		dot(c, m)
		ui.Text(c, a.Name).FontSize(13.5).FontWeight(600).TextColor(pal.text).SingleLine().Shrink(0)
		if a.Role == "super" {
			chip(c, "Solax", markAccent)
		}
		ui.Text(c, a.Description).FontSize(13).TextColor(pal.textMuted).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		chip(c, string(a.Driver), markQuiet)
	})
	if it.Clicked() {
		d.agentSel, d.form.open = a.Name, false
	}
}

// agentDetail shows the chosen agent, with its actions.
func (d *desktop) agentDetail(c *ui.Context, a protocol.Agent) {
	icon, m := agentIcon, markQuiet
	if a.Role == "super" {
		icon, m = sparklesIcon, markAccent
	}
	desc := a.Description
	if desc == "" {
		desc = "No description. Solax routes by the description, so it does not give jobs to this agent by itself."
	}
	section(c, "detail-"+a.ID, sectionSpec{
		title: a.Name, count: string(a.Driver), desc: desc,
		lead: func() { avatar(c, icon, m) },
		actions: func() {
			if a.Role != "super" && button(c, "make-super", "Make it Solax", plainButton, false) {
				name, yes := a.Name, true
				d.do(func(ctx context.Context, c *client.Client) (func(), error) {
					_, err := c.UpdateAgent(ctx, name, protocol.UpdateAgentRequest{Super: &yes})
					return d.reloadAgents, err
				})
			}
			if button(c, "remove", "Remove", removeButton, false) {
				name := a.Name
				d.ask("Remove "+name+"?", "Solax can no longer give jobs to "+name+". Its past jobs stay.", "Remove", true, func() {
					d.do(func(ctx context.Context, c *client.Client) (func(), error) {
						return d.reloadAgents, c.DeleteAgent(ctx, name)
					})
				})
			}
		},
	}, func() {
		m, word := onOff(a.Online)
		row(c, "status", "Status", "Online when its machine is connected and has the driver.", func() {
			dot(c, m)
			valueText(c, word, false)
		})
		row(c, "machine", "Machine", "", func() { valueText(c, a.Machine, false) })
		row(c, "dir", "Work folder", "The agent works in this folder.", func() { valueText(c, a.Dir, true) })
		model := a.Model
		if model == "" {
			model = "Driver default"
		}
		row(c, "model", "Model", "", func() { valueText(c, model, a.Model != "") })
		if a.Role == "super" {
			row(c, "role", "Role", "You talk to this agent. It gives jobs to the others.", func() { chip(c, "Solax", markAccent) })
		}
	})
}

// agentFormView is the form for a new agent.
func (d *desktop) agentFormView(c *ui.Context) {
	f := &d.form
	machines := d.machineNames()
	if f.Machine == "" && len(machines) > 0 {
		f.Machine = machines[0]
	}
	drivers := d.driversOf(f.Machine)
	if !contains(drivers, f.Driver) && len(drivers) > 0 {
		f.Driver = drivers[0]
	}
	ready := !f.busy && strings.TrimSpace(f.Name) != "" && f.Machine != "" && f.Driver != "" && strings.TrimSpace(f.Dir) != ""
	pal := paletteFor(c)
	section(c, "new-agent", sectionSpec{
		title: "New agent", desc: "Solax reads the description to choose the agent for a job.",
		lead: func() { avatar(c, plusIcon, markAccent) },
		actions: func() {
			if len(d.agents) > 0 {
				iconButton(c, "close", closeIcon, "Close", pal, func() { f.open = false })
			}
		},
		footer: func() {
			if len(d.agents) > 0 && button(c, "cancel", "Cancel", plainButton, false) {
				d.form = agentForm{Machine: f.Machine}
			}
			if button(c, "add-agent", "Add agent", primaryButton, !ready) {
				d.addAgent()
			}
		},
	}, func() {
		row(c, "name", "Name", "Lower case, digits and dashes.", func() {
			ui.Row(c).Width(fieldW).Children(func() { textField(c, "name", &f.Name, "coder") })
		})
		row(c, "machine", "Machine", "A machine that you approved.", func() {
			if len(machines) == 0 {
				valueText(c, "No machines yet", false)
				return
			}
			ui.Row(c).Width(fieldW).Children(func() { selectField(c, "machine", &f.Machine, machines) })
		})
		row(c, "driver", "Driver", "The agent CLIs that the daemon found on this machine.", func() {
			ui.Row(c).Width(fieldW).Children(func() { selectField(c, "driver", &f.Driver, drivers) })
		})
		row(c, "dir", "Work folder", "The agent works in this folder on its machine.", func() {
			ui.Row(c).Width(fieldW).Children(func() { textField(c, "dir", &f.Dir, "~/src/my-app") })
		})
		row(c, "model", "Model", "Optional. Empty uses the driver's default.", func() {
			ui.Row(c).Width(fieldW).Children(func() { textField(c, "model", &f.Model, "haiku") })
		})
		stackRow(c, "desc", "Description", "What this agent is good at. Solax routes by it.", func() {
			textArea(c, "desc", &f.Desc, "Implements features, refactors and fixes code", 72)
		})
		row(c, "super", "Make it Solax", "You talk to Solax. It gives jobs to the other agents.", func() {
			ui.Switch(c, &f.Super).Label("Make it Solax")
		})
	})
}

// addAgent sends the form to the server, and on success shows the new
// agent.
func (d *desktop) addAgent() {
	f := &d.form
	req := protocol.CreateAgentRequest{
		Machine: f.Machine, Name: strings.TrimSpace(f.Name), Driver: ir.Kind(f.Driver), Dir: strings.TrimSpace(f.Dir),
		Model: strings.TrimSpace(f.Model), Description: strings.TrimSpace(f.Desc), Super: f.Super,
	}
	f.busy = true
	d.do(func(ctx context.Context, c *client.Client) (func(), error) {
		_, err := c.CreateAgent(ctx, req)
		if err != nil {
			d.update(func() { d.form.busy = false })
			return nil, err
		}
		return func() {
			d.form = agentForm{Machine: req.Machine}
			d.agentSel = req.Name
			d.reloadAgents()
		}, nil
	})
	if _, err := d.api(); err != nil {
		f.busy = false
	}
}

// machineNames are the approved machines, by name.
func (d *desktop) machineNames() []string {
	var out []string
	for _, m := range d.machines {
		if m.State == protocol.MachineApproved {
			out = append(out, m.Name)
		}
	}
	return out
}

// driversOf returns the working drivers of a machine; all three when the
// machine never reported any (it may be offline).
func (d *desktop) driversOf(machine string) []string {
	for _, m := range d.machines {
		if m.Name != machine {
			continue
		}
		var out []string
		for _, dr := range m.Drivers {
			if dr.OK {
				out = append(out, string(dr.Kind))
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return []string{"claude", "codex", "pi"}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
