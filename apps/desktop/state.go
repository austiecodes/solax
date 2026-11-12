package main

import (
	"context"
	"encoding/json"
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

// connState is the state of the server stream.
type connState struct {
	State  string // connecting | connected | disconnected
	Server string
	Error  string
}

// desktop is the whole state of the window. The view reads it on the UI
// thread; background work changes it only through update.
type desktop struct {
	profiles *profile.Store
	win      *mygo.Window

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
	superNote string // what it does now, for example a command it runs

	// Jobs.
	jobs       []protocol.Job
	jobSel     string
	events     map[string][]ir.Event      // session ID → stored and live events
	live       map[string]map[string]string // session ID → item ID → streamed text
	followDraft string

	// Agents and machines.
	agents   []protocol.Agent
	machines []protocol.Machine
	form     agentForm
	pairCode string

	// Settings.
	setName, setServer, setToken string
}

// agentForm is the "add agent" form.
type agentForm struct {
	Name, Machine, Driver, Dir, Model, Desc string
	Super                                   bool
	busy                                    bool
}

func newDesktop(profiles *profile.Store) *desktop {
	return &desktop{
		profiles: profiles, page: pageChat,
		conn:   connState{State: "disconnected"},
		events: map[string][]ir.Event{}, live: map[string]map[string]string{},
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
		d.events, d.live, d.jobSel = map[string][]ir.Event{}, map[string]map[string]string{}, ""
	})
	go d.feed(ctx, c, p.Server)
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
			d.messages, d.jobs, d.agents, d.machines = msgs, jobs, agents, machines
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

func (d *desktop) upsertJob(j protocol.Job) {
	for i := range d.jobs {
		if d.jobs[i].ID == j.ID {
			d.jobs[i] = j
			return
		}
	}
	d.jobs = append([]protocol.Job{j}, d.jobs...)
}

func (d *desktop) job(id string) (protocol.Job, bool) {
	for _, j := range d.jobs {
		if j.ID == id {
			return j, true
		}
	}
	return protocol.Job{}, false
}

// sessionEvent tracks the Super Agent activity and the live events of
// job sessions.
func (d *desktop) sessionEvent(n protocol.SessionEventNote) {
	e := n.Event
	if n.Super {
		switch e.Type {
		case ir.TurnStarted:
			d.superBusy, d.superNote = true, "thinking…"
		case ir.TurnCompleted:
			d.superBusy, d.superNote = false, ""
		case ir.ItemStarted:
			if e.Item != nil && e.Item.Tool != nil {
				d.superNote = "running " + toolSummary(e.Item.Tool)
			}
		}
		return
	}
	if e.Type == ir.ItemDelta && e.Delta != nil {
		m := d.live[n.SessionID]
		if m == nil {
			m = map[string]string{}
			d.live[n.SessionID] = m
		}
		m[e.Delta.ItemID] += e.Delta.Text
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
	j, ok := d.job(jobID)
	if !ok {
		return
	}
	d.do(func(ctx context.Context, c *client.Client) (func(), error) {
		evs, err := c.SessionEvents(ctx, j.SessionID, 0)
		return func() {
			// Keep live events that came after the stored ones.
			var last uint64
			if len(evs) > 0 {
				last = evs[len(evs)-1].Seq
			}
			for _, e := range d.events[j.SessionID] {
				if e.Seq > last {
					evs = append(evs, e)
				}
			}
			d.events[j.SessionID] = evs
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
