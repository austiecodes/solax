package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/austiecodes/solax/apps/desktop/locales"
	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/protocol"
)

// The workflows page. A workflow is a script that the server runs; this
// page shows its runs and the saved scripts, as the tasks page shows
// tasks: a list, or one run or saved script that takes the whole card.

// wfState is the state of the workflows page.
type wfState struct {
	list   protocol.WorkflowList
	sel    string                // the run (wfl_ ID) or saved script (name) in view; "" for the list
	item   protocol.WorkflowItem // the full form of sel: source, steps, runs
	answer string                // the text of an answer to wf.ask
	tab    string                // the tab of the output card: result, log or script
	code   markdown.Options      // the highlighted code of the output card
	args   string                // the args of a saved script to run, as JSON
	busy   bool

	followed map[string]bool // the sessions whose stored events followTasks loaded
	unfold   map[string]bool // the rail: the finished phases opened
	rounds   map[string]int  // the graph: the round that each loop shows
	focus    string          // the graph: the step it last scrolled to
}

// workflowLive tells whether a run can still change.
func workflowLive(state string) bool { return state == "running" || state == "waiting" }

// reloadWorkflows reads the runs and the saved scripts, and the one in
// view.
func (d *desktop) reloadWorkflows() {
	d.do(func(ctx context.Context, c *client.Client) (func(), error) {
		l, err := c.Workflows(ctx)
		return func() {
			d.wf.list = l
			if d.wf.sel != "" {
				d.loadWorkflow(d.wf.sel)
			}
		}, err
	})
}

// loadWorkflow reads the full form of a run or a saved script.
func (d *desktop) loadWorkflow(ref string) {
	d.do(func(ctx context.Context, c *client.Client) (func(), error) {
		it, err := c.Workflow(ctx, ref)
		return func() {
			if d.wf.sel == ref {
				d.wf.item = it
				if it.Run != nil && workflowLive(it.Run.State) {
					d.followTasks(*it.Run)
				}
			}
		}, err
	})
}

// openWorkflow shows a run or a saved script.
func (d *desktop) openWorkflow(ref string) {
	d.page, d.status = pageWorkflows, ""
	if d.wf.sel != ref {
		d.wf.sel, d.wf.item, d.wf.answer, d.wf.args = ref, protocol.WorkflowItem{}, "", ""
	}
	d.loadWorkflow(ref)
}

// upsertWorkflow merges a pushed run into the list. A push has no steps,
// so the run in view loads again.
func (d *desktop) upsertWorkflow(w protocol.Workflow) {
	found := false
	for i := range d.wf.list.Runs {
		if d.wf.list.Runs[i].ID == w.ID {
			d.wf.list.Runs[i], found = w, true
		}
	}
	if !found {
		d.wf.list.Runs = append([]protocol.Workflow{w}, d.wf.list.Runs...)
	}
	if d.wf.sel == w.ID {
		d.loadWorkflow(w.ID)
	}
}

// workflowTask follows a task of a workflow: its steps change with it.
func (d *desktop) workflowTask(j protocol.Task) {
	if j.WorkflowID != "" && j.WorkflowID == d.wf.sel && d.page == pageWorkflows {
		d.loadWorkflow(j.WorkflowID)
	}
}

func (d *desktop) workflowsPage(c *ui.Context) {
	switch it := d.wf.item; {
	case d.wf.sel == "":
	case it.Run != nil && it.Run.ID == d.wf.sel:
		d.runDetail(c, *it.Run)
		return
	case it.Saved != nil && it.Saved.Name == d.wf.sel:
		d.savedDetail(c, *it.Saved, it.Runs)
		return
	default:
		d.header(c, pageSpec{icon: workflowIcon, title: d.wf.sel, actions: d.closeWorkflow(c)})
		return // it loads
	}
	pal := paletteFor(c)
	runs, saved := d.wf.list.Runs, d.wf.list.Saved
	spec := pageSpec{icon: workflowIcon, title: locales.NavWorkflows(), sub: locales.WorkflowsSubtitle(),
		actions: func() { iconButton(c, "refresh", reloadIcon, locales.Refresh(), pal, d.reloadWorkflows) }}
	if len(runs) == 0 && len(saved) == 0 {
		d.header(c, spec)
		emptyState(c, pal, emptySpec{
			icon: workflowIcon, title: locales.WorkflowsEmptyTitle(), detail: locales.WorkflowsEmptyDetail(),
			command: "solax workflow run review.ts",
			action:  locales.ChatPlaceholder(), run: func() { d.goTo(pageChat) },
		})
		return
	}
	d.showPage(c, spec, func() {
		section(c, "runs", sectionSpec{title: locales.WorkflowRuns(), count: countText(len(runs)), rows: listRows}, func() {
			if len(runs) == 0 {
				note(c, locales.WorkflowRunsEmpty())
			}
			for _, w := range runs {
				d.runItem(c, pal, w)
			}
		})
		section(c, "saved", sectionSpec{
			title: locales.WorkflowSaved(), count: countText(len(saved)), rows: listRows,
			desc: locales.WorkflowSavedHelp(),
		}, func() {
			if len(saved) == 0 {
				note(c, locales.WorkflowSavedEmpty())
			}
			for _, s := range saved {
				d.savedItem(c, pal, s)
			}
		})
	})
}

// closeWorkflow is the button that goes back to the list.
func (d *desktop) closeWorkflow(c *ui.Context) func() {
	return func() {
		iconButton(c, "close", closeIcon, locales.Close(), paletteFor(c), func() { d.wf.sel = "" })
	}
}

// runItem is a run in a list, on one line.
func (d *desktop) runItem(c *ui.Context, pal *palette, w protocol.Workflow) {
	it := listItem(c, w.ID, false, w.Title, func() {
		statusIcon(c, pal, w.State, false, 15)
		ui.Text(c, w.Title).FontSize(13.5).TextColor(pal.text).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		if w.State == "waiting" {
			chip(c, locales.WorkflowAsks(), markWarn)
		}
		if w.Name != "" && w.Name != w.Title {
			chip(c, w.Name, markQuiet)
		}
		ui.Text(c, ago(w.CreatedAt)).FontSize(12).TextColor(pal.textFaint).FontFeatures("tnum").SingleLine().TextAlign(ui.End).Width(80).Shrink(0)
	})
	if it.Clicked() {
		d.openWorkflow(w.ID)
	}
}

// savedItem is a saved script in the list, on one line.
func (d *desktop) savedItem(c *ui.Context, pal *palette, s protocol.SavedWorkflow) {
	it := listItem(c, "saved-"+s.Name, false, s.Name, func() {
		ui.Text(c, s.Name).FontSize(13.5).FontWeight(600).TextColor(pal.text).SingleLine().Shrink(0)
		ui.Text(c, s.Description).FontSize(13).TextColor(pal.textMuted).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
	})
	if it.Clicked() {
		d.openWorkflow(s.Name)
	}
}

// runDetail shows a run: its open question, its state, its steps, its
// result, its log and its script.
func (d *desktop) runDetail(c *ui.Context, w protocol.Workflow) {
	tl := buildTimeline(w, d.phasesOf(w))
	sub := stateWord(w.State)
	if w.Name != "" && w.Name != w.Title {
		sub = w.Name + " · " + sub
	}
	if r := tl.liveRound(); r != "" {
		sub += " · " + r
	}
	d.showPage(c, pageSpec{icon: workflowIcon, title: w.Title, sub: sub, wide: true, actions: func() {
		// A failed run offers this in its callout.
		if w.State == "cancelled" && button(c, "resume", locales.WorkflowResumeRun(), plainButton, d.wf.busy) {
			d.resumeWorkflow(w)
		}
		if workflowLive(w.State) && button(c, "cancel", locales.WorkflowCancel(), plainButton, false) {
			id := w.ID
			d.ask(locales.WorkflowCancelQuestion(), locales.WorkflowCancelDetail(), locales.WorkflowCancel(), true, func() {
				d.do(func(ctx context.Context, c *client.Client) (func(), error) {
					nw, err := c.CancelWorkflow(ctx, id)
					return func() { d.upsertWorkflow(nw) }, err
				})
			}).keep = locales.WorkflowKeep()
		}
		d.closeWorkflow(c)()
	}}, func() {
		if q, ok := openQuestion(w); ok && w.State == "waiting" {
			d.questionView(c, w.ID, q)
		}
		if w.State == "failed" {
			d.failureCallout(c, w, tl)
		}
		d.runSummary(c, w, tl)
		if len(w.Steps) > 0 {
			d.runView(c, tl)
		}
		d.outputView(c, w)
	})
}

// outputView shows the result, the log and the script of a run in one
// card, chosen with tabs.
func (d *desktop) outputView(c *ui.Context, w protocol.Workflow) {
	pal := paletteFor(c)
	var segs []segment
	if len(w.Result) > 0 {
		segs = append(segs, segment{"result", locales.WorkflowResult()})
	}
	if w.Logs != "" {
		segs = append(segs, segment{"log", locales.WorkflowLog()})
	}
	if w.Source != "" {
		segs = append(segs, segment{"script", locales.WorkflowScript()})
	}
	if len(segs) == 0 {
		return
	}
	tab := segs[0].value
	for _, sg := range segs {
		if sg.value == d.wf.tab {
			tab = d.wf.tab
		}
	}
	text := ""
	switch tab {
	case "result":
		text = resultText(w.Result)
	case "log":
		text = strings.TrimRight(w.Logs, "\n")
	case "script":
		text = w.Source
	}
	ui.Column(c.Key("output")).Gap(10).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			if t, ok := segmented(c, "output-tabs", tab, segs); ok {
				d.wf.tab = t
			}
			ui.Spacer(c)
			iconButton(c, "copy", docIcon, locales.WorkflowCopy(), pal, func() { c.WriteClipboard(text) })
		})
		ui.Column(c).Radius(12).Border(1, pal.divider).Background(pal.panel).Clip().Children(func() {
			switch tab {
			case "log":
				logView(c, pal, text)
			case "result":
				lang := "json"
				if !json.Valid([]byte(text)) {
					lang = ""
				}
				d.codeView(c, lang, text)
			default:
				d.codeView(c, "ts", text)
			}
		})
	})
}

// codeView shows code in a language, highlighted as the transcript's
// code blocks, with no head: the card around it has the tabs.
func (d *desktop) codeView(c *ui.Context, lang, code string) {
	if d.wf.code.CodeBlock == nil {
		d.wf.code.CodeBlock = func(c *ui.Context, b *markdown.CodeBlockView) { b.Body(c) }
		d.wf.code.Theme = &markdown.Theme{FontSize: 13}
	}
	fence := strings.Repeat("`", max(3, longestRun(code, '`')+1))
	ui.Column(c).Padding(4, 2).Children(func() {
		markdown.View(c, fence+lang+"\n"+code+"\n"+fence, &d.wf.code)
	})
}

// longestRun is the length of the longest run of r in s.
func longestRun(s string, r rune) int {
	most, n := 0, 0
	for _, x := range s {
		if x == r {
			n++
			most = max(most, n)
		} else {
			n = 0
		}
	}
	return most
}

// logView shows the log of a run: the lines that wf.phase and wf.round
// write stand out.
func logView(c *ui.Context, pal *palette, text string) {
	ui.Column(c).Padding(12, sectionPad).Gap(2).Selectable().Children(func() {
		for i, line := range strings.Split(text, "\n") {
			col := pal.text
			switch {
			case strings.HasPrefix(line, "phase: "):
				col = pal.accent
			case strings.HasPrefix(line, "round: "):
				col = pal.attention
			case strings.Contains(line, " failed: "):
				col = pal.danger
			}
			ui.Text(c.Key(i), line).Font("monospace").FontSize(12).LineHeight(1.5).TextColor(col)
		}
	})
}

// question is an open wf.ask, as the server keeps it in the step's
// target.
type question struct {
	Prompt  string   `json:"prompt"`
	Options []string `json:"options"`
}

// openQuestion returns the oldest open question of a run: the one that
// an answer goes to.
func openQuestion(w protocol.Workflow) (question, bool) {
	for _, st := range w.Steps {
		if st.Kind != "ask" || st.State != "pending" {
			continue
		}
		var q question
		if json.Unmarshal([]byte(st.Target), &q) == nil {
			return q, true
		}
	}
	return question{}, false
}

// questionView asks the user what the script asks: a button for each
// option, or a text to send.
func (d *desktop) questionView(c *ui.Context, id string, q question) {
	pal := paletteFor(c)
	send := func(choice, text string) {
		d.wf.busy = true
		d.do(func(ctx context.Context, c *client.Client) (func(), error) {
			nw, err := c.AnswerWorkflow(ctx, id, choice, text)
			d.update(func() { d.wf.busy = false })
			return func() { d.wf.answer = ""; d.upsertWorkflow(nw) }, err
		})
		if _, err := d.api(); err != nil {
			d.wf.busy = false
		}
	}
	section(c, "question", sectionSpec{
		title: locales.WorkflowQuestion(), lead: func() { avatar(c, workflowIcon, markWarn) },
		footer: func() {
			text := strings.TrimSpace(d.wf.answer)
			if len(q.Options) == 0 {
				if button(c, "answer", locales.WorkflowAnswer(), primaryButton, d.wf.busy || text == "") {
					send(text, "")
				}
				return
			}
			for i, o := range q.Options {
				kind := plainButton
				if i == 0 {
					kind = primaryButton
				}
				if button(c, "option-"+o, o, kind, d.wf.busy) {
					send(o, text)
				}
			}
		},
	}, func() {
		ui.Text(c, q.Prompt).FontSize(13.5).LineHeight(1.5).TextColor(pal.text).Selectable().Padding(14, sectionPad, 6, sectionPad)
		help := locales.WorkflowAnswerHelp()
		if len(q.Options) > 0 {
			help = locales.WorkflowNoteHelp()
		}
		stackRow(c, "answer-text", help, "", func() { textArea(c, "answer-text", &d.wf.answer, "", 56) })
	})
}

// stepText is what a step says: its label, or the title of its task or
// the prompt of its question, and the agent of its task.
func (d *desktop) stepText(st protocol.WorkflowStep) (text, agent string) {
	text = st.Label
	switch st.Kind {
	case "task", "reply":
		if j, ok := d.task(st.Target); ok {
			agent = j.Agent
			if text == "" {
				text = j.Title
			}
		}
	case "ask":
		var q question
		if text == "" && json.Unmarshal([]byte(st.Target), &q) == nil {
			text = q.Prompt
		}
	}
	if text == "" {
		text = st.Target
	}
	if text == "" {
		text = st.Kind
	}
	return text, agent
}

// activity says what the agent of a task does now, as its transcript
// does: "Running go test ./...", "Thinking"; "" when that is not known.
func (d *desktop) activity(taskID string) string {
	j, ok := d.task(taskID)
	if !ok || !taskLive(j.State) || len(d.events[j.SessionID]) == 0 {
		return ""
	}
	bs := eventBlocks(d.events[j.SessionID], d.live[j.SessionID], true)
	if n := len(bs); n > 0 && bs[n-1].live && bs[n-1].kind == blockSteps {
		return clip(stepsTitle(bs[n-1]), 72)
	}
	return ""
}

// followTasks loads the stored events of the tasks that a run waits for,
// once, so that their bars show what their agents do. Later events come
// as pushes.
func (d *desktop) followTasks(w protocol.Workflow) {
	for _, st := range w.Steps {
		if st.State != "pending" || (st.Kind != "task" && st.Kind != "reply") {
			continue
		}
		j, ok := d.task(st.Target)
		if !ok || j.SessionID == "" || d.wf.followed[j.SessionID] {
			continue
		}
		if d.wf.followed == nil {
			d.wf.followed = map[string]bool{}
		}
		d.wf.followed[j.SessionID] = true
		d.loadSession(j.SessionID)
	}
}

// phasesOf returns the phases that the saved script of w declares.
func (d *desktop) phasesOf(w protocol.Workflow) []protocol.WorkflowPhase {
	for _, s := range d.wf.list.Saved {
		if w.Name != "" && s.Name == w.Name {
			return s.Meta.Phases
		}
	}
	return nil
}

// savedDetail shows a saved script, runs it with args, and lists its
// runs.
func (d *desktop) savedDetail(c *ui.Context, s protocol.SavedWorkflow, runs []protocol.Workflow) {
	pal := paletteFor(c)
	d.showPage(c, pageSpec{icon: workflowIcon, title: s.Name, sub: s.Description, actions: d.closeWorkflow(c)}, func() {
		section(c, "start", sectionSpec{
			title: locales.WorkflowRunTitle(), desc: locales.WorkflowRunHelp(),
			footer: func() {
				if button(c, "run", locales.WorkflowRun(), primaryButton, d.wf.busy) {
					d.startSaved(s.Name)
				}
			},
		}, func() {
			if s.Meta.WhenToUse != "" {
				row(c, "when", locales.WorkflowWhen(), s.Meta.WhenToUse, nil)
			}
			for i, p := range s.Meta.Phases {
				row(c, "phase-"+p.Title, locales.WorkflowPhase(i+1, p.Title), p.Detail, nil)
			}
			stackRow(c, "args", locales.WorkflowArgs(), locales.WorkflowArgsHelp(), func() {
				textArea(c, "args", &d.wf.args, `{"goal": "…"}`, 72)
			})
		})
		section(c, "runs", sectionSpec{title: locales.WorkflowRuns(), count: countText(len(runs)), rows: listRows}, func() {
			if len(runs) == 0 {
				note(c, locales.WorkflowRunsEmpty())
			}
			for _, w := range runs {
				d.runItem(c, pal, w)
			}
		})
		if s.Source != "" {
			section(c, "script", sectionSpec{title: locales.WorkflowScript()}, func() { d.codeView(c, "ts", s.Source) })
		}
	})
}

// startSaved runs a saved script with the args of the form, and shows the
// new run.
func (d *desktop) startSaved(name string) {
	req := protocol.StartWorkflowRequest{Name: name}
	if a := strings.TrimSpace(d.wf.args); a != "" {
		if !json.Valid([]byte(a)) {
			d.status = locales.WorkflowArgsInvalid()
			return
		}
		req.Args = json.RawMessage(a)
	}
	d.startWorkflow(req)
}

// startWorkflow starts a run and shows it.
func (d *desktop) startWorkflow(req protocol.StartWorkflowRequest) {
	d.wf.busy = true
	d.do(func(ctx context.Context, c *client.Client) (func(), error) {
		w, err := c.StartWorkflow(ctx, req)
		d.update(func() { d.wf.busy = false })
		return func() {
			d.upsertWorkflow(w)
			d.openWorkflow(w.ID)
		}, err
	})
	if _, err := d.api(); err != nil {
		d.wf.busy = false
	}
}

// resultText is the result of a run as text: a JSON string as it is,
// other JSON indented.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var b bytes.Buffer
	if json.Indent(&b, raw, "", "  ") != nil {
		return string(raw)
	}
	return b.String()
}

// monoText is text in the monospace font, to select and copy.
func monoText(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).Font("monospace").FontSize(12).LineHeight(1.5).TextColor(paletteFor(c).text).Selectable()
}

// workflowCount is what the sidebar row of the page says: that a run
// asks something, else how many runs work.
func (d *desktop) workflowCount(c *ui.Context, pal *palette) {
	live, asks := 0, false
	for _, w := range d.wf.list.Runs {
		if workflowLive(w.State) {
			live++
		}
		asks = asks || w.State == "waiting"
	}
	switch {
	case asks:
		statusIcon(c, pal, "waiting", false, 12)
	case live > 0:
		ui.Textf(c, "%d", live).FontSize(12).TextColor(pal.textFaint).FontFeatures("tnum")
	}
}
