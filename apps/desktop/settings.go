package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/internal/profile"
	"github.com/austiecodes/solax/pkg/client"
)

// machinesPage shows the machines with the agent CLIs their daemons found,
// and approves new machines by their pairing code.
func (d *desktop) machinesPage(c *ui.Context) {
	pal := paletteFor(c)
	d.header(c, "Machines", "Each machine runs \"solax daemon start\". The daemon reports which agent CLIs it found.", func() {
		if button(c, "refresh", "Refresh", plainButton, false) {
			d.reloadAgents()
		}
	})
	ui.Scroll(c).Grow(1).Padding(16, 20).Gap(10).Children(func() {
		if len(d.machines) == 0 {
			ui.Text(c, "No machines yet. On a machine, run: solax daemon start").TextColor(pal.textMuted)
		}
		for _, m := range d.machines {
			ui.Column(c.Key(m.ID)).Gap(6).Padding(12, 14).Radius(10).Border(1, pal.divider).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, m.Name).Bold().FontSize(15)
					ui.Badge(c, string(m.State))
					if m.Online {
						ui.Badge(c, "online").Background(pal.success).TextColor(pal.onAccent)
					} else {
						ui.Badge(c, "offline")
					}
				})
				for _, dr := range m.Drivers {
					line := fmt.Sprintf("%-7s %s", dr.Kind, dr.Version)
					color := pal.text
					if !dr.OK {
						line, color = fmt.Sprintf("%-7s not available: %s", dr.Kind, dr.Problem), pal.textMuted
					}
					ui.Text(c, line).Font("monospace").FontSize(12).TextColor(color)
				}
				if !m.LastSeen.IsZero() {
					ui.Text(c, "last seen "+ago(m.LastSeen)+" · "+m.ID).FontSize(11).TextColor(pal.textMuted).Selectable()
				}
			})
		}
		ui.Text(c, "Approve a machine").Bold().Margin(12, 0, 0, 0)
		ui.Row(c).Gap(8).Children(func() {
			ui.Row(c).Width(300).Children(func() { textField(c, "pair-code", &d.pairCode, "Pairing code, for example K7QM-2XRP") })
			if button(c, "approve", "Approve", plainButton, strings.TrimSpace(d.pairCode) == "") {
				code := strings.TrimSpace(d.pairCode)
				d.do(func(ctx context.Context, c *client.Client) (func(), error) {
					_, err := c.ApproveMachine(ctx, code)
					return func() { d.pairCode = ""; d.reloadAgents() }, err
				})
			}
		})
	})
}

// settingsPage sets the server and the client token. The token goes to
// the macOS keychain.
func (d *desktop) settingsPage(c *ui.Context) {
	pal := paletteFor(c)
	d.header(c, "Settings", "The server this app talks to, and how the app looks.", nil)
	ui.Scroll(c).Grow(1).Padding(16, 20).Gap(12).Children(func() {
		d.lookForm(c)
		ui.Text(c, "Server").Bold().Margin(12, 0, 0, 0)
		ui.Form(c, func() {
			ui.Field(c, "Profile", func() { ui.TextInput(c, &d.setName).Placeholder("local") })
			ui.Field(c, "Server URL", func() { ui.TextInput(c, &d.setServer).Placeholder("http://127.0.0.1:8740") })
			ui.Field(c, "Client token", func() {
				ui.TextInput(c, &d.setToken).Password().Placeholder("sxc_… (leave empty to keep the saved one)")
			}).
				Description("Create one on the server host: solax server token create --name desktop")
		})
		ready := strings.TrimSpace(d.setName) != "" && strings.TrimSpace(d.setServer) != ""
		if button(c, "save", "Save and connect", primaryButton, !ready) {
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
		if ps := d.profiles.List(); len(ps) > 0 {
			ui.Text(c, "Saved profiles").Bold().Margin(12, 0, 0, 0)
			for _, p := range ps {
				ui.Row(c.Key(p.Name)).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, p.Name+" · "+p.Server).Grow(1)
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
		}
		ui.Text(c, fmt.Sprintf("Connection: %s %s", d.conn.State, d.conn.Error)).FontSize(12).TextColor(pal.textMuted)
	})
}

// appearances are the Appearance choices, as the Settings page shows them.
var appearances = []string{"System", "Light", "Dark"}

// lookForm chooses the theme and the appearance. They apply at once.
func (d *desktop) lookForm(c *ui.Context) {
	ui.Text(c, "Look").Bold()
	ui.Form(c, func() {
		ui.Field(c, "Theme", func() {
			if ui.Select(c, &d.prefs.Theme, themeNames).Changed() {
				d.savePrefs()
			}
		}).Description("The colors of Kero's themes. Paper is the default.")
		ui.Field(c, "Appearance", func() {
			if ui.Select(c, &d.setAppearance, appearances).Changed() {
				d.prefs.Appearance = strings.ToLower(d.setAppearance)
				if d.prefs.Appearance == "system" {
					d.prefs.Appearance = ""
				}
				applyAppearance(d.prefs.Appearance)
				d.savePrefs()
			}
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
