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

// agentsPage lists the agents and sets up new ones. The Super Agent
// routes by the description, so the form asks for it.
func (d *desktop) agentsPage(c *ui.Context) {
	pal := paletteFor(c)
	d.header(c, "Agents", "Coding agents on your machines. Solax picks one by its description.", func() {
		if button(c, "refresh", "Refresh", plainButton, false) {
			d.reloadAgents()
		}
	})
	ui.Scroll(c).Grow(1).Padding(16, 20).Gap(10).Children(func() {
		if len(d.agents) == 0 {
			ui.Text(c, "No agents yet. Add one below.").TextColor(pal.textMuted)
		}
		for _, a := range d.agents {
			d.agentCard(c, a)
		}
		ui.Text(c, "Add an agent").Bold().FontSize(15).Margin(12, 0, 0, 0)
		d.agentFormView(c)
	})
}

func (d *desktop) agentCard(c *ui.Context, a protocol.Agent) {
	pal := paletteFor(c)
	ui.Column(c.Key(a.ID)).Gap(6).Padding(12, 14).Radius(10).Border(1, pal.divider).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, a.Name).Bold().FontSize(15)
			if a.Role == "super" {
				ui.Badge(c, "Solax").Background(pal.accent).TextColor(pal.onAccent)
			}
			ui.Badge(c, string(a.Driver))
			if a.Online {
				ui.Badge(c, "online").Background(pal.success).TextColor(pal.onAccent)
			} else {
				ui.Badge(c, "offline")
			}
			ui.Spacer(c)
			if a.Role != "super" && button(c, "make-super", "Make It Solax", plainButton, false) {
				name, yes := a.Name, true
				d.do(func(ctx context.Context, c *client.Client) (func(), error) {
					_, err := c.UpdateAgent(ctx, name, protocol.UpdateAgentRequest{Super: &yes})
					return d.reloadAgents, err
				})
			}
			if button(c, "remove", "Remove", plainButton, false) {
				name := a.Name
				d.ask("Remove "+name+"?", "Solax can no longer give jobs to "+name+". Its past jobs stay.", "Remove", true, func() {
					d.do(func(ctx context.Context, c *client.Client) (func(), error) {
						return d.reloadAgents, c.DeleteAgent(ctx, name)
					})
				})
			}
		})
		if a.Description != "" {
			ui.Text(c, a.Description)
		}
		info := fmt.Sprintf("%s · %s", a.Machine, a.Dir)
		if a.Model != "" {
			info += " · model " + a.Model
		}
		ui.Text(c, info).FontSize(12).TextColor(pal.textMuted).Font("monospace")
	})
}

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
	ui.Form(c, func() {
		ui.Field(c, "Name", func() { ui.TextInput(c, &f.Name).Placeholder("coder") }).Description("Lower case, digits and dashes. Solax uses it to route.")
		ui.Field(c, "Machine", func() { ui.Select(c, &f.Machine, machines) })
		ui.Field(c, "Driver", func() { ui.Select(c, &f.Driver, drivers) }).Description("The agent CLIs that the daemon found on this machine.")
		ui.Field(c, "Work folder", func() { ui.TextInput(c, &f.Dir).Placeholder("~/src/my-app") })
		ui.Field(c, "Model", func() { ui.TextInput(c, &f.Model).Placeholder("optional, for example haiku") })
		ui.Field(c, "Description", func() { ui.TextArea(c, &f.Desc).Height(70).Placeholder("What this agent is good at") })
		ui.Field(c, "Role", func() { ui.Checkbox(c, &f.Super, "This is Solax (the agent you talk to)") })
	})
	ready := !f.busy && strings.TrimSpace(f.Name) != "" && f.Machine != "" && f.Driver != "" && strings.TrimSpace(f.Dir) != ""
	if button(c, "add-agent", "Add agent", primaryButton, !ready) {
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
				d.reloadAgents()
			}, nil
		})
		if _, err := d.api(); err != nil {
			f.busy = false
		}
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
