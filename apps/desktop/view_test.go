package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/internal/profile"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

type memSecrets map[string]string

func (m memSecrets) Get(p string) (string, error) { return m[p], nil }
func (m memSecrets) Set(p, t string) error        { m[p] = t; return nil }
func (m memSecrets) Delete(p string) error        { delete(m, p); return nil }

func testDesktop(t *testing.T) *desktop {
	t.Helper()
	st, err := profile.Open(t.TempDir()+"/profiles.json", memSecrets{})
	if err != nil {
		t.Fatal(err)
	}
	return newDesktop(st, "")
}

func push(t *testing.T, event string, v any) protocol.Push {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.Push{Event: event, Payload: b}
}

// Every page renders without a server, and a click on an empty form
// starts no request.
func TestPagesRender(t *testing.T) {
	for _, page := range []string{pageChat, pageJobs, pageAgents, pageMachines, pageSettings} {
		t.Run(page, func(t *testing.T) {
			d := testDesktop(t)
			d.page = page
			tt := ui.NewTester(d.view, 1240, 820)
			if !tt.HasText("Solax") || !tt.HasText("Machines") {
				t.Fatalf("page did not render: %q", tt.Texts())
			}
		})
	}
}

// Pushes from the stream build the conversation, the jobs and the live
// state of the Super Agent; the jobs page shows the events of a job.
func TestStreamPushes(t *testing.T) {
	d := testDesktop(t)
	now := time.Now()
	d.apply(push(t, "super.message", protocol.Message{ID: "m2", Seq: 2, Author: protocol.Author{Kind: "super"}, Text: "On it."}))
	d.apply(push(t, "super.message", protocol.Message{ID: "m1", Seq: 1, Author: protocol.Author{Kind: "human"}, Text: "Fix the bug"}))
	d.apply(push(t, "super.message", protocol.Message{ID: "m1", Seq: 1, Author: protocol.Author{Kind: "human"}, Text: "Fix the bug"}))
	if len(d.messages) != 2 || d.messages[0].ID != "m1" {
		t.Fatalf("messages = %+v", d.messages)
	}

	d.apply(push(t, "session.event", protocol.SessionEventNote{Super: true, Event: ir.Event{Type: ir.TurnStarted}}))
	if !d.superBusy {
		t.Fatal("Super Agent turn did not show as busy")
	}

	job := protocol.Job{ID: "job_1", Agent: "coder", SessionID: "ses_1", State: "running", Title: "Fix the bug", Prompt: "Fix the bug", CreatedAt: now}
	d.apply(push(t, "job", job))
	tool := &ir.ToolCall{Name: "Bash", Class: ir.ToolShell, Input: json.RawMessage(`{"command":"go test ./..."}`), Output: "ok"}
	for _, e := range []ir.Event{
		{Seq: 1, Type: ir.TurnStarted, Turn: &ir.Turn{Cause: ir.CauseInput}},
		{Seq: 2, Type: ir.ItemCompleted, Item: &ir.Item{ID: "t1", Kind: ir.ItemToolCall, Status: ir.StatusCompleted, Tool: tool}},
		{Seq: 0, Type: ir.ItemDelta, Delta: &ir.Delta{ItemID: "a1", Kind: "text", Text: "Fixed"}},
	} {
		d.apply(push(t, "session.event", protocol.SessionEventNote{SessionID: "ses_1", JobID: "job_1", Agent: "coder", Event: e}))
	}
	if len(d.events["ses_1"]) != 2 || d.live["ses_1"]["a1"].text != "Fixed" {
		t.Fatalf("events = %+v live = %+v", d.events["ses_1"], d.live["ses_1"])
	}

	d.page, d.jobSel = pageJobs, "job_1"
	tt := ui.NewTester(d.view, 1240, 820)
	// The tool call is done and text follows it, so its group is closed.
	for _, want := range []string{"coder", "Fix the bug", "1 tool used", "Fixed"} {
		if !tt.HasText(want) {
			t.Fatalf("jobs page lacks %q: %q", want, tt.Texts())
		}
	}
	if tt.HasText("go test ./...") {
		t.Fatal("a closed group shows its steps")
	}
	tt.Click("1 tool used")
	tt.Advance(time.Second)
	if !tt.HasText("go test ./...") || !tt.HasText("Ran") {
		t.Fatalf("an open group lacks its steps: %q", tt.Texts())
	}

	job.State, job.Result = "done", "All tests pass."
	d.apply(push(t, "job", job))
	if d.countJobs(func(j protocol.Job) bool { return jobLive(j.State) }) != 0 || len(d.jobs) != 1 {
		t.Fatalf("jobs = %+v", d.jobs)
	}
}

func TestJobOf(t *testing.T) {
	if got := jobOf("[job job_9] agent coder: done"); got != "job_9" {
		t.Fatalf("jobOf = %q", got)
	}
	if jobOf("hello") != "" {
		t.Fatal("jobOf of a plain text")
	}
}

// Each nav action opens its page; the sidebar toggles; settings opens.
func TestActions(t *testing.T) {
	d := testDesktop(t)
	for _, p := range navPages {
		d.run(p.action)
		if d.page != p.id {
			t.Fatalf("%s opened %q", p.action, d.page)
		}
	}
	d.run("settings.open")
	if d.page != pageSettings {
		t.Fatalf("settings.open opened %q", d.page)
	}
	d.run("sidebar.toggle")
	if !d.prefs.SidebarHidden {
		t.Fatal("sidebar did not hide")
	}
	// Hidden, the page header offers to show it again.
	tt := ui.NewTester(d.view, 1240, 820)
	tt.Advance(time.Second)
	if !tt.HasText("Server URL") {
		t.Fatalf("settings page not shown: %q", tt.Texts())
	}
	if tt.HasText("Machines") {
		t.Fatal("sidebar still shows after it hid")
	}
}

// Prefs survive a restart: the page, the job, the sidebar.
func TestPrefsRoundTrip(t *testing.T) {
	st, err := profile.Open(t.TempDir()+"/profiles.json", memSecrets{})
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/app.json"
	d := newDesktop(st, path)
	if d.page != pageChat || d.prefs.SidebarWidth != defaultSidebar {
		t.Fatalf("defaults: page %q width %v", d.page, d.prefs.SidebarWidth)
	}
	d.page, d.jobSel, d.prefs.SidebarWidth, d.prefs.SidebarHidden = pageJobs, "job_7", 300, true
	d.savePrefs()

	d2 := newDesktop(st, path)
	if d2.page != pageJobs || d2.jobSel != "job_7" || d2.prefs.SidebarWidth != 300 || !d2.prefs.SidebarHidden {
		t.Fatalf("restored %+v page %q job %q", d2.prefs, d2.page, d2.jobSel)
	}
}

// A job that ends while the user looks elsewhere is unread: bold in the
// sidebar, counted in the footer. Opening it reads it; the footer chip
// jumps to the next unread one.
func TestUnreadJobs(t *testing.T) {
	d := testDesktop(t)
	d.conn.State = "connected"
	now := time.Now()
	a := protocol.Job{ID: "job_a", Agent: "coder", State: "running", Title: "Job A", CreatedAt: now}
	b := protocol.Job{ID: "job_b", Agent: "coder", State: "running", Title: "Job B", CreatedAt: now}
	d.apply(push(t, "job", a))
	d.apply(push(t, "job", b))
	d.openJob("job_a") // the user watches A

	a.State, b.State = "done", "failed"
	d.apply(push(t, "job", a))
	d.apply(push(t, "job", b))
	if d.unread["job_a"] || !d.unread["job_b"] {
		t.Fatalf("unread = %v, want only job_b", d.unread)
	}
	tt := ui.NewTester(d.view, 1240, 820)
	if !tt.HasText("Job B") || !tt.HasText("1") {
		t.Fatalf("sidebar lacks the unread job: %q", tt.Texts())
	}
	if err := tt.Click("1 failed"); err != nil {
		t.Fatal(err)
	}
	if d.jobSel != "job_b" || d.unread["job_b"] {
		t.Fatalf("chip opened %q, unread %v", d.jobSel, d.unread)
	}
}

// The Jobs page without an open job lists all jobs; nav.jobs goes there.
func TestJobsOverview(t *testing.T) {
	d := testDesktop(t)
	d.apply(push(t, "job", protocol.Job{ID: "job_1", Agent: "coder", State: "queued", Title: "Write docs", CreatedAt: time.Now()}))
	d.openJob("job_1")
	d.run("nav.jobs")
	if d.jobSel != "" || d.page != pageJobs {
		t.Fatalf("nav.jobs: page %q job %q", d.page, d.jobSel)
	}
	tt := ui.NewTester(d.view, 1240, 820)
	if !tt.HasText("1 job, 1 running") {
		t.Fatalf("overview not shown: %q", tt.Texts())
	}
}

// The palette finds jobs and actions by fuzzy letters; Return runs the
// first match, and the arrows move the highlight.
func TestPalette(t *testing.T) {
	d := testDesktop(t)
	now := time.Now()
	d.apply(push(t, "job", protocol.Job{ID: "job_1", Agent: "coder", State: "running", Title: "Add a README", CreatedAt: now}))
	d.apply(push(t, "job", protocol.Job{ID: "job_2", Agent: "tester", State: "done", Title: "Run tests", CreatedAt: now}))

	got := matches(d.paletteItems(), "rdme")
	if len(got) == 0 || got[0].title != "Add a README" {
		t.Fatalf("rdme matched %+v", got)
	}
	if got := matches(d.paletteItems(), "machines"); len(got) == 0 || got[0].title != "Machines" {
		t.Fatalf("machines matched %+v", got)
	}
	if got := matches(d.paletteItems(), "zzzq"); len(got) != 0 {
		t.Fatalf("zzzq matched %+v", got)
	}

	d.run("palette.open")
	tt := ui.NewTester(d.view, 1240, 820)
	if !tt.HasText("Jobs") || !tt.HasText("Actions") {
		t.Fatalf("palette groups missing: %q", tt.Texts())
	}
	tt.Type("run tests")
	tt.Key(0, ui.KeyEnter)
	if d.pal.open || d.page != pageJobs || d.jobSel != "job_2" {
		t.Fatalf("palette: open %v page %q job %q", d.pal.open, d.page, d.jobSel)
	}

	d.run("palette.open")
	tt.Frame()
	tt.Type("agents")
	tt.Key(0, ui.KeyEnter)
	if d.page != pageAgents {
		t.Fatalf("palette action opened %q", d.page)
	}
}

// Remove asks first; Cancel leaves the agent, and Escape too.
func TestConfirm(t *testing.T) {
	d := testDesktop(t)
	d.page = pageAgents
	d.agents = []protocol.Agent{{ID: "a1", Name: "coder", Driver: "claude", Machine: "mbp", Dir: "~/src"}}
	tt := ui.NewTester(d.view, 1240, 820)
	if err := tt.Click("Remove"); err != nil {
		t.Fatal(err)
	}
	if d.confirm == nil || !tt.HasText("Remove coder?") {
		t.Fatalf("no question: %q", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if d.confirm != nil {
		t.Fatal("Cancel left the question open")
	}

	ran := false
	d.ask("Sure?", "It cannot be undone.", "Do it", true, func() { ran = true })
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	if !ran || d.confirm != nil {
		t.Fatalf("Return: ran %v confirm %v", ran, d.confirm)
	}
}

type fakeNotes struct {
	shown []string // titles
	count int      // the last badge
}

func (f *fakeNotes) notify(_, title, _ string) { f.shown = append(f.shown, title) }
func (f *fakeNotes) badge(n int)               { f.count = n }

// A job that ends while the window is in the back notifies; one that ends
// while it is in front does not, but is unread all the same. Cancelled
// jobs never notify. The badge counts unread failed jobs.
func TestNotifications(t *testing.T) {
	d := testDesktop(t)
	notes := &fakeNotes{}
	d.notes = notes
	now := time.Now()
	jobs := []protocol.Job{
		{ID: "j1", Agent: "coder", State: "running", Title: "One", CreatedAt: now},
		{ID: "j2", Agent: "tester", State: "running", Title: "Two", CreatedAt: now},
		{ID: "j3", Agent: "coder", State: "running", Title: "Three", CreatedAt: now},
	}
	for _, j := range jobs {
		d.apply(push(t, "job", j))
	}

	jobs[0].State = "done" // in front: no notification
	d.apply(push(t, "job", jobs[0]))
	d.setFocused(false)
	jobs[1].State, jobs[1].Error = "failed", "exit 1"
	d.apply(push(t, "job", jobs[1]))
	jobs[2].State = "cancelled"
	d.apply(push(t, "job", jobs[2]))

	if len(notes.shown) != 1 || notes.shown[0] != "tester failed" {
		t.Fatalf("notifications = %q", notes.shown)
	}
	if !d.unread["j1"] || !d.unread["j2"] || !d.unread["j3"] {
		t.Fatalf("unread = %v", d.unread)
	}
	if notes.count != 1 {
		t.Fatalf("badge = %d, want 1", notes.count)
	}
	d.openJob("j2")
	if notes.count != 0 || d.unread["j2"] {
		t.Fatalf("after reading: badge %d unread %v", notes.count, d.unread)
	}
}

// Unread jobs survive a restart, and a job that ended while the app was
// closed is unread on the first load. A reload after a reconnect marks
// what ended while the stream was down.
func TestUnreadAcrossRestart(t *testing.T) {
	st, err := profile.Open(t.TempDir()+"/profiles.json", memSecrets{})
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/app.json"
	now := time.Now()

	d := newDesktop(st, path)
	d.setJobs([]protocol.Job{
		{ID: "old", State: "done", UpdatedAt: now.Add(-time.Hour)},
		{ID: "a", State: "running", UpdatedAt: now},
	})
	d.markUnread("old")
	d.savePrefs()

	// Restart: "a" ended while the app was closed; "old" is still unread.
	d2 := newDesktop(st, path)
	d2.setJobs([]protocol.Job{
		{ID: "old", State: "done", UpdatedAt: now.Add(-time.Hour)},
		{ID: "a", State: "done", UpdatedAt: time.Now().Add(time.Minute)},
		{ID: "b", State: "running", UpdatedAt: now},
	})
	if !d2.unread["old"] || !d2.unread["a"] || d2.unread["b"] {
		t.Fatalf("after restart unread = %v", d2.unread)
	}

	// Reconnect: "b" ended while the stream was down; "old" left the list.
	d2.setJobs([]protocol.Job{
		{ID: "a", State: "done"},
		{ID: "b", State: "failed"},
	})
	if !d2.unread["b"] || d2.unread["old"] {
		t.Fatalf("after reconnect unread = %v", d2.unread)
	}
}

// Every theme has a light and a dark look; an unknown theme falls back to
// paper; themes differ.
func TestThemes(t *testing.T) {
	for _, name := range themeNames {
		l, ok := looks[name]
		if !ok || l[0].pal.dark || !l[1].pal.dark {
			t.Fatalf("theme %q: %+v", name, l)
		}
	}
	if looks["paper"][0].pal.card == looks["petal"][0].pal.card {
		t.Fatal("paper and petal have one card color")
	}
	if p := loadPrefs(""); p.Theme != defaultTheme {
		t.Fatalf("default theme = %q", p.Theme)
	}
}
