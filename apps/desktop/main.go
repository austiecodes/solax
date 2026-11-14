// Command desktop is the Solax desktop client for macOS: the conversation
// with the Super Agent, the jobs it gives to agents with their live
// events, and the setup of agents and machines. The UI is native Go
// (mygo/ui), with no webview.
package main

import (
	"log"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/internal/profile"
)

// keychainService names the keychain items that hold the client tokens.
const keychainService = "dev.austiecodes.solax"

func main() {
	// A second launch shows the window of the first.
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	path, err := profile.DefaultPath()
	if err != nil {
		log.Fatal(err)
	}
	profiles, err := profile.Open(path, profile.Keychain{Service: keychainService})
	if err != nil {
		log.Fatal(err)
	}
	d := newDesktop(profiles, filepath.Join(filepath.Dir(path), "app.json"))
	d.notes = systemNotifier{d}
	applyAppearance(d.prefs.Appearance)
	mygo.App.SetMenu(d.menu())
	mygo.App.OnSecondInstance(func([]string, string) { d.showWindow() })
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { d.savePrefs() })
	// Back in the app, the notifications have done their work: the
	// sidebar shows the same jobs as unread.
	mygo.App.OnDidBecomeActive(mygo.ClearNotifications)
	mygo.App.WhenReady(func() {
		d.win = mygo.NewWindow(framed(mygo.WindowOptions{
			Title: "Solax", Width: 1240, Height: 820, MinWidth: 720, MinHeight: 480,
			StateKey: "main", Vibrancy: mygo.VibrancySidebar, Content: ui.View(d.view),
		}))
		d.win.OnFocus(func() { d.update(func() { d.setFocused(true) }) })
		d.win.OnBlur(func() { d.update(func() { d.setFocused(false) }) })
		go d.connect()
	})
	defer d.stop()
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// framed hides the title bar and keeps the traffic lights, centered on
// barMid, the line the sidebar buttons and page headers center on.
func framed(o mygo.WindowOptions) mygo.WindowOptions {
	o.TitleBarStyle = mygo.TitleBarHiddenInset
	o.TitleBarHeight = topBar
	// The lights are 16 tall, and mygo puts them a DIP above the position.
	o.TrafficLightPosition = &mygo.Point{X: 18, Y: barMid - 7}
	return o
}

func (d *desktop) showWindow() {
	if d.win != nil {
		d.win.Show()
		d.win.Focus()
	}
}

// setFocused tracks whether the window is in front. Coming back reads the
// job in view, which may have ended while the user was away.
func (d *desktop) setFocused(f bool) {
	d.focused = f
	if f && d.page == pageJobs {
		d.markRead(d.jobSel)
	}
}
