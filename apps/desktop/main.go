// Command desktop is the Solax desktop client for macOS: the conversation
// with the Super Agent, the jobs it gives to agents with their live
// events, and the setup of agents and machines. The UI is native Go
// (mygo/ui), with no webview.
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/internal/profile"
)

// keychainService names the keychain items that hold the client tokens.
const keychainService = "dev.austiecodes.solax"

func main() {
	path, err := profile.DefaultPath()
	if err != nil {
		log.Fatal(err)
	}
	profiles, err := profile.Open(path, profile.Keychain{Service: keychainService})
	if err != nil {
		log.Fatal(err)
	}
	d := newDesktop(profiles)
	mygo.App.WhenReady(func() {
		d.win = mygo.NewWindow(mygo.WindowOptions{
			Title: "Solax", Width: 1240, Height: 820, MinWidth: 820, MinHeight: 520,
			StateKey: "main", Content: ui.View(d.view),
		})
		go d.connect()
	})
	defer d.stop()
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
