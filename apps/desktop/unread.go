package main

import (
	"slices"
	"time"

	"github.com/egoist/mygo"

	"github.com/austiecodes/solax/apps/desktop/locales"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Unread tasks, as kero's unread sessions: a task that ends while the user
// does not look at it. The sidebar shows it bold, the footer counts it, a
// notification tells of it while the window is not in front, and the Dock
// badge counts the failed ones. The list is saved with the prefs, so it
// survives a restart; tasks that ended while the app was closed become
// unread on the first load.

// notifier tells the system about tasks: notifications and the Dock badge.
// Tests use a fake.
type notifier interface {
	notify(taskID, title, body string)
	badge(n int)
}

// noNotifier does nothing: the default until main sets the system one.
type noNotifier struct{}

func (noNotifier) notify(string, string, string) {}
func (noNotifier) badge(int)                     {}

// systemNotifier shows macOS notifications. A click opens the task.
type systemNotifier struct{ d *desktop }

func (s systemNotifier) notify(taskID, title, body string) {
	if !mygo.NotificationsSupported() {
		return
	}
	n := mygo.NewNotification(mygo.NotificationOptions{ID: "task/" + taskID, Group: "tasks", Title: title, Body: body})
	n.OnClick(func() {
		s.d.update(func() {
			s.d.openTask(taskID)
			s.d.showWindow()
		})
		n.Close()
	})
	go n.Show() // Show asks for permission the first time, and waits
}

func (systemNotifier) badge(n int) { mygo.App.SetBadgeCount(n) }

// taskEnded handles a task that came to an end, from a push or a reload:
// unread unless the user looks at it, and a notification while the window
// is not in front. Cancelled tasks notify nobody: someone chose that.
func (d *desktop) taskEnded(j protocol.Task, notify bool) {
	if d.watching(j.ID) {
		return
	}
	d.markUnread(j.ID)
	if !notify || d.focused || j.State == "cancelled" {
		return
	}
	title, body := locales.NotifyDone(j.Agent), j.Title
	if j.State == "failed" {
		title = locales.NotifyFailed(j.Agent)
		if j.Error != "" {
			body += ": " + clip(j.Error, 140)
		}
	}
	d.notes.notify(j.ID, title, body)
}

func (d *desktop) markUnread(id string) {
	if !d.unread[id] {
		d.unread[id] = true
		d.unreadChanged()
	}
}

func (d *desktop) markRead(id string) {
	if d.unread[id] {
		delete(d.unread, id)
		d.unreadChanged()
	}
}

// unreadChanged saves the list and sets the badge.
func (d *desktop) unreadChanged() {
	d.notes.badge(d.countTasks(func(j protocol.Task) bool { return j.State == "failed" && d.unread[j.ID] }))
	d.savePrefs()
}

// setTasks takes the tasks of a full reload. On the first load it marks
// what ended since the app last ran; later (after a reconnect) what ended
// while the stream was down. Unread IDs of tasks gone from the list go.
func (d *desktop) setTasks(tasks []protocol.Task) {
	old := map[string]string{}
	for _, j := range d.tasks {
		old[j.ID] = j.State
	}
	first := !d.loaded
	d.tasks, d.loaded = tasks, true
	for _, j := range tasks {
		if taskLive(j.State) {
			continue
		}
		if first {
			if seen := d.prefs.SeenAt; !seen.IsZero() && j.UpdatedAt.After(seen) {
				d.taskEnded(j, false)
			}
			continue
		}
		if prev, ok := old[j.ID]; !ok || taskLive(prev) {
			d.taskEnded(j, true)
		}
	}
	gone := false
	for id := range d.unread {
		if !slices.ContainsFunc(tasks, func(j protocol.Task) bool { return j.ID == id }) {
			delete(d.unread, id)
			gone = true
		}
	}
	if gone || first {
		d.unreadChanged()
	}
}

// unreadIDs is the unread list to save, sorted so the file is stable.
func (d *desktop) unreadIDs() []string {
	ids := make([]string, 0, len(d.unread))
	for id := range d.unread {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// seenNow is the time saved as SeenAt: now once the tasks are loaded;
// before that the old time stays, or tasks that ended while the app was
// closed would never be marked.
func (d *desktop) seenNow() time.Time {
	if d.loaded {
		return time.Now()
	}
	return d.prefs.SeenAt
}
