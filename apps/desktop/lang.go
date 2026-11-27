package main

import (
	"errors"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"

	"github.com/austiecodes/solax/apps/desktop/internal/profile"
	"github.com/austiecodes/solax/apps/desktop/locales"
	"github.com/austiecodes/solax/pkg/protocol"
)

// The app says its words in the language it shows: English, its own, or
// Simplified Chinese. They are the messages of locales/<tag>.json; "go run
// github.com/egoist/mygo/cmd/mygo generate" writes their functions
// (locales/catalog_gen.go). The app follows the languages of the system
// unless Settings › Look chose one; MyGo keeps that choice.

// actionTitles are the titles of the actions (internal/actions), which
// the menu bar, the tooltips and the palette show.
var actionTitles = map[string]func() string{
	"settings.open":  locales.ActionSettings,
	"palette.open":   locales.ActionGoTo,
	"nav.chat":       locales.NavChat,
	"nav.tasks":      locales.NavTasks,
	"nav.workflows":  locales.NavWorkflows,
	"nav.agents":     locales.NavAgents,
	"nav.projects":   locales.NavProjects,
	"nav.machines":   locales.NavMachines,
	"sidebar.toggle": locales.ActionToggleSidebar,
	"theme.toggle":   locales.ActionToggleDark,
	"data.reload":    locales.ActionReload,
}

// menuTitle is the title of a menu of the menu bar.
func menuTitle(menu string) string {
	switch menu {
	case "Go":
		return locales.MenuGo()
	case "View":
		return locales.MenuView()
	}
	return menu
}

// stateWord says a task, workflow or machine state, or the state of the
// stream.
func stateWord(state string) string {
	switch state {
	case "queued":
		return locales.StateQueued()
	case "running":
		return locales.StateRunning()
	case "waiting":
		return locales.StateWaiting()
	case "done":
		return locales.StateDone()
	case "failed":
		return locales.StateFailed()
	case "cancelled":
		return locales.StateCancelled()
	case string(protocol.MachinePending):
		return locales.StatePending()
	case string(protocol.MachineApproved):
		return locales.StateApproved()
	case string(protocol.MachineRevoked):
		return locales.StateRevoked()
	case "connecting":
		return locales.ConnConnecting()
	case "connected":
		return locales.ConnConnected()
	case "disconnected":
		return locales.ConnDisconnected()
	}
	return state
}

// errText says an error of the app in the language shown. The errors of
// the server come as the server says them.
func errText(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, profile.ErrNoProfile):
		return locales.ErrNoProfile()
	}
	return err.Error()
}

// The Language choices: the system's, or one of the app's languages,
// each named in itself.
const langSystem = ""

func languages() []string { return []string{langSystem, "en", "zh-Hans"} }

// languageLabel is the Settings label of a language choice.
func languageLabel(tag string) string {
	if tag == langSystem {
		return locales.LanguageSystem()
	}
	return i18n.Autonym(tag)
}

// setLanguage shows the app in a language, "" for the system's. MyGo
// keeps it for the next launches.
func (d *desktop) setLanguage(tag string) {
	d.lang = tag
	if d.win != nil {
		mygo.App.SetLocale(tag)
	}
}

// localeChanged makes again what the app said once: the menu bar.
func (d *desktop) localeChanged() {
	mygo.App.SetMenu(d.menu())
}
