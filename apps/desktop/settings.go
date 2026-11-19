package main

import (
	"context"
	"errors"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/internal/profile"
	"github.com/austiecodes/solax/apps/desktop/locales"
	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/protocol"
)

// machinesPage lists the machines with the agent CLIs their daemons
// found, shows the chosen one, and approves new machines by their
// pairing code.
func (d *desktop) machinesPage(c *ui.Context) {
	pal := paletteFor(c)
	d.showPage(c, pageSpec{icon: machineIcon, title: locales.NavMachines(), sub: locales.MachinesSubtitle()}, func() {
		section(c, "machines", sectionSpec{
			title: locales.NavMachines(), count: countText(len(d.machines)), rows: listRows,
			desc:    locales.MachinesHelp(),
			actions: func() { iconButton(c, "refresh", reloadIcon, locales.Refresh(), pal, d.reloadAgents) },
		}, func() {
			if len(d.machines) == 0 {
				note(c, locales.MachinesEmpty())
			}
			for _, m := range d.machines {
				d.machineItem(c, pal, m)
			}
		})
		if m, ok := d.chosenMachine(); ok {
			d.machineDetail(c, m)
		}
		section(c, "pair", sectionSpec{
			title: locales.MachineApproveTitle(),
			desc:  locales.PairHelp(),
		}, func() {
			row(c, "code", locales.PairCode(), "", func() {
				ui.Row(c).Width(200).Children(func() { textField(c, "pair-code", &d.pairCode, "K7QM-2XRP") })
				if button(c, "approve", locales.Approve(), primaryButton, strings.TrimSpace(d.pairCode) == "") {
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
// online, its name, its approval if not yet, its runtimes and when it was
// last seen.
func (d *desktop) machineItem(c *ui.Context, pal *palette, m protocol.Machine) {
	cur, _ := d.chosenMachine()
	mk, _ := onOff(m.Online)
	it := listItem(c, m.ID, m.ID == cur.ID, m.Name, func() {
		dot(c, mk)
		ui.Text(c, m.Name).FontSize(13.5).FontWeight(600).TextColor(pal.text).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		if m.State != protocol.MachineApproved {
			chip(c, stateWord(string(m.State)), stateMark(m.State))
		}
		ui.Spacer(c)
		for _, dr := range m.Runtimes {
			if dr.OK {
				chip(c, string(dr.Kind), markQuiet)
			}
		}
		when := locales.SeenOnline()
		if !m.Online && !m.LastSeen.IsZero() {
			when = ago(m.LastSeen)
		} else if !m.Online {
			when = locales.NeverSeen()
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
		title: m.Name, count: stateWord(string(m.State)),
		lead: func() { avatar(c, machineIcon, mk) },
	}, func() {
		row(c, "status", locales.FieldStatus(), "", func() {
			dot(c, mk)
			valueText(c, word, false)
		})
		if !m.LastSeen.IsZero() {
			row(c, "seen", locales.FieldLastSeen(), "", func() { valueText(c, ago(m.LastSeen), false) })
		}
		row(c, "id", locales.FieldMachineID(), "", func() { valueText(c, m.ID, true) })
		if len(m.Runtimes) == 0 {
			row(c, "runtimes", locales.FieldAgentCLIs(), locales.RuntimesNotReported(), nil)
		}
		for _, dr := range m.Runtimes {
			help := dr.Version
			if !dr.OK {
				help = dr.Problem
			}
			row(c, "runtime-"+string(dr.Kind), string(dr.Kind), help, func() {
				if dr.OK {
					chip(c, locales.RuntimeReady(), markGood)
				} else {
					chip(c, locales.RuntimeNotAvailable(), markBad)
				}
			})
		}
	})
}

// settingsPage sets the look, the server and the client token. The token
// goes to the macOS keychain.
func (d *desktop) settingsPage(c *ui.Context) {
	d.showPage(c, pageSpec{icon: gearIcon, title: locales.Settings(), sub: locales.SettingsSubtitle()}, func() {
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
		title: locales.SettingsServer(), desc: locales.ServerHelp(),
		actions: func() { d.connChip(c) },
		footer: func() {
			if button(c, "save", locales.SaveConnect(), primaryButton, !ready) {
				d.saveServer()
			}
		},
	}, func() {
		row(c, "profile", locales.FieldProfile(), locales.ProfileHelp(), func() {
			ui.Row(c).Width(fieldW).Children(func() { textField(c, "profile", &d.setName, "local") })
		})
		row(c, "url", locales.FieldServerURL(), "", func() {
			ui.Row(c).Width(fieldW).Children(func() { textField(c, "url", &d.setServer, "http://127.0.0.1:8740") })
		})
		row(c, "token", locales.FieldToken(), locales.TokenHelp(), func() {
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
	state := stateWord(d.conn.State)
	if d.conn.State == "" {
		state = locales.NotConnected()
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
				d.status = locales.SettingsError(errText(err))
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
	section(c, "profiles", sectionSpec{title: locales.SavedProfiles(), count: countText(len(ps))}, func() {
		for _, p := range ps {
			row(c, p.Name, p.Name, p.Server, func() {
				if button(c, "use", locales.Use(), plainButton, false) {
					name := p.Name
					go func() {
						if err := d.profiles.Use(name); err != nil {
							d.update(func() { d.status = errText(err) })
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
	section(c, "local", sectionSpec{title: locales.ThisMac()}, func() {
		if d.localErr != nil {
			help := locales.LocalHint()
			if !errors.Is(d.localErr, profile.ErrNoLocal) {
				help += " " + d.localErr.Error()
			}
			row(c, "setup", locales.LocalNotSetUp(), help, func() {
				if button(c, "local-check", locales.CheckAgain(), plainButton, false) {
					go d.checkLocal()
				}
			})
			return
		}
		if !d.local.Loopback() {
			row(c, "remote", locales.AnotherHost(), locales.LocalRemote(d.local.Server), nil)
			return
		}
		inUse := d.setName == profile.LocalName && d.setServer == d.local.Server
		label := locales.UseThisServer()
		if inUse {
			label = locales.InUse()
		}
		row(c, "local", locales.LocalServer(), locales.LocalRuns(d.local.Server), func() {
			if button(c, "local-use", label, plainButton, inUse) {
				go d.useLocal()
			}
		})
	})
}

// appearances are the Appearance prefs: the system's, light and dark.
var appearances = []string{"", "light", "dark"}

// lookForm chooses the theme, the appearance and the language. They apply
// at once.
func (d *desktop) lookForm(c *ui.Context) {
	section(c, "look", sectionSpec{title: locales.Look()}, func() {
		row(c, "theme", locales.Theme(), locales.ThemeHelp(), func() {
			ui.Row(c).Width(180).Children(func() {
				if selectField(c, "theme", &d.prefs.Theme, themeNames).Changed() {
					d.savePrefs()
				}
			})
		})
		row(c, "appearance", locales.Appearance(), locales.AppearanceHelp(), func() {
			ui.Row(c).Width(180).Children(func() {
				if a, ok := choose(c, "appearance", d.prefs.Appearance, appearances, appearanceLabel); ok {
					d.prefs.Appearance = a
					applyAppearance(a)
					d.savePrefs()
				}
			})
		})
		row(c, "language", locales.Language(), locales.LanguageHelp(), func() {
			ui.Row(c).Width(180).Children(func() {
				if tag, ok := choose(c, "language", d.lang, languages(), languageLabel); ok {
					d.setLanguage(tag)
				}
			})
		})
	})
}

// sidebarForm chooses how the sidebar groups the tasks.
func (d *desktop) sidebarForm(c *ui.Context) {
	section(c, "sidebar", sectionSpec{title: locales.SidebarSection()}, func() {
		row(c, "grouping", locales.GroupTasksBy(), locales.GroupTasksHelp(), func() {
			ui.Row(c).Width(180).Children(func() {
				if id, ok := choose(c, "grouping", d.prefs.TaskGroups, groupingIDs(), groupingLabel); ok {
					d.prefs.TaskGroups = id
					d.savePrefs()
				}
			})
		})
	})
}

// choose is a select of values shown by their labels in the language
// shown. It reports a new choice.
func choose(c *ui.Context, key, value string, values []string, label func(string) string) (string, bool) {
	labels := make([]string, len(values))
	shown := ""
	for i, v := range values {
		labels[i] = label(v)
		if v == value {
			shown = labels[i]
		}
	}
	if !selectField(c, key, &shown, labels).Changed() {
		return value, false
	}
	for i, l := range labels {
		if l == shown {
			return values[i], values[i] != value
		}
	}
	return value, false
}

// appearanceLabel is the Settings label of an Appearance pref.
func appearanceLabel(a string) string {
	switch a {
	case "light":
		return locales.AppearanceLight()
	case "dark":
		return locales.AppearanceDark()
	}
	return locales.AppearanceSystem()
}
