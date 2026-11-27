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
	for _, page := range []string{pageChat, pageTasks, pageWorkflows, pageAgents, pageMachines, pageSettings} {
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

// Pushes from the stream build the conversation, the tasks and the live
// state of the Super Agent; the tasks page shows the events of a task.
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

	task := protocol.Task{ID: "tsk_1", Agent: "coder", SessionID: "ses_1", State: "running", Title: "Fix the bug", Prompt: "Fix the bug", CreatedAt: now}
	d.apply(push(t, "task", task))
	tool := &ir.ToolCall{Name: "Bash", Class: ir.ToolShell, Input: json.RawMessage(`{"command":"go test ./..."}`), Output: "ok"}
	for _, e := range []ir.Event{
		{Seq: 1, Type: ir.TurnStarted, Turn: &ir.Turn{Cause: ir.CauseInput}},
		{Seq: 2, Type: ir.ItemCompleted, Item: &ir.Item{ID: "t1", Kind: ir.ItemToolCall, Status: ir.StatusCompleted, Tool: tool}},
		{Seq: 0, Type: ir.ItemDelta, Delta: &ir.Delta{ItemID: "a1", Kind: "text", Text: "Fixed"}},
	} {
		d.apply(push(t, "session.event", protocol.SessionEventNote{SessionID: "ses_1", TaskID: "tsk_1", Agent: "coder", Event: e}))
	}
	if len(d.events["ses_1"]) != 2 || d.live["ses_1"]["a1"].text != "Fixed" {
		t.Fatalf("events = %+v live = %+v", d.events["ses_1"], d.live["ses_1"])
	}

	d.page, d.taskSel = pageTasks, "tsk_1"
	tt := ui.NewTester(d.view, 1240, 820)
	// The tool call is done and text follows it, so its group is closed.
	for _, want := range []string{"coder", "Fix the bug", "1 tool used", "Fixed"} {
		if !tt.HasText(want) {
			t.Fatalf("tasks page lacks %q: %q", want, tt.Texts())
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

	task.State, task.Result = "done", "All tests pass."
	d.apply(push(t, "task", task))
	if d.countTasks(func(j protocol.Task) bool { return taskLive(j.State) }) != 0 || len(d.tasks) != 1 {
		t.Fatalf("tasks = %+v", d.tasks)
	}
}

func TestParseEvent(t *testing.T) {
	for _, tc := range []struct {
		text string
		want chatEvent
	}{
		{"[task tsk_9] agent coder: done", chatEvent{kind: "task", id: "tsk_9", name: "coder", state: "done"}},
		{"[task tsk_9] agent coder: cancelled by the user", chatEvent{kind: "task", id: "tsk_9", name: "coder", state: "cancelled"}},
		{"[workflow wfl_1] timeline-demo: done", chatEvent{kind: "workflow", id: "wfl_1", name: "timeline-demo", state: "done"}},
		{"[workflow wfl_1] fix: tests: failed", chatEvent{kind: "workflow", id: "wfl_1", name: "fix: tests", state: "failed"}},
		{"[workflow wfl_1] timeline-demo: asks: Ship it? Yes: or no", chatEvent{kind: "workflow", id: "wfl_1", name: "timeline-demo", state: "waiting", question: "Ship it? Yes: or no"}},
	} {
		if got, ok := parseEvent(tc.text); !ok || got != tc.want {
			t.Errorf("parseEvent(%q) = %+v, %v; want %+v", tc.text, got, ok, tc.want)
		}
	}
	for _, text := range []string{"hello", "[task tsk_9]", "[job j1] agent coder: done", "[task tsk_9] coder done"} {
		if _, ok := parseEvent(text); ok {
			t.Errorf("parseEvent(%q) reads an event", text)
		}
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

// Prefs survive a restart: the page, the task, the sidebar.
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
	d.page, d.taskSel, d.prefs.SidebarWidth, d.prefs.SidebarHidden = pageTasks, "tsk_7", 300, true
	d.savePrefs()

	d2 := newDesktop(st, path)
	if d2.page != pageTasks || d2.taskSel != "tsk_7" || d2.prefs.SidebarWidth != 300 || !d2.prefs.SidebarHidden {
		t.Fatalf("restored %+v page %q task %q", d2.prefs, d2.page, d2.taskSel)
	}
}

// A task that ends while the user looks elsewhere is unread: bold in the
// sidebar, counted in the footer. Opening it reads it; the footer chip
// jumps to the next unread one.
func TestUnreadTasks(t *testing.T) {
	d := testDesktop(t)
	d.conn.State = "connected"
	now := time.Now()
	a := protocol.Task{ID: "tsk_a", Agent: "coder", State: "running", Title: "Task A", CreatedAt: now}
	b := protocol.Task{ID: "tsk_b", Agent: "coder", State: "running", Title: "Task B", CreatedAt: now}
	d.apply(push(t, "task", a))
	d.apply(push(t, "task", b))
	d.openTask("tsk_a") // the user watches A

	a.State, b.State = "done", "failed"
	d.apply(push(t, "task", a))
	d.apply(push(t, "task", b))
	if d.unread["tsk_a"] || !d.unread["tsk_b"] {
		t.Fatalf("unread = %v, want only tsk_b", d.unread)
	}
	tt := ui.NewTester(d.view, 1240, 820)
	if !tt.HasText("Task B") || !tt.HasText("1") {
		t.Fatalf("sidebar lacks the unread task: %q", tt.Texts())
	}
	if err := tt.Click("1 failed"); err != nil {
		t.Fatal(err)
	}
	if d.taskSel != "tsk_b" || d.unread["tsk_b"] {
		t.Fatalf("chip opened %q, unread %v", d.taskSel, d.unread)
	}
}

// The Tasks page without an open task lists all tasks; nav.tasks goes there.
func TestTasksOverview(t *testing.T) {
	d := testDesktop(t)
	d.apply(push(t, "task", protocol.Task{ID: "tsk_1", Agent: "coder", State: "queued", Title: "Write docs", CreatedAt: time.Now()}))
	d.openTask("tsk_1")
	d.run("nav.tasks")
	if d.taskSel != "" || d.page != pageTasks {
		t.Fatalf("nav.tasks: page %q task %q", d.page, d.taskSel)
	}
	tt := ui.NewTester(d.view, 1240, 820)
	if !tt.HasText("1 task, 1 running") {
		t.Fatalf("overview not shown: %q", tt.Texts())
	}
}

// The palette finds tasks and actions by fuzzy letters; Return runs the
// first match, and the arrows move the highlight.
func TestPalette(t *testing.T) {
	d := testDesktop(t)
	now := time.Now()
	d.apply(push(t, "task", protocol.Task{ID: "tsk_1", Agent: "coder", State: "running", Title: "Add a README", CreatedAt: now}))
	d.apply(push(t, "task", protocol.Task{ID: "tsk_2", Agent: "tester", State: "done", Title: "Run tests", CreatedAt: now}))

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
	if !tt.HasText("Tasks") || !tt.HasText("Actions") {
		t.Fatalf("palette groups missing: %q", tt.Texts())
	}
	tt.Type("run tests")
	tt.Key(0, ui.KeyEnter)
	if d.pal.open || d.page != pageTasks || d.taskSel != "tsk_2" {
		t.Fatalf("palette: open %v page %q task %q", d.pal.open, d.page, d.taskSel)
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
	d.agents = []protocol.Agent{{ID: "a1", Name: "coder", Runtime: "claude", Machine: "mbp"}}
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

// A task that ends while the window is in the back notifies; one that ends
// while it is in front does not, but is unread all the same. Cancelled
// tasks never notify. The badge counts unread failed tasks.
func TestNotifications(t *testing.T) {
	d := testDesktop(t)
	notes := &fakeNotes{}
	d.notes = notes
	now := time.Now()
	tasks := []protocol.Task{
		{ID: "j1", Agent: "coder", State: "running", Title: "One", CreatedAt: now},
		{ID: "j2", Agent: "tester", State: "running", Title: "Two", CreatedAt: now},
		{ID: "j3", Agent: "coder", State: "running", Title: "Three", CreatedAt: now},
	}
	for _, j := range tasks {
		d.apply(push(t, "task", j))
	}

	tasks[0].State = "done" // in front: no notification
	d.apply(push(t, "task", tasks[0]))
	d.setFocused(false)
	tasks[1].State, tasks[1].Error = "failed", "exit 1"
	d.apply(push(t, "task", tasks[1]))
	tasks[2].State = "cancelled"
	d.apply(push(t, "task", tasks[2]))

	if len(notes.shown) != 1 || notes.shown[0] != "tester failed" {
		t.Fatalf("notifications = %q", notes.shown)
	}
	if !d.unread["j1"] || !d.unread["j2"] || !d.unread["j3"] {
		t.Fatalf("unread = %v", d.unread)
	}
	if notes.count != 1 {
		t.Fatalf("badge = %d, want 1", notes.count)
	}
	d.openTask("j2")
	if notes.count != 0 || d.unread["j2"] {
		t.Fatalf("after reading: badge %d unread %v", notes.count, d.unread)
	}
}

// Unread tasks survive a restart, and a task that ended while the app was
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
	d.setTasks([]protocol.Task{
		{ID: "old", State: "done", UpdatedAt: now.Add(-time.Hour)},
		{ID: "a", State: "running", UpdatedAt: now},
	})
	d.markUnread("old")
	d.savePrefs()

	// Restart: "a" ended while the app was closed; "old" is still unread.
	d2 := newDesktop(st, path)
	d2.setTasks([]protocol.Task{
		{ID: "old", State: "done", UpdatedAt: now.Add(-time.Hour)},
		{ID: "a", State: "done", UpdatedAt: time.Now().Add(time.Minute)},
		{ID: "b", State: "running", UpdatedAt: now},
	})
	if !d2.unread["old"] || !d2.unread["a"] || d2.unread["b"] {
		t.Fatalf("after restart unread = %v", d2.unread)
	}

	// Reconnect: "b" ended while the stream was down; "old" left the list.
	d2.setTasks([]protocol.Task{
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

// The Agents page shows the first agent until the user chooses one; the
// + button opens the form for a new agent in its place, and the close
// button shows the agent again.
func TestAgentsPage(t *testing.T) {
	d := testDesktop(t)
	d.page = pageAgents
	d.agents = []protocol.Agent{
		{ID: "a1", Name: "coder", Runtime: "pi", Description: "Writes code"},
		{ID: "a2", Name: "solax", Runtime: "pi", Role: "super"},
	}
	tt := ui.NewTester(d.view, 1240, 820)
	if !tt.HasText("Make it Solax") || tt.HasText("Add agent") {
		t.Fatalf("want coder's detail, got %q", tt.Texts())
	}
	if err := tt.Click("New agent"); err != nil {
		t.Fatal(err)
	}
	if !d.form.open || !tt.HasText("Add agent") {
		t.Fatalf("form did not open: %q", tt.Texts())
	}
	if err := tt.Click("Close"); err != nil {
		t.Fatal(err)
	}
	if d.form.open {
		t.Fatal("form did not close")
	}
	if err := tt.Click("solax"); err != nil {
		t.Fatal(err)
	}
	if d.agentSel != "solax" || tt.HasText("Make it Solax") {
		t.Fatalf("solax not shown: sel %q", d.agentSel)
	}
}

// The Projects page shows the first project until the user chooses one.
// Edit opens the form with the values of the project, and the machine of
// a project does not change.
func TestProjectsPage(t *testing.T) {
	d := testDesktop(t)
	d.page = pageProjects
	d.machines = []protocol.Machine{{ID: "mac_1", Name: "mbp", State: protocol.MachineApproved}}
	d.projects = []protocol.Project{
		{ID: "prj_1", Name: "solax", Machine: "mbp", Path: "~/codes/solax", Description: "The server"},
		{ID: "prj_2", Name: "app", Machine: "mbp", Path: "~/src/app"},
	}
	tt := ui.NewTester(d.view, 1240, 820)
	if !tt.HasText("~/codes/solax") || tt.HasText("Add project") {
		t.Fatalf("want the detail of solax, got %q", tt.Texts())
	}
	if err := tt.Click("Edit"); err != nil {
		t.Fatal(err)
	}
	f := d.pform
	if !f.open || f.editing != "prj_1" || f.Name != "solax" || f.Path != "~/codes/solax" || !tt.HasText("Edit project") {
		t.Fatalf("edit form = %+v: %q", f, tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("New project"); err != nil {
		t.Fatal(err)
	}
	if !d.pform.open || d.pform.editing != "" || d.pform.Machine != "mbp" || !tt.HasText("Add project") {
		t.Fatalf("new form = %+v: %q", d.pform, tt.Texts())
	}
	if err := tt.Click("Close"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("app"); err != nil {
		t.Fatal(err)
	}
	if d.projectSel != "prj_2" || d.pform.open || !tt.HasText("~/src/app") {
		t.Fatalf("app not shown: sel %q", d.projectSel)
	}

	// The folder of a managed project does not move: its form shows it as
	// text, with no field.
	d.projects = append(d.projects, protocol.Project{ID: "prj_3", Name: "notes", Kind: "managed", Machine: "mbp", Path: "~/.solax/projects/notes"})
	d.projectSel = "prj_3"
	tt.Frame()
	if err := tt.Click("Edit"); err != nil {
		t.Fatal(err)
	}
	if !d.pform.managed || !tt.HasText("~/.solax/projects/notes") || tt.HasText("~/src/my-app") {
		t.Fatalf("managed edit form = %+v: %q", d.pform, tt.Texts())
	}
}

// An edit sends only the fields that changed.
func TestProjectPatch(t *testing.T) {
	old := protocol.Project{Name: "solax", Path: "~/codes/solax", Description: "The server"}
	r := projectPatch(old, "solax", "~/src/solax", "The server")
	if r.Name != nil || r.Description != nil || r.Path == nil || *r.Path != "~/src/solax" {
		t.Fatalf("patch = %+v", r)
	}
	if r := projectPatch(old, old.Name, old.Path, old.Description); r != (protocol.UpdateProjectRequest{}) {
		t.Fatalf("no change gave %+v", r)
	}
}
