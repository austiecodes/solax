package main

import (
	"slices"
	"time"

	"github.com/egoist/mygo"

	"github.com/austiecodes/solax/pkg/protocol"
)

// Unread jobs, as kero's unread sessions: a job that ends while the user
// does not look at it. The sidebar shows it bold, the footer counts it, a
// notification tells of it while the window is not in front, and the Dock
// badge counts the failed ones. The list is saved with the prefs, so it
// survives a restart; jobs that ended while the app was closed become
// unread on the first load.

// notifier tells the system about jobs: notifications and the Dock badge.
// Tests use a fake.
type notifier interface {
	notify(jobID, title, body string)
	badge(n int)
}

// noNotifier does nothing: the default until main sets the system one.
type noNotifier struct{}

func (noNotifier) notify(string, string, string) {}
func (noNotifier) badge(int)                     {}

// systemNotifier shows macOS notifications. A click opens the job.
type systemNotifier struct{ d *desktop }

func (s systemNotifier) notify(jobID, title, body string) {
	if !mygo.NotificationsSupported() {
		return
	}
	n := mygo.NewNotification(mygo.NotificationOptions{ID: "job/" + jobID, Group: "jobs", Title: title, Body: body})
	n.OnClick(func() {
		s.d.update(func() {
			s.d.openJob(jobID)
			s.d.showWindow()
		})
		n.Close()
	})
	go n.Show() // Show asks for permission the first time, and waits
}

func (systemNotifier) badge(n int) { mygo.App.SetBadgeCount(n) }

// jobEnded handles a job that came to an end, from a push or a reload:
// unread unless the user looks at it, and a notification while the window
// is not in front. Cancelled jobs notify nobody: someone chose that.
func (d *desktop) jobEnded(j protocol.Job, notify bool) {
	if d.watching(j.ID) {
		return
	}
	d.markUnread(j.ID)
	if !notify || d.focused || j.State == "cancelled" {
		return
	}
	title, body := j.Agent+" finished", j.Title
	if j.State == "failed" {
		title = j.Agent + " failed"
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
	d.notes.badge(d.countJobs(func(j protocol.Job) bool { return j.State == "failed" && d.unread[j.ID] }))
	d.savePrefs()
}

// setJobs takes the jobs of a full reload. On the first load it marks
// what ended since the app last ran; later (after a reconnect) what ended
// while the stream was down. Unread IDs of jobs gone from the list go.
func (d *desktop) setJobs(jobs []protocol.Job) {
	old := map[string]string{}
	for _, j := range d.jobs {
		old[j.ID] = j.State
	}
	first := !d.loaded
	d.jobs, d.loaded = jobs, true
	for _, j := range jobs {
		if jobLive(j.State) {
			continue
		}
		if first {
			if seen := d.prefs.SeenAt; !seen.IsZero() && j.UpdatedAt.After(seen) {
				d.jobEnded(j, false)
			}
			continue
		}
		if prev, ok := old[j.ID]; !ok || jobLive(prev) {
			d.jobEnded(j, true)
		}
	}
	gone := false
	for id := range d.unread {
		if !slices.ContainsFunc(jobs, func(j protocol.Job) bool { return j.ID == id }) {
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

// seenNow is the time saved as SeenAt: now once the jobs are loaded;
// before that the old time stays, or jobs that ended while the app was
// closed would never be marked.
func (d *desktop) seenNow() time.Time {
	if d.loaded {
		return time.Now()
	}
	return d.prefs.SeenAt
}
