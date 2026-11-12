package main

import (
	"context"
	"fmt"
	"github.com/austiecodes/solax/apps/desktop/internal/profile"
	"github.com/austiecodes/solax/pkg/protocol"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"strings"
	"time"
)

type desktop struct {
	a                                              *app
	win                                            *mygo.Window
	page, status, draft, name, server, token, code string
	busy                                           bool
	profiles                                       []profile.Profile
	workspaces                                     []protocol.Workspace
	tasks                                          []protocol.Task
	machines                                       []protocol.Machine
	messages                                       []protocol.Message
	ws                                             protocol.Workspace
	task                                           protocol.Task
	connection                                     Connection
}

// Background operations capture inputs; completion updates run on the UI thread.
func (d *desktop) run(work func(context.Context) (func(), error)) {
	if d.busy {
		return
	}
	d.busy, d.status = true, ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		done, err := work(ctx)
		d.win.Update(func() {
			d.busy = false
			if err != nil {
				d.status = err.Error()
				return
			}
			if done != nil {
				done()
			}
		})
	}()
}
func (d *desktop) refresh() {
	page, ws, task := d.page, d.ws.ID, d.task.ID
	d.run(func(ctx context.Context) (func(), error) {
		switch page {
		case "Settings":
			ps := (Profiles{d.a}).List()
			return func() { d.profiles = ps }, nil
		case "Machines":
			ms, err := (Machines{d.a}).List(ctx)
			return func() { d.machines = ms }, err
		case "Workspaces":
			wss, err := (Workspaces{d.a}).List(ctx)
			if err != nil {
				return nil, err
			}
			var ts []protocol.Task
			var ms []protocol.Message
			if ws != "" {
				ts, err = (Tasks{d.a}).List(ctx, ws)
				if err != nil {
					return nil, err
				}
				if task != "" {
					ms, err = (Workspaces{d.a}).Messages(ctx, ws, task, 0)
				}
			}
			return func() { d.workspaces, d.tasks, d.messages = wss, ts, ms }, err
		default:
			ms, err := (Super{d.a}).History(ctx, 0)
			return func() { d.messages = ms }, err
		}
	})
}
func (d *desktop) view(c *ui.Context) {
	ui.Column(c).Fill().Padding(20).Gap(12).Children(func() {
		ui.Row(c).Gap(12).Children(func() {
			ui.Text(c, "Solax").Bold().FontSize(24)
			for _, page := range []string{"Super", "Workspaces", "Machines", "Settings"} {
				if ui.Button(c, page).Disabled(d.busy).Clicked() {
					d.page, d.messages, d.draft = page, nil, ""
					d.refresh()
				}
			}
			if ui.Button(c, "Refresh").Disabled(d.busy).Clicked() {
				d.refresh()
			}
		})
		ui.Text(c, string(d.connection.State)+" · "+d.connection.Server)
		if d.connection.Error != "" {
			ui.Text(c, d.connection.Error)
		}
		if d.busy {
			ui.Text(c, "Loading…")
		}
		if d.status != "" {
			ui.Text(c, d.status)
		}
		switch d.page {
		case "Settings":
			d.settings(c)
		case "Machines":
			d.machineView(c)
		case "Workspaces":
			d.workspaceView(c)
		default:
			d.timeline(c)
		}
	})
}
func (d *desktop) appendMessage(m protocol.Message) {
	if d.page == "Workspaces" && (m.WorkspaceID != d.ws.ID || m.TaskID != d.task.ID) {
		return
	}
	for _, old := range d.messages {
		if old.ID == m.ID || old.Seq == m.Seq {
			return
		}
	}
	d.messages = append(d.messages, m)
}
func (d *desktop) timeline(c *ui.Context) {
	ui.Scroll(c).Grow(1).Gap(10).Children(func() {
		for _, m := range d.messages {
			ui.Column(c.Key(m.ID)).Gap(4).Children(func() {
				ui.Text(c, fmt.Sprintf("%s %s · %s", m.Author.Kind, m.Author.ID, m.CreatedAt)).Bold()
				ui.Text(c, m.Text)
			})
		}
	})
	ui.TextArea(c, &d.draft).Disabled(d.busy).Height(90).Placeholder("Write a message")
	if ui.PrimaryButton(c, "Send").Disabled(d.busy || strings.TrimSpace(d.draft) == "" || (d.page == "Workspaces" && d.task.ID == "")).Clicked() {
		page, ws, task, text := d.page, d.ws.ID, d.task.ID, d.draft
		d.run(func(ctx context.Context) (func(), error) {
			var m protocol.Message
			var err error
			if page == "Super" {
				m, err = (Super{d.a}).Send(ctx, text)
			} else {
				m, err = (Workspaces{d.a}).Post(ctx, ws, protocol.PostMessageRequest{TaskID: task, Text: text})
			}
			return func() { d.appendMessage(m); d.draft = "" }, err
		})
	}
}
func (d *desktop) workspaceView(c *ui.Context) {
	ui.Row(c).Gap(16).Grow(1).Children(func() {
		ui.Scroll(c).Width(230).Gap(8).Children(func() {
			for _, ws := range d.workspaces {
				if ui.Button(c.Key(ws.ID), ws.Name).Disabled(d.busy).Clicked() {
					d.ws, d.task, d.messages, d.draft = ws, protocol.Task{}, nil, ""
					d.refresh()
				}
			}
			ui.Text(c, "Tasks").Bold()
			for _, task := range d.tasks {
				if ui.Button(c.Key(task.ID), task.Title+" · "+task.State).Disabled(d.busy).Clicked() {
					d.task, d.messages, d.draft = task, nil, ""
					d.refresh()
				}
			}
		})
		ui.Column(c).Grow(1).Gap(8).Children(func() {
			ui.Text(c, d.ws.Name+" / "+d.task.Title).Bold()
			for _, m := range d.ws.Members {
				ui.Text(c, m.AgentID+" · "+m.Role)
			}
			if d.task.ID == "" {
				ui.Text(c, "Select a workspace and task.")
			}
			d.timeline(c)
		})
	})
}
func (d *desktop) upsertMachine(m protocol.Machine) {
	for i := range d.machines {
		if d.machines[i].ID == m.ID {
			d.machines[i] = m
			return
		}
	}
	d.machines = append(d.machines, m)
}
func (d *desktop) machineView(c *ui.Context) {
	ui.Scroll(c).Grow(1).Gap(12).Children(func() {
		for _, m := range d.machines {
			ui.Text(c, fmt.Sprintf("%s · %s · online: %t", m.Name, m.State, m.Online)).Bold()
			for _, a := range m.Agents {
				ui.Text(c, fmt.Sprintf("%s (%s) %s", a.Name, a.Driver, a.Role))
			}
		}
	})
	ui.TextInput(c, &d.code).Disabled(d.busy).Placeholder("Pairing code")
	if ui.PrimaryButton(c, "Approve").Disabled(d.busy || strings.TrimSpace(d.code) == "").Clicked() {
		code := strings.TrimSpace(d.code)
		d.run(func(ctx context.Context) (func(), error) {
			m, err := (Machines{d.a}).Approve(ctx, code)
			return func() { d.code = ""; d.upsertMachine(m) }, err
		})
	}
}
func (d *desktop) settings(c *ui.Context) {
	ui.Scroll(c).Grow(1).Gap(8).Children(func() {
		for _, p := range d.profiles {
			ui.Text(c, p.Name+" · "+p.Server)
		}
	})
	ui.TextInput(c, &d.name).Placeholder("Profile name")
	ui.TextInput(c, &d.server).Placeholder("https://solax.example.com")
	ui.TextInput(c, &d.token).Password().Placeholder("Client token (sxc_…)")
	if ui.PrimaryButton(c, "Save and use").Disabled(d.busy || strings.TrimSpace(d.name) == "" || strings.TrimSpace(d.server) == "").Clicked() {
		p, token := profile.Profile{Name: strings.TrimSpace(d.name), Server: strings.TrimSpace(d.server)}, d.token
		d.run(func(ctx context.Context) (func(), error) {
			s := Profiles{d.a}
			if err := s.Save(p, token); err != nil {
				return nil, err
			}
			if err := s.Use(p.Name); err != nil {
				return nil, err
			}
			err := s.Test(ctx)
			ps := s.List()
			return func() {
				d.token = ""
				d.profiles = ps
				d.messages = nil
				d.workspaces = nil
				d.tasks = nil
				d.machines = nil
				d.ws = protocol.Workspace{}
				d.task = protocol.Task{}
				if err != nil {
					d.status = err.Error()
				} else {
					d.status = "Connected."
				}
			}, nil
		})
	}
}
