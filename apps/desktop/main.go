// Command desktop is the Solax desktop client. It is the entry point for
// the user: the conversation with the Super Agent, the workspaces and the
// machines.
package main

import (
	"log"

	"github.com/egoist/mygo"

	"github.com/austiecodes/solax/apps/desktop/internal/profile"
)

func main() {
	path, err := profile.DefaultPath()
	if err != nil {
		log.Fatal(err)
	}
	profiles, err := profile.Open(path, profile.Keychain{Service: "dev.austiecodes.solax"})
	if err != nil {
		log.Fatal(err)
	}
	a := &app{profiles: profiles, stream: &stream{}}

	mygo.Bind(Profiles{a})
	mygo.Bind(Super{a})
	mygo.Bind(Workspaces{a})
	mygo.Bind(Tasks{a})
	mygo.Bind(Machines{a})

	// Setup that must not run during `mygo generate` goes in WhenReady.
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:           "Solax",
			Width:           1200,
			Height:          780,
			MinWidth:        720,
			MinHeight:       480,
			BackgroundColor: "light-dark(#f6f7f9, #0f1115)", // --bg in src/style.css
			StateKey:        "main",
			URL:             "/",
		})
		if err := a.connect(); err != nil {
			ConnectionChanged.Broadcast(Connection{State: Disconnected, Error: err.Error()})
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
