package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/internal/profile"
	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/protocol"
)

// machinesPage lists the machines with the agent CLIs their daemons
// found, shows the chosen one, and approves new machines by their
// pairing code.
func (d *desktop) machinesPage(c *ui.Context) {
	pal := paletteFor(c)
	d.showPage(c, pageSpec{icon: machineIcon, title: "Machines", sub: "Where your agents run"}, func() {
		section(c, "machines", sectionSpec{
			title: "Machines", count: countText(len(d.machines)), rows: listRows,
			desc:    "Each machine runs \"solax daemon start\". The daemon reports the agent CLIs that it finds.",
			actions: func() { iconButton(c, "refresh", reloadIcon, "Refresh", pal, d.reloadAgents) },
		}, func() {
			if len(d.machines) == 0 {
				note(c, "No machines yet. On this Mac, run \"solax setup local\". On another machine, run \"solax daemon start\".")
			}
			for _, m := range d.machines {
				d.machineItem(c, pal, m)
			}
		})
		if m, ok := d.chosenMachine(); ok {
			d.machineDetail(c, m)
		}
		section(c, "pair", sectionSpec{
			title: "Approve a machine",
			desc:  "Run \"solax daemon start\" on the new machine. It shows a pairing code. Type the code here.",
		}, func() {
			row(c, "code", "Pairing code", "", func() {
				ui.Row(c).Width(200).Children(func() { textField(c, "pair-code", &d.pairCode, "K7QM-2XRP") })
				if button(c, "approve", "Approve", primaryButton, strings.TrimSpace(d.pairCode) == "") {
					code := strings.TrimSpace(d.pairCode)
					d.do(func(ctx context.Context, c *client.Client) (func(), error) {
						_, err := c.ApproveMachine(ctx, code)
						return func() { d.pairCode = ""; d.reloadAgents() }, err
					})
				}
			})
		})
	})
}

// chosenMachine is the machine the page shows: the one the user chose,
// else the first.
func (d *desktop) chosenMachine() (protocol.Machine, bool) {
	for _, m := range d.machines {
		if m.ID == d.machineSel {
			return m, true
		}
	}
	if len(d.machines) > 0 {
		return d.machines[0], true
	}
	return protocol.Machine{}, false
}

// machineItem is a machine in the list, on one line: whether it is
// online, its name, its approval if not yet, its drivers and when it was
// last seen.
func (d *desktop) machineItem(c *ui.Context, pal *palette, m protocol.Machine) {
	cur, _ := d.chosenMachine()
	mk, _ := onOff(m.Online)
	it := listItem(c, m.ID, m.ID == cur.ID, m.Name, func() {
		dot(c, mk)
		ui.Text(c, m.Name).FontSize(13.5).FontWeight(600).TextColor(pal.text).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		if m.State != protocol.MachineApproved {
			chip(c, string(m.State), stateMark(m.State))
		}
		ui.Spacer(c)
		for _, dr := range m.Drivers {
			if dr.OK {
				chip(c, string(dr.Kind), markQuiet)
			}
		}
		when := "online"
		if !m.Online && !m.LastSeen.IsZero() {
			when = ago(m.LastSeen)
		} else if !m.Online {
			when = "never seen"
		}
		ui.Text(c, when).FontSize(12).TextColor(pal.textFaint).SingleLine().TextAlign(ui.End).Width(80).Shrink(0)
	})
	if it.Clicked() {
		d.machineSel = m.ID
	}
}

func stateMark(s protocol.MachineState) mark {
	switch s {
	case protocol.MachinePending:
		return markWarn
	case protocol.MachineRevoked:
		return markBad
	}
	return markQuiet
}

// machineDetail shows the chosen machine: its state and each agent CLI
// that its daemon looked for.
func (d *desktop) machineDetail(c *ui.Context, m protocol.Machine) {
	mk, word := onOff(m.Online)
	section(c, "detail-"+m.ID, sectionSpec{
		title: m.Name, count: string(m.State),
		lead: func() { avatar(c, machineIcon, mk) },
	}, func() {
		row(c, "status", "Status", "", func() {
			dot(c, mk)
			valueText(c, word, false)
		})
		if !m.LastSeen.IsZero() {
			row(c, "seen", "Last seen", "", func() { valueText(c, ago(m.LastSeen), false) })
		}
		row(c, "id", "Machine ID", "", func() { valueText(c, m.ID, true) })
		if len(m.Drivers) == 0 {
			row(c, "drivers", "Agent CLIs", "The daemon has not reported yet.", nil)
		}
		for _, dr := range m.Drivers {
			help := dr.Version
			if !dr.OK {
				help = dr.Problem
			}
			row(c, "driver-"+string(dr.Kind), string(dr.Kind), help, func() {
				if dr.OK {
					chip(c, "Ready", markGood)
				} else {
					chip(c, "Not available", markBad)
				}
			})
		}
	})
}

// settingsPage sets the look, the server and the client token. The token
// goes to the macOS keychain.
func (d *desktop) settingsPage(c *ui.Context) {
	d.showPage(c, pageSpec{icon: gearIcon, title: "Settings", sub: "How the app looks, and the server it talks to"}, func() {
		d.lookForm(c)
		d.sidebarForm(c)
		d.localForm(c)
		d.serverForm(c)
		d.profilesView(c)
	})
}

// serverForm sets the server and the client token, then connects.
func (d *desktop) serverForm(c *ui.Context) {
	ready := strings.TrimSpace(d.setName) != "" && strings.TrimSpace(d.setServer) != ""
	section(c, "server", sectionSpec{
		title: "Server", desc: "The Solax server this app talks to.",
		actions: func() { d.connChip(c) },
		footer: func() {
			if button(c, "save", "Save and connect", primaryButton, !ready) {
				d.saveServer()
			}
		},
	}, func() {
		row(c, "profile", "Profile", "A name for this server.", func() {
			ui.Row(c).Width(fieldW).Children(func() { textField(c, "profile", &d.setName, "local") })
		})
		row(c, "url", "Server URL", "", func() {
			ui.Row(c).Width(fieldW).Children(func() { textField(c, "url", &d.setServer, "http://127.0.0.1:8740") })
		})
		row(c, "token", "Client token", "Create one on the server host: solax server token create --name desktop. Leave it empty to keep the saved one.", func() {
			ui.Row(c).Width(fieldW).Children(func() { textField(c, "token", &d.setToken, "sxc_…").Password() })
		})
	})
}

// connChip says how the stream to the server is, by a dot and a word.
func (d *desktop) connChip(c *ui.Context) {
	m := markBad
	switch d.conn.State {
	case "connected":
		m = markGood
	case "connecting":
		m = markWarn
	}
	state := d.conn.State
	if state == "" {
		state = "not connected"
	}
	ui.Row(c.Key("conn")).Gap(6).AlignItems(ui.Center).Tooltip(strings.TrimSpace(d.conn.Server + "\n" + d.conn.Error)).Children(func() {
		dot(c, m)
		ui.Text(c, state).FontSize(12.5).TextColor(paletteFor(c).textMuted).SingleLine()
	})
}

// saveServer saves the profile and its token, uses it, and checks that
// the server answers.
func (d *desktop) saveServer() {
	p, token := profile.Profile{Name: d.setName, Server: d.setServer}, d.setToken
	go func() {
		err := d.profiles.Save(p, token)
		if err == nil {
			err = d.profiles.Use(strings.TrimSpace(p.Name))
		}
		if err == nil {
			_, tok, cerr := d.profiles.Current()
			if cerr == nil {
				err = client.New(strings.TrimRight(p.Server, "/"), tok).Ping(context.Background())
			}
		}
		d.update(func() {
			d.setToken = ""
			if err != nil {
				d.status = "settings: " + err.Error()
				return
			}
			d.status = ""
			d.page = pageChat
		})
		if err == nil {
			d.connect()
		}
	}()
}

// profilesView lists the saved profiles; a click uses one.
func (d *desktop) profilesView(c *ui.Context) {
	ps := d.profiles.List()
	if len(ps) == 0 {
		return
	}
	section(c, "profiles", sectionSpec{title: "Saved profiles", count: countText(len(ps))}, func() {
		for _, p := range ps {
			row(c, p.Name, p.Name, p.Server, func() {
				if button(c, "use", "Use", plainButton, false) {
					name := p.Name
					go func() {
						if err := d.profiles.Use(name); err != nil {
							d.update(func() { d.status = err.Error() })
							return
						}
						d.connect()
					}()
				}
			})
		}
	})
}

// localForm offers the server that the solax CLI of this Mac uses. After
// "solax setup local", one click connects the app to it.
func (d *desktop) localForm(c *ui.Context) {
	section(c, "local", sectionSpec{title: "This Mac"}, func() {
		if d.localErr != nil {
			help := "To run Solax only on this Mac, run \"solax setup local\" in a terminal. It starts the server and the daemon here."
			if !errors.Is(d.localErr, profile.ErrNoLocal) {
				help += " " + d.localErr.Error()
			}
			row(c, "setup", "Not set up", help, func() {
				if button(c, "local-check", "Check again", plainButton, false) {
					go d.checkLocal()
				}
			})
			return
		}
		if !d.local.Loopback() {
			row(c, "remote", "Another host", fmt.Sprintf("The solax CLI uses %s. To use it here, add it under Server with a client token.", d.local.Server), nil)
			return
		}
		inUse := d.setName == profile.LocalName && d.setServer == d.local.Server
		label := "Use this server"
		if inUse {
			label = "In use"
		}
		row(c, "local", "Local server", fmt.Sprintf("The server and the daemon run on this Mac, on %s.", d.local.Server), func() {
			if button(c, "local-use", label, plainButton, inUse) {
				go d.useLocal()
			}
		})
	})
}

// appearances are the Appearance choices, as the Settings page shows them.
var appearances = []string{"System", "Light", "Dark"}

// lookForm chooses the theme and the appearance. They apply at once.
func (d *desktop) lookForm(c *ui.Context) {
	section(c, "look", sectionSpec{title: "Look"}, func() {
		row(c, "theme", "Theme", "Graphite is the default. The others are Kero's themes.", func() {
			ui.Row(c).Width(180).Children(func() {
				if selectField(c, "theme", &d.prefs.Theme, themeNames).Changed() {
					d.savePrefs()
				}
			})
		})
		row(c, "appearance", "Appearance", "System follows the macOS setting.", func() {
			ui.Row(c).Width(180).Children(func() {
				if selectField(c, "appearance", &d.setAppearance, appearances).Changed() {
					d.prefs.Appearance = strings.ToLower(d.setAppearance)
					if d.prefs.Appearance == "system" {
						d.prefs.Appearance = ""
					}
					applyAppearance(d.prefs.Appearance)
					d.savePrefs()
				}
			})
		})
	})
}

// sidebarForm chooses how the sidebar groups the jobs.
func (d *desktop) sidebarForm(c *ui.Context) {
	section(c, "sidebar", sectionSpec{title: "Sidebar"}, func() {
		row(c, "grouping", "Group jobs by", "How the sidebar lists the recent jobs. A project is the work folder of the job's agent.", func() {
			label := groupingLabel(d.prefs.JobGroups)
			labels := make([]string, len(groupings))
			for i, g := range groupings {
				labels[i] = g.label
			}
			ui.Row(c).Width(180).Children(func() {
				if selectField(c, "grouping", &label, labels).Changed() {
					d.prefs.JobGroups = groupingByLabel(label)
					d.savePrefs()
				}
			})
		})
	})
}

// appearanceLabel is the Settings label of an Appearance pref.
func appearanceLabel(a string) string {
	switch a {
	case "light":
		return "Light"
	case "dark":
		return "Dark"
	}
	return "System"
}
