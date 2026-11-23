package main

import (
	"context"
	"encoding/json"
	"log"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/austiecodes/solax/apps/desktop/internal/profile"
	"github.com/austiecodes/solax/apps/desktop/locales"
	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Pages of the sidebar.
const (
	pageChat      = "chat"
	pageTasks     = "tasks"
	pageWorkflows = "workflows"
	pageAgents    = "agents"
	pageMachines  = "machines"
	pageSettings  = "settings"
)

// navPage is one page of the sidebar: the action that opens it and the
// icon of its row. A new page is one entry here, one action in
// internal/actions, and its view in pageView.
type navPage struct {
	id, action string
	icon       *ui.SVG
}

var navPages = []navPage{
	{pageChat, "nav.chat", sparklesIcon},
	{pageTasks, "nav.tasks", tasksIcon},
	{pageWorkflows, "nav.workflows", workflowIcon},
	{pageAgents, "nav.agents", agentIcon},
	{pageMachines, "nav.machines", machineIcon},
}

func validPage(id string) bool {
	return id == pageSettings || slices.ContainsFunc(navPages, func(p navPage) bool { return p.id == id })
}

// connState is the state of the server stream.
type connState struct {
	State  string // connecting | connected | disconnected
	Server string
	Error  string
}

// desktop is the whole state of the window. The view reads it on the UI
// thread; background work changes it only through update.
type desktop struct {
	profiles  *profile.Store
	win       *mygo.Window
	prefs     prefs
	prefsPath string  // "" in tests: nothing is saved
	sideDrag  float32 // the sidebar width when a drag of its edge began
	shown     float32 // how far the sidebar shows, 0 to 1, as last drawn
	focused   bool    // the window is in front
	notes     notifier
	loaded    bool // the tasks of this server came at least once

	// Over the page: the command palette and a pending question.
	pal     paletteState
	confirm *confirmation

	// client and the stream are replaced when the profile changes.
	mu       sync.Mutex
	client   *client.Client
	stopFeed context.CancelFunc

	page   string
	conn   connState
	status string // the last error, shown until the next action
	md     markdown.Options

	// Chat with the Super Agent.
	messages  []protocol.Message
	draft     string
	sending   bool
	chatList  ui.ListState
	superBusy bool // its session runs a turn
	// superWaiting is true from a send until the turn after it ends, so
	// the chat shows "Working…" while the session starts.
	superWaiting bool
	superSes     string // its session: its events fill the chat with steps

	// Tasks.
	tasks       []protocol.Task
	taskSel     string
	unread      map[string]bool                 // tasks that ended while the user was not on them
	events      map[string][]ir.Event           // session ID → stored and live events
	live        map[string]map[string]*liveText // session ID → item ID → streamed text
	folds       map[string]*fold                // block or step key → open or closed (transcript_view.go)
	followDraft string
	taskList    ui.ListState

	// Workflows (workflows.go).
	wf wfState

	// Agents and machines.
	agents     []protocol.Agent
	machines   []protocol.Machine
	agentSel   string // the agent the Agents page shows, by name
	machineSel string // the machine the Machines page shows, by ID
	form       agentForm
	instr      instrDraft
	pairCode   string

	// Settings.
	setName, setServer, setToken string
	lang                         string // the language MyGo keeps, "" for the system's

	// queue takes the updates of background work in tests (update).
	queue chan func()

	// The server that the solax CLI of this Mac uses ("solax setup
	// local"). readLocal reads it; nil (in tests) means none.
	readLocal func() (profile.Local, error)
	local     profile.Local
	localErr  error
}

// agentForm is the "new agent" form.
type agentForm struct {
	Name, Machine, Runtime, Dir, Model, Desc, Instructions string
	Super                                                  bool
	open                                                   bool // it shows in place of the chosen agent
	busy                                                   bool
}

// instrDraft edits the instructions of the agent that the Agents page
// shows. base is the text of the agent when the edit started: the draft
// is changed when text is not base.
type instrDraft struct {
	agent      string // agent ID
	base, text string
	busy       bool
}

// newDesktop makes the state. prefsPath is where the window prefs live;
// "" keeps them in memory only.
func newDesktop(profiles *profile.Store, prefsPath string) *desktop {
	p := loadPrefs(prefsPath)
	unread := map[string]bool{}
	for _, id := range p.Unread {
		unread[id] = true
	}
	return &desktop{
		profiles: profiles, prefs: p, prefsPath: prefsPath, notes: noNotifier{},
		page: p.Page, taskSel: p.Task,
		conn: connState{State: "disconnected"}, focused: true, unread: unread,
		events: map[string][]ir.Event{}, live: map[string]map[string]*liveText{}, folds: map[string]*fold{},
		localErr: profile.ErrNoLocal, // until checkLocal runs
	}
}

// savePrefs writes the prefs with the page and task in view.
func (d *desktop) savePrefs() {
	d.prefs.Page, d.prefs.Task = d.page, d.taskSel
	d.prefs.Unread, d.prefs.SeenAt = d.unreadIDs(), d.seenNow()
	if err := d.prefs.save(d.prefsPath); err != nil {
		log.Printf("solax: save prefs: %v", err)
	}
}

// update runs fn on the UI thread. Without a window (in tests) it runs
// fn at once, or hands it to queue when the test set one, to run it on
// the test's goroutine.
func (d *desktop) update(fn func()) {
	if d.win == nil {
		if d.queue != nil {
			d.queue <- fn
			return
		}
		fn()
		return
	}
	d.win.Update(fn)
}

// api returns the client of the profile in use.
func (d *desktop) api() (*client.Client, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.client == nil {
		return nil, profile.ErrNoProfile
	}
	return d.client, nil
}

// do runs work off the UI thread and applies its result on it. An error
// goes to the status line.
func (d *desktop) do(work func(ctx context.Context, c *client.Client) (func(), error)) {
	c, err := d.api()
	if err != nil {
		d.status = errText(err)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		apply, err := work(ctx, c)
		d.update(func() {
			if err != nil {
				d.status = errText(err)
				return
			}
			d.status = ""
			if apply != nil {
				apply()
			}
		})
	}()
}

// connect uses the current profile: a new client, a new stream, and a
// full reload. It is safe to call again after the profile changed.
func (d *desktop) connect() {
	local, lerr := d.checkLocal()
	if lerr == nil {
		// With no profile yet, a local setup is the answer: no form.
		if _, err := d.profiles.AdoptLocal(local); err != nil {
			log.Printf("solax: use local setup: %v", err)
		}
		// "solax setup local" may have made a new token since.
		if _, err := d.profiles.SyncLocal(local); err != nil {
			log.Printf("solax: sync local token: %v", err)
		}
	}
	p, token, err := d.profiles.Current()
	if err != nil {
		d.update(func() { d.conn = connState{State: "disconnected", Error: errText(err)}; d.page = pageSettings })
		return
	}
	c := client.New(p.Server, token)
	ctx, cancel := context.WithCancel(context.Background())
	d.mu.Lock()
	if d.stopFeed != nil {
		d.stopFeed()
	}
	d.client, d.stopFeed = c, cancel
	d.mu.Unlock()
	d.update(func() {
		d.setName, d.setServer = p.Name, p.Server
		d.messages, d.tasks, d.agents, d.machines = nil, nil, nil, nil
		d.wf.list, d.wf.item = protocol.WorkflowList{}, protocol.WorkflowItem{}
		d.loaded = false // the first load of this server marks what ended since SeenAt
		// taskSel stays: after a restart it names the task last open, and
		// reloadAll loads its events. A task of another server shows as
		// "Choose a task".
		d.events, d.live, d.superSes = map[string][]ir.Event{}, map[string]map[string]*liveText{}, ""
	})
	go d.feed(ctx, c, p.Server)
}

// checkLocal reads the local setup again and shows the result in
// Settings.
func (d *desktop) checkLocal() (profile.Local, error) {
	l, err := profile.Local{}, profile.ErrNoLocal
	if d.readLocal != nil {
		l, err = d.readLocal()
	}
	d.update(func() { d.local, d.localErr = l, err })
	return l, err
}

// useLocal switches to the local setup.
func (d *desktop) useLocal() {
	l, err := d.checkLocal()
	if err == nil {
		err = d.profiles.UseLocal(l)
	}
	if err != nil {
		d.update(func() { d.status = locales.SettingsError(errText(err)) })
		return
	}
	d.update(func() { d.status = ""; d.page = pageChat })
	d.connect()
}

// stop closes the stream.
func (d *desktop) stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopFeed != nil {
		d.stopFeed()
	}
}

// feed keeps the stream open and reconnects with backoff. After each
// connect it reloads everything, so nothing missed while away is lost.
func (d *desktop) feed(ctx context.Context, c *client.Client, server string) {
	backoff := time.Second
	for ctx.Err() == nil {
		d.update(func() { d.conn = connState{State: "connecting", Server: server} })
		err := c.StreamReady(ctx, func() {
			backoff = time.Second
			d.update(func() { d.conn = connState{State: "connected", Server: server} })
			d.reloadAll()
		}, func(p protocol.Push) { d.update(func() { d.apply(p) }) })
		if ctx.Err() != nil {
			return
		}
		msg := locales.StreamClosed()
		if err != nil {
			msg = err.Error()
		}
		d.update(func() { d.conn = connState{State: "disconnected", Server: server, Error: msg} })
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

// reloadAll reads the conversation, tasks, workflows, agents and machines.
func (d *desktop) reloadAll() {
	d.do(func(ctx context.Context, c *client.Client) (func(), error) {
		msgs, err := c.SuperMessages(ctx, 0)
		if err != nil {
			return nil, err
		}
		tasks, err := c.Tasks(ctx, 100)
		if err != nil {
			return nil, err
		}
		agents, err := c.Agents(ctx)
		if err != nil {
			return nil, err
		}
		machines, err := c.Machines(ctx)
		if err != nil {
			return nil, err
		}
		wfs, err := c.Workflows(ctx)
		return func() {
			d.messages, d.agents, d.machines, d.wf.list = msgs, agents, machines, wfs
			d.setTasks(tasks)
			if sa := d.superAgent(); sa != nil && sa.SessionID != "" {
				d.superSes = sa.SessionID
				d.loadSession(sa.SessionID)
			}
			if d.taskSel != "" {
				d.loadEvents(d.taskSel)
			}
			if d.wf.sel != "" {
				d.loadWorkflow(d.wf.sel)
			}
		}, err
	})
}

func (d *desktop) reloadAgents() {
	d.do(func(ctx context.Context, c *client.Client) (func(), error) {
		agents, err := c.Agents(ctx)
		if err != nil {
			return nil, err
		}
		machines, err := c.Machines(ctx)
		return func() { d.agents, d.machines = agents, machines }, err
	})
}

// apply merges one push into the state. It runs on the UI thread.
func (d *desktop) apply(p protocol.Push) {
	switch p.Event {
	case "super.message":
		var m protocol.Message
		if json.Unmarshal(p.Payload, &m) == nil {
			d.addMessage(m)
		}
	case "task":
		var j protocol.Task
		if json.Unmarshal(p.Payload, &j) == nil {
			d.upsertTask(j)
			d.workflowTask(j)
		}
	case "workflow":
		var w protocol.Workflow
		if json.Unmarshal(p.Payload, &w) == nil {
			d.upsertWorkflow(w)
		}
	case "session.event":
		var n protocol.SessionEventNote
		if json.Unmarshal(p.Payload, &n) == nil {
			d.sessionEvent(n)
		}
	case "agents.changed", "machines.changed":
		d.reloadAgents()
	}
}

func (d *desktop) addMessage(m protocol.Message) {
	for _, old := range d.messages {
		if old.ID == m.ID {
			return
		}
	}
	d.messages = append(d.messages, m)
	sort.SliceStable(d.messages, func(i, j int) bool { return d.messages[i].Seq < d.messages[j].Seq })
}

// upsertTask adds or replaces a task. A task that comes to an end goes to
// taskEnded (unread.go).
func (d *desktop) upsertTask(j protocol.Task) {
	for i := range d.tasks {
		if d.tasks[i].ID == j.ID {
			ended := taskLive(d.tasks[i].State) && !taskLive(j.State)
			d.tasks[i] = j
			if ended {
				d.taskEnded(j, true)
			}
			return
		}
	}
	d.tasks = append([]protocol.Task{j}, d.tasks...)
}

// watching tells whether the user sees a task now.
func (d *desktop) watching(id string) bool {
	return d.focused && d.page == pageTasks && d.taskSel == id
}

func (d *desktop) task(id string) (protocol.Task, bool) {
	for _, j := range d.tasks {
		if j.ID == id {
			return j, true
		}
	}
	return protocol.Task{}, false
}

// sessionEvent keeps the events of the sessions in view: the Super
// Agent's, for the chat, and those of tasks. Deltas stream text in until
// their item completes.
func (d *desktop) sessionEvent(n protocol.SessionEventNote) {
	e := n.Event
	if n.Super {
		if n.SessionID != "" && d.superSes != n.SessionID {
			// The first event of a new session: read what came before it.
			d.superSes = n.SessionID
			d.loadSession(n.SessionID)
		}
		switch e.Type {
		case ir.TurnStarted:
			d.superBusy = true
		case ir.TurnCompleted, ir.SessionExitedEvent:
			d.superBusy, d.superWaiting = false, false
		}
	}
	if e.Type == ir.ItemDelta && e.Delta != nil {
		m := d.live[n.SessionID]
		if m == nil {
			m = map[string]*liveText{}
			d.live[n.SessionID] = m
		}
		l := m[e.Delta.ItemID]
		if l == nil {
			l = &liveText{kind: e.Delta.Kind}
			m[e.Delta.ItemID] = l
		}
		l.text += e.Delta.Text
		return
	}
	if e.Type == ir.ItemCompleted && e.Item != nil {
		delete(d.live[n.SessionID], e.Item.ID)
	}
	evs := d.events[n.SessionID]
	if len(evs) > 0 && evs[len(evs)-1].Seq >= e.Seq {
		return // already loaded
	}
	d.events[n.SessionID] = append(evs, e)
}

// loadEvents reads the stored events of the session of a task.
func (d *desktop) loadEvents(taskID string) {
	if j, ok := d.task(taskID); ok {
		d.loadSession(j.SessionID)
	}
}

// loadSession reads the stored events of a session, and keeps the live
// events that came after them.
func (d *desktop) loadSession(id string) {
	d.do(func(ctx context.Context, c *client.Client) (func(), error) {
		evs, err := c.SessionEvents(ctx, id, 0)
		return func() {
			var last uint64
			if len(evs) > 0 {
				last = evs[len(evs)-1].Seq
			}
			for _, e := range d.events[id] {
				if e.Seq > last {
					evs = append(evs, e)
				}
			}
			d.events[id] = evs
		}, err
	})
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
