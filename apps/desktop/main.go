// Command desktop is the Solax desktop client. It is the entry point for
// the user: the conversation with the Super Agent, the workspaces and the
// machines.
package main

import (
	"context"
	"github.com/austiecodes/solax/pkg/protocol"
	"github.com/egoist/mygo/ui"
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

	d := &desktop{a: a, page: "Settings", name: "default", connection: Connection{State: Disconnected}}
	mygo.App.WhenReady(func() {
		d.win = mygo.NewWindow(mygo.WindowOptions{Title: "Solax", Width: 1200, Height: 780, MinWidth: 720, MinHeight: 480, StateKey: "main", Content: ui.View(d.view)})
		ConnectionChanged.listen(func(v Connection) { d.win.Update(func() { d.connection = v }) })
		MessageCreated.listen(func(v protocol.Message) {
			d.win.Update(func() {
				if d.page == "Workspaces" {
					d.appendMessage(v)
				}
			})
		})
		SuperReplied.listen(func(v protocol.Message) {
			d.win.Update(func() {
				if d.page == "Super" {
					d.appendMessage(v)
				}
			})
		})
		MachineUpdated.listen(func(v protocol.Machine) { d.win.Update(func() { d.upsertMachine(v) }) })
		d.run(func(ctx context.Context) (func(), error) {
			ps := a.profiles.List()
			err := a.connect()
			return func() {
				d.profiles = ps
				if err != nil {
					d.status = err.Error()
				}
			}, nil
		})
	})
	defer a.stream.Stop()

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
