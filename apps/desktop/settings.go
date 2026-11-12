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
	t := c.Theme()
	header(c, "Machines", "Each machine runs \"solax daemon start\". The daemon reports which agent CLIs it found.", func() {
		if ui.Button(c, "Refresh").Clicked() {
			d.reloadAgents()
		}
	})
	ui.Scroll(c).Grow(1).Padding(16, 20).Gap(10).Children(func() {
		if len(d.machines) == 0 {
			ui.Text(c, "No machines yet. On a machine, run: solax daemon start").TextColor(t.TextMuted)
		}
		for _, m := range d.machines {
			ui.Column(c.Key(m.ID)).Gap(6).Padding(12, 14).Radius(10).Border(1, t.Border).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, m.Name).Bold().FontSize(15)
					ui.Badge(c, string(m.State))
					if m.Online {
						ui.Badge(c, "online").Background(t.Success).TextColor(t.AccentText)
					} else {
						ui.Badge(c, "offline")
					}
				})
				for _, dr := range m.Drivers {
					line := fmt.Sprintf("%-7s %s", dr.Kind, dr.Version)
					color := t.Text
					if !dr.OK {
						line, color = fmt.Sprintf("%-7s not available: %s", dr.Kind, dr.Problem), t.TextMuted
					}
					ui.Text(c, line).Font("monospace").FontSize(12).TextColor(color)
				}
				if !m.LastSeen.IsZero() {
					ui.Text(c, "last seen "+ago(m.LastSeen)+" · "+m.ID).FontSize(11).TextColor(t.TextMuted).Selectable()
				}
			})
		}
		ui.Text(c, "Approve a machine").Bold().Margin(12, 0, 0, 0)
		ui.Row(c).Gap(8).Children(func() {
			ui.TextInput(c, &d.pairCode).Placeholder("Pairing code, for example K7QM-2XRP").Width(280)
			if ui.Button(c, "Approve").Disabled(strings.TrimSpace(d.pairCode) == "").Clicked() {
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
	t := c.Theme()
	header(c, "Settings", "The Solax server this app talks to.", nil)
	ui.Scroll(c).Grow(1).Padding(16, 20).Gap(12).Children(func() {
		ui.Form(c, func() {
			ui.Field(c, "Profile", func() { ui.TextInput(c, &d.setName).Placeholder("local") })
			ui.Field(c, "Server URL", func() { ui.TextInput(c, &d.setServer).Placeholder("http://127.0.0.1:8740") })
			ui.Field(c, "Client token", func() { ui.TextInput(c, &d.setToken).Password().Placeholder("sxc_… (leave empty to keep the saved one)") }).
				Description("Create one on the server host: solax-server token create --name desktop")
		})
		ready := strings.TrimSpace(d.setName) != "" && strings.TrimSpace(d.setServer) != ""
		if ui.PrimaryButton(c, "Save and connect").Disabled(!ready).Clicked() && ready {
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
					if ui.Button(c, "Use").Clicked() {
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
		ui.Text(c, fmt.Sprintf("Connection: %s %s", d.conn.State, d.conn.Error)).FontSize(12).TextColor(t.TextMuted)
	})
}

