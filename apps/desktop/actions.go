package main

import (
	"github.com/egoist/mygo"

	"github.com/austiecodes/solax/apps/desktop/internal/actions"
)

// run runs an action, from the menu bar, a button or (later) the command
// palette. It runs on the UI thread.
func (d *desktop) run(id string) {
	switch id {
	case "palette.open":
		d.openPalette()
	case "settings.open":
		d.goTo(pageSettings)
	case "sidebar.toggle":
		d.prefs.SidebarHidden = !d.prefs.SidebarHidden
		d.savePrefs()
	case "theme.toggle":
		d.toggleDark()
	case "data.reload":
		d.reloadAll()
	case "nav.jobs":
		d.jobSel = "" // the list of all jobs; a job opens from the sidebar
		d.goTo(pageJobs)
	default:
		for _, p := range navPages {
			if p.action == id {
				d.goTo(p.id)
			}
		}
	}
	if d.win != nil {
		d.win.Invalidate()
	}
}

// goTo shows a page and clears the last error, which was about the page
// left.
func (d *desktop) goTo(page string) {
	d.page, d.status = page, ""
	if page == pageJobs && d.jobSel != "" {
		d.loadEvents(d.jobSel)
	}
}

// toggleDark turns the window to the other appearance than the one it
// shows, over the system's.
func (d *desktop) toggleDark() {
	d.prefs.Appearance = "dark"
	if mygo.Theme.IsDark() {
		d.prefs.Appearance = "light"
	}
	d.setAppearance = appearanceLabel(d.prefs.Appearance)
	applyAppearance(d.prefs.Appearance)
	d.savePrefs()
}

// applyAppearance sets the light or dark appearance; "" follows the
// system.
func applyAppearance(a string) {
	switch a {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
}

// title is the title of an action.
func title(id string) string {
	a, _ := actions.Find(id)
	return a.Title
}

// keys is the shortcut of an action as the menus show it, "" for none.
func keys(id string) string {
	a, _ := actions.Find(id)
	return actions.Glyphs(a.Keys)
}

// tip is a tooltip: what a button does, with the shortcut of its action.
func tip(label, id string) string {
	if k := keys(id); k != "" {
		return label + " (" + k + ")"
	}
	return label
}

// item is a menu bar item that runs an action.
func (d *desktop) item(a actions.Action) *mygo.MenuItem {
	id := a.ID
	return &mygo.MenuItem{
		Label: a.Title, Accelerator: a.Keys,
		Click: func(*mygo.MenuItem, *mygo.Window) { d.update(func() { d.run(id) }) },
	}
}

func (d *desktop) items(menu string) []*mygo.MenuItem {
	var out []*mygo.MenuItem
	for _, a := range actions.InMenu(menu) {
		out = append(out, d.item(a))
	}
	return out
}

// menu is the menu bar. The app, Go and View menus come from the action
// list; Edit and Window are the system's.
func (d *desktop) menu() *mygo.Menu {
	app := append([]*mygo.MenuItem{{Role: mygo.RoleAbout}, mygo.Separator()}, d.items(actions.MenuApp)...)
	app = append(app, mygo.Separator(),
		&mygo.MenuItem{Role: mygo.RoleHide}, &mygo.MenuItem{Role: mygo.RoleHideOthers}, &mygo.MenuItem{Role: mygo.RoleUnhide},
		mygo.Separator(), &mygo.MenuItem{Role: mygo.RoleQuit})
	view := append(d.items(actions.MenuView), mygo.Separator(), &mygo.MenuItem{Role: mygo.RoleToggleFullScreen})
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu, Submenu: app},
		{Role: mygo.RoleEditMenu},
		{Label: actions.MenuGo, Submenu: d.items(actions.MenuGo)},
		{Label: actions.MenuView, Submenu: view},
		{Role: mygo.RoleWindowMenu},
	})
}
