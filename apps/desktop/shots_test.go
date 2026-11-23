package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// TestShots draws each page with sample data to PNG files, to look at the
// layout without a server. It runs only when SOLAX_SHOTS names a folder:
//
//	SOLAX_SHOTS=../../tmp/shots go test -run TestShots .
func TestShots(t *testing.T) {
	dir := os.Getenv("SOLAX_SHOTS")
	if dir == "" {
		t.Skip("set SOLAX_SHOTS to a folder to draw the pages")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{true, false} {
		shots := map[string]func(d *desktop){
			"chat":       func(d *desktop) { d.page = pageChat },
			"chat-empty": func(d *desktop) { d.page, d.messages = pageChat, nil },
			"chat-working": func(d *desktop) {
				d.page, d.superWaiting = pageChat, true
				d.messages = append(d.messages, protocol.Message{ID: "m5", Seq: 5, Author: protocol.Author{Kind: "human"}, Text: "give coder the README task"})
			},
			"tasks": func(d *desktop) { d.page = pageTasks },
			"task": func(d *desktop) {
				d.page, d.taskSel = pageTasks, "tsk_3"
				d.folds["steps-r1"] = &fold{open: true, set: true}
			},
			"agents":      func(d *desktop) { d.page = pageAgents },
			"agent-new":   func(d *desktop) { d.page, d.form.open = pageAgents, true },
			"machines":    func(d *desktop) { d.page = pageMachines },
			"settings":    func(d *desktop) { d.page = pageSettings },
			"tasks-agent": func(d *desktop) { d.page, d.prefs.TaskGroups = pageTasks, "agent" },
			"tasks-empty": func(d *desktop) { d.page, d.tasks = pageTasks, nil },
			"chat-unset":  func(d *desktop) { d.page, d.agents, d.messages = pageChat, nil, nil },
			"palette":     func(d *desktop) { d.openPalette() },
			"confirm": func(d *desktop) {
				d.page, d.taskSel = pageTasks, "tsk_1"
				d.ask("Cancel this task?", `coder stops work on "Add a README to the repo". You cannot resume it.`, "Cancel task", true, func() {}).keep = "Keep task"
			},
			"tasks-flat":    func(d *desktop) { d.page, d.prefs.TaskGroups = pageTasks, "none" },
			"workflows":     func(d *desktop) { d.page = pageWorkflows },
			"workflow-run":  func(d *desktop) { d.page, d.wf.sel, d.wf.item = pageWorkflows, "wfl_1", sampleRun(time.Now()) },
			"workflow-done": func(d *desktop) { d.page, d.wf.sel, d.wf.item = pageWorkflows, "wfl_2", sampleDoneRun(time.Now()) },
			"workflow-live": func(d *desktop) {
				d.page, d.wf.sel, d.wf.item = pageWorkflows, "wfl_3", sampleLiveRun(time.Now())
				followSample(d)
			},
			"workflow-live-rail": func(d *desktop) {
				d.page, d.wf.sel, d.wf.item, d.prefs.WorkflowView = pageWorkflows, "wfl_3", sampleLiveRun(time.Now()), viewRail
				followSample(d)
			},
			"workflow-run-rail": func(d *desktop) {
				d.page, d.wf.sel, d.wf.item, d.prefs.WorkflowView = pageWorkflows, "wfl_1", sampleRun(time.Now()), viewRail
			},
			"workflow-failed-rail": func(d *desktop) {
				d.page, d.wf.sel, d.wf.item, d.prefs.WorkflowView = pageWorkflows, "wfl_4", sampleFailedRun(time.Now()), viewRail
			},
			"workflow-failed": func(d *desktop) {
				d.page, d.wf.sel, d.wf.item = pageWorkflows, "wfl_4", sampleFailedRun(time.Now())
			},
			"workflow-saved": func(d *desktop) {
				d.page, d.wf.sel = pageWorkflows, "fix-until-pass"
				d.wf.item = protocol.WorkflowItem{Saved: &d.wf.list.Saved[0], Runs: d.wf.list.Runs[:1]}
				d.wf.item.Saved.Source = sampleSource
			},
		}
		for page, setup := range shots {
			d := sampleDesktop(t)
			setup(d)
			tt := ui.NewTester(d.view, 1000, 640)
			tt.SetTitleBar(ui.TitleBar{Height: 28, Left: 78})
			tt.SetDark(dark)
			tt.SetScale(2)
			tt.Advance(time.Second)
			tt.Frame()
			name := page
			if dark {
				name += "-dark"
			}
			writePNG(t, filepath.Join(dir, name+".png"), tt)
		}
	}
}

func writePNG(t *testing.T, path string, tt *ui.Tester) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

// sampleDesktop is a desktop with two machines, two agents, a few tasks and
// a short chat.
func sampleDesktop(t *testing.T) *desktop {
	d := testDesktop(t)
	now := time.Now()
	d.conn = connState{State: "connected"}
	d.machines = []protocol.Machine{
		{ID: "mac_1", Name: "AustiedeMacBook-Pro.local", State: protocol.MachineApproved, Online: true, LastSeen: now,
			Runtimes: []protocol.RuntimeProbe{{Kind: "claude", Version: "2.1.0", OK: true}, {Kind: "pi", Version: "0.9.2", OK: true}, {Kind: "codex", Problem: "not found in PATH"}}},
		{ID: "mac_2", Name: "build-box", State: protocol.MachinePending, LastSeen: now.Add(-3 * time.Hour)},
	}
	d.agents = []protocol.Agent{
		{ID: "a1", Name: "coder", Runtime: "pi", Online: true, Machine: "AustiedeMacBook-Pro.local", Dir: "/Users/austie/codes",
			Description: "General coder: implements features, refactors and fixes code"},
		{ID: "a2", Name: "solax", Role: "super", Runtime: "pi", Online: true, Machine: "AustiedeMacBook-Pro.local", Dir: "~/codes"},
		{ID: "a3", Name: "reviewer", Runtime: "claude", Machine: "build-box", Dir: "~/src/app", Model: "haiku",
			Description:  "Reads diffs and points out bugs before merge",
			Instructions: "Review only the diff. Report each bug with a failing test; do not change the style."},
	}
	d.tasks = []protocol.Task{
		{ID: "tsk_1", Agent: "coder", State: "running", Title: "Add a README to the repo", CreatedAt: now.Add(-2 * time.Minute)},
		{ID: "tsk_2", Agent: "reviewer", State: "failed", Title: "Review the auth change", CreatedAt: now.Add(-50 * time.Minute)},
		{ID: "tsk_3", Agent: "coder", State: "done", Title: "You are doing a README pass", CreatedAt: now.Add(-time.Hour), SessionID: "ses_3",
			Prompt: "Read the repo and write a short README."},
	}
	d.unread["tsk_2"] = true
	d.wf.list = sampleWorkflows(now)
	d.messages = []protocol.Message{
		{ID: "m1", Seq: 1, Author: protocol.Author{Kind: "human"}, Text: "hello", CreatedAt: now},
		{ID: "m2", Seq: 2, Author: protocol.Author{Kind: "super"}, Text: "Hi! I'm the Solax router. I dispatch work to your coding agents on your machines. What would you like done today?", CreatedAt: now},
		{ID: "m3", Seq: 3, Author: protocol.Author{Kind: "human"}, Text: "can you add another pi as coder?", CreatedAt: now},
		{ID: "m4", Seq: 4, Author: protocol.Author{Kind: "super"}, Text: "You can run it yourself in a terminal:\n\n```bash\nsolax agent add coder --runtime pi --dir ~/codes \\\n    --desc \"General coder: implements features\"\n```\n\nThen tell me what to give it.", CreatedAt: now},
	}
	d.events["ses_3"] = []ir.Event{
		done(1, ir.Item{ID: "u1", Kind: ir.ItemUserMessage, Text: "Read the repo and write a short README."}, now),
		done(2, ir.Item{ID: "r1", Kind: ir.ItemReasoning, Text: "Look at the layout of the repo first, then at the commands."}, now.Add(3*time.Second)),
		done(3, ir.Item{ID: "t1", Kind: ir.ItemToolCall, Tool: shell("ls -la")}, now),
		done(4, ir.Item{ID: "t2", Kind: ir.ItemToolCall, Tool: shell("go list ./...")}, now),
		done(5, ir.Item{ID: "a1", Kind: ir.ItemAgentMessage, Text: "I wrote `README.md`. It covers:\n\n- what Solax is\n- how to run the server and the daemon\n- how to pair a machine"}, now),
		done(6, ir.Item{ID: "u2", Kind: ir.ItemUserMessage, Text: "Add a section on the desktop app too."}, now),
		done(7, ir.Item{ID: "t3", Kind: ir.ItemToolCall, Tool: shell("cat apps/desktop/README.md")}, now),
		done(8, ir.Item{ID: "a2", Kind: ir.ItemAgentMessage, Text: "Done. The new section links to `apps/desktop/README.md`."}, now),
	}
	return d
}
