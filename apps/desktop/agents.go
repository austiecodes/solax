package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/locales"
	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// agentsPage lists the agents, as Alma lists its crew: a roster, and
// under it the chosen agent, or the form for a new one. Solax routes by
// the description, so the roster shows it.
func (d *desktop) agentsPage(c *ui.Context) {
	pal := paletteFor(c)
	d.showPage(c, pageSpec{icon: agentIcon, title: locales.NavAgents(), sub: locales.AgentsSubtitle()}, func() {
		section(c, "roster", sectionSpec{
			title: locales.Roster(), count: countText(len(d.agents)), rows: listRows,
			desc: locales.RosterHelp(),
			actions: func() {
				iconButton(c, "refresh", reloadIcon, locales.Refresh(), pal, d.reloadAgents)
				iconButton(c, "new", plusIcon, locales.NewAgent(), pal, func() { d.form.open = true })
			},
		}, func() {
			if len(d.agents) == 0 {
				note(c, locales.AgentsEmpty())
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
// its name, its role, its description and its runtime.
func (d *desktop) agentItem(c *ui.Context, pal *palette, a protocol.Agent) {
	cur, _ := d.chosenAgent()
	selected := !d.form.open && a.Name == cur.Name
	m, _ := onOff(a.Online)
	it := listItem(c, a.ID, selected, a.Name, func() {
		dot(c, m)
		ui.Text(c, a.Name).FontSize(13.5).FontWeight(600).TextColor(pal.text).SingleLine().Shrink(0)
		if a.Role == "super" {
			chip(c, locales.NavChat(), markAccent)
		}
		ui.Text(c, a.Description).FontSize(13).TextColor(pal.textMuted).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		chip(c, string(a.Runtime), markQuiet)
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
		desc = locales.AgentNoDesc()
	}
	section(c, "detail-"+a.ID, sectionSpec{
		title: a.Name, count: string(a.Runtime), desc: desc,
		lead: func() { avatar(c, icon, m) },
		actions: func() {
			if a.Role != "super" && button(c, "make-super", locales.AgentMakeSolax(), plainButton, false) {
				name, yes := a.Name, true
				d.do(func(ctx context.Context, c *client.Client) (func(), error) {
					_, err := c.UpdateAgent(ctx, name, protocol.UpdateAgentRequest{Super: &yes})
					return d.reloadAgents, err
				})
			}
			if button(c, "remove", locales.AgentRemove(), removeButton, false) {
				name := a.Name
				d.ask(locales.AgentRemoveQuestion(name), locales.AgentRemoveDetail(name), locales.AgentRemove(), true, func() {
					d.do(func(ctx context.Context, c *client.Client) (func(), error) {
						return d.reloadAgents, c.DeleteAgent(ctx, name)
					})
				})
			}
		},
	}, func() {
		m, word := onOff(a.Online)
		row(c, "status", locales.FieldStatus(), locales.AgentStatusHelp(), func() {
			dot(c, m)
			valueText(c, word, false)
		})
		row(c, "machine", locales.FieldMachine(), "", func() { valueText(c, a.Machine, false) })
		row(c, "dir", locales.FieldDir(), locales.AgentDirHelp(), func() { valueText(c, a.Dir, true) })
		model := a.Model
		if model == "" {
			model = locales.ModelDefault()
		}
		row(c, "model", locales.FieldModel(), "", func() { valueText(c, model, a.Model != "") })
		if a.Role == "super" {
			row(c, "role", locales.FieldRole(), locales.AgentRoleHelp(), func() { chip(c, locales.NavChat(), markAccent) })
		}
		d.instructionsView(c, a)
	})
}

// instructionsView edits the instructions of agent a in place. Save and
// Cancel show when the text changed.
func (d *desktop) instructionsView(c *ui.Context, a protocol.Agent) {
	ed := &d.instr
	// Start a draft for another agent, or follow a change from elsewhere
	// (the CLI) while the draft has no change of its own.
	if ed.agent != a.ID || (ed.text == ed.base && ed.base != a.Instructions && !ed.busy) {
		*ed = instrDraft{agent: a.ID, base: a.Instructions, text: a.Instructions}
	}
	stackRow(c, "instructions", locales.FieldInstructions(), locales.AgentInstructionsHelp(), func() {
		textArea(c, "instructions", &ed.text, locales.FieldInstructionsPlaceholder(), 120)
		if ed.text == ed.base {
			return
		}
		ui.Row(c.Key("instructions-actions")).Gap(8).Justify(ui.End).Children(func() {
			if button(c, "instructions-cancel", locales.Cancel(), plainButton, ed.busy) {
				ed.text = ed.base
			}
			if button(c, "instructions-save", locales.Save(), primaryButton, ed.busy) {
				d.saveInstructions(a)
			}
		})
	})
}

// saveInstructions sends the draft of agent a to the server.
func (d *desktop) saveInstructions(a protocol.Agent) {
	ed := &d.instr
	name, id, text := a.Name, a.ID, strings.TrimSpace(ed.text)
	ed.busy = true
	d.do(func(ctx context.Context, c *client.Client) (func(), error) {
		_, err := c.UpdateAgent(ctx, name, protocol.UpdateAgentRequest{Instructions: &text})
		if err != nil {
			d.update(func() { d.instr.busy = false })
			return nil, err
		}
		return func() {
			// Change the list now: until it loads again, the old text
			// would come back into the draft.
			for i := range d.agents {
				if d.agents[i].ID == id {
					d.agents[i].Instructions = text
				}
			}
			if d.instr.agent == id {
				d.instr = instrDraft{agent: id, base: text, text: text}
			}
			d.reloadAgents()
		}, nil
	})
	if _, err := d.api(); err != nil {
		ed.busy = false
	}
}

// agentFormView is the form for a new agent.
func (d *desktop) agentFormView(c *ui.Context) {
	f := &d.form
	machines := d.machineNames()
	if f.Machine == "" && len(machines) > 0 {
		f.Machine = machines[0]
	}
	runtimes := d.runtimesOf(f.Machine)
	if !contains(runtimes, f.Runtime) && len(runtimes) > 0 {
		f.Runtime = runtimes[0]
	}
	ready := !f.busy && strings.TrimSpace(f.Name) != "" && f.Machine != "" && f.Runtime != "" && strings.TrimSpace(f.Dir) != ""
	pal := paletteFor(c)
	section(c, "new-agent", sectionSpec{
		title: locales.NewAgent(), desc: locales.NewAgentHelp(),
		lead: func() { avatar(c, plusIcon, markAccent) },
		actions: func() {
			if len(d.agents) > 0 {
				iconButton(c, "close", closeIcon, locales.Close(), pal, func() { f.open = false })
			}
		},
		footer: func() {
			if len(d.agents) > 0 && button(c, "cancel", locales.Cancel(), plainButton, false) {
				d.form = agentForm{Machine: f.Machine}
			}
			if button(c, "add-agent", locales.AgentAdd(), primaryButton, !ready) {
				d.addAgent()
			}
		},
	}, func() {
		row(c, "name", locales.FieldName(), locales.FieldNameHelp(), func() {
			ui.Row(c).Width(fieldW).Children(func() { textField(c, "name", &f.Name, "coder") })
		})
		row(c, "machine", locales.FieldMachine(), locales.FieldMachineHelp(), func() {
			if len(machines) == 0 {
				valueText(c, locales.NoMachinesYet(), false)
				return
			}
			ui.Row(c).Width(fieldW).Children(func() { selectField(c, "machine", &f.Machine, machines) })
		})
		row(c, "runtime", locales.FieldRuntime(), locales.FieldRuntimeHelp(), func() {
			ui.Row(c).Width(fieldW).Children(func() { selectField(c, "runtime", &f.Runtime, runtimes) })
		})
		row(c, "dir", locales.FieldDir(), locales.FieldDirHelp(), func() {
			ui.Row(c).Width(fieldW).Children(func() { textField(c, "dir", &f.Dir, "~/src/my-app") })
		})
		row(c, "model", locales.FieldModel(), locales.FieldModelHelp(), func() {
			ui.Row(c).Width(fieldW).Children(func() { textField(c, "model", &f.Model, "haiku") })
		})
		stackRow(c, "desc", locales.FieldDesc(), locales.FieldDescHelp(), func() {
			textArea(c, "desc", &f.Desc, locales.FieldDescPlaceholder(), 72)
		})
		stackRow(c, "instructions", locales.FieldInstructions(), locales.FieldInstructionsHelp(), func() {
			textArea(c, "instructions", &f.Instructions, locales.FieldInstructionsPlaceholder(), 120)
		})
		row(c, "super", locales.AgentMakeSolax(), locales.FieldSuperHelp(), func() {
			ui.Switch(c, &f.Super).Label(locales.AgentMakeSolax())
		})
	})
}

// addAgent sends the form to the server, and on success shows the new
// agent.
func (d *desktop) addAgent() {
	f := &d.form
	req := protocol.CreateAgentRequest{
		Machine: f.Machine, Name: strings.TrimSpace(f.Name), Runtime: ir.Kind(f.Runtime), Dir: strings.TrimSpace(f.Dir),
		Model: strings.TrimSpace(f.Model), Description: strings.TrimSpace(f.Desc), Super: f.Super,
		Instructions: strings.TrimSpace(f.Instructions),
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

// runtimesOf returns the working runtimes of a machine; the built-in ones
// when the machine never reported any (it may be offline).
func (d *desktop) runtimesOf(machine string) []string {
	for _, m := range d.machines {
		if m.Name != machine {
			continue
		}
		var out []string
		for _, dr := range m.Runtimes {
			if dr.OK {
				out = append(out, string(dr.Kind))
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	out := make([]string, len(ir.BuiltinKinds))
	for i, k := range ir.BuiltinKinds {
		out[i] = string(k)
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
