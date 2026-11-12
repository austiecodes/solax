package main

import (
	"github.com/austiecodes/solax/pkg/protocol"
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestNativePages(t *testing.T) {
	for _, page := range []string{"Super", "Workspaces", "Machines", "Settings"} {
		t.Run(page, func(t *testing.T) {
			d := &desktop{page: page, connection: Connection{State: Disconnected}}
			tt := ui.NewTester(d.view, 1200, 780)
			if !tt.HasText("Solax") {
				t.Fatalf("native page did not render: %v", tt.Texts())
			}
			// Empty forms must not start network work or dereference a missing app.
			label := "Send"
			if page == "Machines" {
				label = "Approve"
			}
			if page == "Settings" {
				label = "Save and use"
			}
			_ = tt.Click(label)
			if d.busy {
				t.Fatal("empty form started a request")
			}
		})
	}
}

func TestTaskTimelineFiltersAndDeduplicates(t *testing.T) {
	d := &desktop{page: "Workspaces", ws: protocol.Workspace{ID: "ws"}, task: protocol.Task{ID: "task"}}
	for _, m := range []protocol.Message{
		{ID: "other", WorkspaceID: "ws", TaskID: "other", Seq: 1},
		{ID: "other-ws", WorkspaceID: "other", TaskID: "task", Seq: 1},
		{ID: "one", WorkspaceID: "ws", TaskID: "task", Seq: 1},
		{ID: "one", WorkspaceID: "ws", TaskID: "task", Seq: 1},
		{ID: "two", WorkspaceID: "ws", TaskID: "task", Seq: 2},
	} {
		d.appendMessage(m)
	}
	if len(d.messages) != 2 || d.messages[0].ID != "one" || d.messages[1].ID != "two" {
		t.Fatalf("incorrect timeline: %+v", d.messages)
	}
}
