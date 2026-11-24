package main

import (
	"errors"
	"testing"
	"time"

	"github.com/austiecodes/solax/pkg/protocol"
)

// Pushes that come while a reload reads are applied after its snapshot,
// so a snapshot older than a push cannot hide it. A stale reload is
// ignored, and a failed reload still applies the held pushes.
func TestReloadKeepsPushes(t *testing.T) {
	d := testDesktop(t)
	now := time.Now()
	d.syncGen, d.syncing = 1, true

	// While the reload reads: the task ends and a reply comes.
	d.push(push(t, "task", protocol.Task{ID: "t1", State: "running", UpdatedAt: now}))
	d.push(push(t, "task", protocol.Task{ID: "t1", State: "done", UpdatedAt: now.Add(2 * time.Second)}))
	d.push(push(t, "super.message", protocol.Message{ID: "m3", Seq: 3, Text: "Done."}))
	if len(d.tasks) != 0 || len(d.messages) != 0 {
		t.Fatalf("pushes applied during the reload: %v %v", d.tasks, d.messages)
	}

	// The snapshot was read before the task ended.
	d.synced(1, snapshot{
		tasks:    []protocol.Task{{ID: "t1", State: "running", UpdatedAt: now.Add(time.Second)}},
		messages: []protocol.Message{{ID: "m1", Seq: 1}, {ID: "m2", Seq: 2}},
	}, nil)
	if j, _ := d.task("t1"); j.State != "done" {
		t.Fatalf("task after reload = %q, want done", j.State)
	}
	if len(d.messages) != 3 || d.messages[2].ID != "m3" {
		t.Fatalf("messages after reload = %v", d.messages)
	}
	if d.syncing || d.held != nil {
		t.Fatal("still holding pushes after the reload")
	}

	// A late snapshot of an older reload changes nothing.
	d.synced(0, snapshot{}, nil)
	if len(d.tasks) != 1 || len(d.messages) != 3 {
		t.Fatalf("stale snapshot applied: %v %v", d.tasks, d.messages)
	}

	// A failed reload keeps the state and applies what it held.
	d.syncGen, d.syncing = 2, true
	d.push(push(t, "super.message", protocol.Message{ID: "m4", Seq: 4}))
	d.synced(2, snapshot{}, errors.New("offline"))
	if len(d.messages) != 4 || d.syncing || d.status == "" {
		t.Fatalf("after a failed reload: messages %d syncing %v status %q", len(d.messages), d.syncing, d.status)
	}
}

// A task push older than the task in hand does not replace it.
func TestOlderTaskPushIgnored(t *testing.T) {
	d := testDesktop(t)
	now := time.Now()
	d.apply(push(t, "task", protocol.Task{ID: "t1", State: "done", UpdatedAt: now}))
	d.apply(push(t, "task", protocol.Task{ID: "t1", State: "running", UpdatedAt: now.Add(-time.Second)}))
	if j, _ := d.task("t1"); j.State != "done" {
		t.Fatalf("task = %q, want done", j.State)
	}
}
