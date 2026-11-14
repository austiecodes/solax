// Package actions names what the desktop app does. One list feeds the
// menu bar, the shortcuts, the tooltips and, later, the command palette,
// so an action is added in one place.
package actions

import (
	"runtime"
	"strings"
)

// Action is one thing the app does.
type Action struct {
	ID    string
	Title string
	Menu  string // the menu bar menu that shows it
	Keys  string // the default accelerator, as "CmdOrCtrl+Shift+L"; "" for none
}

// Menus, in the order of the menu bar.
const (
	MenuApp  = "Solax"
	MenuGo   = "Go"
	MenuView = "View"
)

// All are the actions, in menu order.
var All = []Action{
	{ID: "settings.open", Title: "Settings…", Menu: MenuApp, Keys: "CmdOrCtrl+,"},

	{ID: "palette.open", Title: "Go to…", Menu: MenuGo, Keys: "CmdOrCtrl+K"},
	{ID: "nav.chat", Title: "Super Agent", Menu: MenuGo, Keys: "CmdOrCtrl+1"},
	{ID: "nav.jobs", Title: "Jobs", Menu: MenuGo, Keys: "CmdOrCtrl+2"},
	{ID: "nav.agents", Title: "Agents", Menu: MenuGo, Keys: "CmdOrCtrl+3"},
	{ID: "nav.machines", Title: "Machines", Menu: MenuGo, Keys: "CmdOrCtrl+4"},

	{ID: "sidebar.toggle", Title: "Toggle Sidebar", Menu: MenuView, Keys: "CmdOrCtrl+B"},
	{ID: "theme.toggle", Title: "Toggle Dark Mode", Menu: MenuView, Keys: "CmdOrCtrl+Shift+L"},
	{ID: "data.reload", Title: "Reload", Menu: MenuView, Keys: "CmdOrCtrl+R"},
}

// Find returns an action by its ID.
func Find(id string) (Action, bool) {
	for _, a := range All {
		if a.ID == id {
			return a, true
		}
	}
	return Action{}, false
}

// InMenu returns the actions of a menu, in order.
func InMenu(menu string) []Action {
	var out []Action
	for _, a := range All {
		if a.Menu == menu {
			out = append(out, a)
		}
	}
	return out
}

// Glyphs writes an accelerator as the menus show it: "⇧⌘L" on macOS,
// "Ctrl+Shift+L" on other systems.
func Glyphs(accel string) string {
	return glyphs(accel, runtime.GOOS == "darwin")
}

func glyphs(accel string, mac bool) string {
	if accel == "" {
		return ""
	}
	parts := strings.Split(accel, "+")
	key := parts[len(parts)-1]
	if key == "" && len(parts) > 1 { // "CmdOrCtrl++"
		key = "+"
	}
	var ctrl, alt, shift, cmd bool
	for _, m := range parts[:len(parts)-1] {
		switch strings.ToLower(m) {
		case "cmdorctrl", "commandorcontrol":
			if mac {
				cmd = true
			} else {
				ctrl = true
			}
		case "cmd", "command", "super", "meta":
			cmd = true
		case "ctrl", "control":
			ctrl = true
		case "alt", "option":
			alt = true
		case "shift":
			shift = true
		}
	}
	key = keyName(key, mac)
	if !mac {
		var out []string
		for _, m := range []struct {
			on   bool
			name string
		}{{cmd, "Super"}, {ctrl, "Ctrl"}, {alt, "Alt"}, {shift, "Shift"}} {
			if m.on {
				out = append(out, m.name)
			}
		}
		return strings.Join(append(out, key), "+")
	}
	var b strings.Builder
	for _, m := range []struct {
		on    bool
		glyph string
	}{{ctrl, "⌃"}, {alt, "⌥"}, {shift, "⇧"}, {cmd, "⌘"}} {
		if m.on {
			b.WriteString(m.glyph)
		}
	}
	b.WriteString(key)
	return b.String()
}

func keyName(key string, mac bool) string {
	macNames := map[string]string{
		"enter": "↩", "return": "↩", "tab": "⇥", "space": "Space", "backspace": "⌫", "delete": "⌦",
		"escape": "⎋", "esc": "⎋", "up": "↑", "down": "↓", "left": "←", "right": "→",
	}
	pcNames := map[string]string{
		"enter": "Enter", "return": "Enter", "tab": "Tab", "space": "Space", "backspace": "Backspace", "delete": "Del",
		"escape": "Esc", "esc": "Esc", "up": "↑", "down": "↓", "left": "←", "right": "→",
	}
	names := pcNames
	if mac {
		names = macNames
	}
	if n, ok := names[strings.ToLower(key)]; ok {
		return n
	}
	return strings.ToUpper(key)
}
