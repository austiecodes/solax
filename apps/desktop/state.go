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
	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Pages of the sidebar.
const (
	pageChat     = "chat"
	pageJobs     = "jobs"
	pageAgents   = "agents"
	pageMachines = "machines"
	pageSettings = "settings"
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
	{pageJobs, "nav.jobs", jobsIcon},
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
	loaded    bool // the jobs of this server came at least once

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
	superBusy bool   // its session runs a turn
	superSes  string // its session: its events fill the chat with steps

	// Jobs.
	jobs        []protocol.Job
	jobSel      string
	unread      map[string]bool                 // jobs that ended while the user was not on them
	events      map[string][]ir.Event           // session ID → stored and live events
	live        map[string]map[string]*liveText // session ID → item ID → streamed text
	folds       map[string]*fold                // block or step key → open or closed (transcript_view.go)
	followDraft string
	jobList     ui.ListState

	// Agents and machines.
	agents   []protocol.Agent
	machines []protocol.Machine
	form     agentForm
	pairCode string

	// Settings.
	setName, setServer, setToken string
	setAppearance                string // the Appearance choice, bound to its select

	// The server that the solax CLI of this Mac uses ("solax setup
	// local"). readLocal reads it; nil (in tests) means none.
	readLocal func() (profile.Local, error)
	local     profile.Local
	localErr  error
}

// agentForm is the "add agent" form.
type agentForm struct {
	Name, Machine, Driver, Dir, Model, Desc string
	Super                                   bool
	busy                                    bool
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
		page: p.Page, jobSel: p.Job, setAppearance: appearanceLabel(p.Appearance),
		conn: connState{State: "disconnected"}, focused: true, unread: unread,
		events: map[string][]ir.Event{}, live: map[string]map[string]*liveText{}, folds: map[string]*fold{},
		localErr: profile.ErrNoLocal, // until checkLocal runs
	}
}

// savePrefs writes the prefs with the page and job in view.
func (d *desktop) savePrefs() {
	d.prefs.Page, d.prefs.Job = d.page, d.jobSel
	d.prefs.Unread, d.prefs.SeenAt = d.unreadIDs(), d.seenNow()
	if err := d.prefs.save(d.prefsPath); err != nil {
		log.Printf("solax: save prefs: %v", err)
	}
}

// update runs fn on the UI thread. Without a window (in tests) it runs
// fn at once.
func (d *desktop) update(fn func()) {
	if d.win == nil {
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
		d.status = err.Error()
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		apply, err := work(ctx, c)
		d.update(func() {
			if err != nil {
				d.status = err.Error()
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
		d.update(func() { d.conn = connState{State: "disconnected", Error: err.Error()}; d.page = pageSettings })
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
		d.messages, d.jobs, d.agents, d.machines = nil, nil, nil, nil
		d.loaded = false // the first load of this server marks what ended since SeenAt
		// jobSel stays: after a restart it names the job last open, and
		// reloadAll loads its events. A job of another server shows as
		// "Choose a job".
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
		d.update(func() { d.status = "settings: " + err.Error() })
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
		msg := "stream closed"
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

// reloadAll reads the conversation, jobs, agents and machines.
func (d *desktop) reloadAll() {
	d.do(func(ctx context.Context, c *client.Client) (func(), error) {
		msgs, err := c.SuperMessages(ctx, 0)
		if err != nil {
			return nil, err
		}
		jobs, err := c.Jobs(ctx, 100)
		if err != nil {
			return nil, err
		}
		agents, err := c.Agents(ctx)
		if err != nil {
			return nil, err
		}
		machines, err := c.Machines(ctx)
		return func() {
			d.messages, d.agents, d.machines = msgs, agents, machines
			d.setJobs(jobs)
			if sa := d.superAgent(); sa != nil && sa.SessionID != "" {
				d.superSes = sa.SessionID
				d.loadSession(sa.SessionID)
			}
			if d.jobSel != "" {
				d.loadEvents(d.jobSel)
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
	case "job":
		var j protocol.Job
		if json.Unmarshal(p.Payload, &j) == nil {
			d.upsertJob(j)
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

// upsertJob adds or replaces a job. A job that comes to an end goes to
// jobEnded (unread.go).
func (d *desktop) upsertJob(j protocol.Job) {
	for i := range d.jobs {
		if d.jobs[i].ID == j.ID {
			ended := jobLive(d.jobs[i].State) && !jobLive(j.State)
			d.jobs[i] = j
			if ended {
				d.jobEnded(j, true)
			}
			return
		}
	}
	d.jobs = append([]protocol.Job{j}, d.jobs...)
}

// watching tells whether the user sees a job now.
func (d *desktop) watching(id string) bool {
	return d.focused && d.page == pageJobs && d.jobSel == id
}

func (d *desktop) job(id string) (protocol.Job, bool) {
	for _, j := range d.jobs {
		if j.ID == id {
			return j, true
		}
	}
	return protocol.Job{}, false
}

// sessionEvent keeps the events of the sessions in view: the Super
// Agent's, for the chat, and those of jobs. Deltas stream text in until
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
			d.superBusy = false
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

// loadEvents reads the stored events of the session of a job.
func (d *desktop) loadEvents(jobID string) {
	if j, ok := d.job(jobID); ok {
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

// toolSummary is one line for a tool call: its name and its main input.
func toolSummary(t *ir.ToolCall) string {
	var in map[string]any
	_ = json.Unmarshal(t.Input, &in)
	for _, k := range []string{"command", "cmd", "file_path", "path", "pattern", "query", "url", "description"} {
		if v, ok := in[k].(string); ok && v != "" {
			return t.Name + ": " + clip(v, 120)
		}
	}
	return t.Name
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
