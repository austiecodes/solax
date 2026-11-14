package main

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// A transcript is what an agent did, as blocks: the messages of the user,
// the replies of the agent, and between them its steps (reasoning and
// tool calls), folded into one group, as chat apps show them. The chat
// with the Super Agent and the jobs both show one; this file builds it
// from IR events, so every driver looks the same. transcript_view.go
// draws it.

type blockKind int

const (
	blockUser  blockKind = iota // a message from the user
	blockReply                  // text from the agent, in markdown
	blockSteps                  // reasoning and tool calls, folded
	blockNote                   // one line: a job that ended, an error
)

type block struct {
	kind  blockKind
	key   string // stable while the block grows, for the list and the fold
	text  string
	steps []step
	live  bool   // steps: the agent works on them now; reply: it streams
	job   string // note: the job it tells of
	warn  bool   // note: an error or a warning
}

// step is one thing the agent did: it thought, or it called a tool.
type step struct {
	id     string
	kind   ir.ItemKind // ir.ItemReasoning or ir.ItemToolCall
	text   string      // what it thought
	tool   *ir.ToolCall
	status ir.ItemStatus
	took   time.Duration // 0 when unknown
}

func (s step) running() bool { return s.status == ir.StatusInProgress }

// liveText is text that streams in for an item that is not complete yet.
type liveText struct {
	kind string // the delta kind: "text", "reasoning" or "tool_output"
	text string
}

// eventBlocks turns the events of a session into blocks. live is the text
// that streams in now; busy tells that the agent still works, so the last
// steps are live.
func eventBlocks(evs []ir.Event, live map[string]*liveText, busy bool) []block {
	var t transcript
	started := map[string]time.Time{}
	for _, e := range evs {
		switch e.Type {
		case ir.ItemStarted:
			if e.Item == nil {
				continue
			}
			started[e.Item.ID] = e.At
			if e.Item.Kind == ir.ItemToolCall && e.Item.Tool != nil {
				t.addStep(step{id: e.Item.ID, kind: ir.ItemToolCall, tool: e.Item.Tool, status: ir.StatusInProgress})
			}
		case ir.ItemCompleted:
			if e.Item != nil {
				t.item(e.Item, took(started[e.Item.ID], e.At))
			}
		case ir.TurnCompleted:
			if e.Turn != nil && e.Turn.Error != nil {
				t.note(block{key: "turn-" + e.TurnID, text: "The turn failed: " + e.Turn.Error.Message, warn: true})
			}
		case ir.NoticeEmitted:
			if n := e.Notice; n != nil && n.Level != "info" {
				t.note(block{key: noteKey(e), text: n.Message, warn: true})
			}
		}
	}
	// What streams in comes last, in the order of the item IDs.
	for _, id := range slices.Sorted(maps.Keys(live)) {
		l := live[id]
		switch l.kind {
		case "reasoning":
			t.addStep(step{id: id, kind: ir.ItemReasoning, text: l.text, status: ir.StatusInProgress})
		case "text":
			if strings.TrimSpace(l.text) != "" {
				t.blocks = append(t.blocks, block{kind: blockReply, key: id, text: l.text, live: true})
			}
		}
	}
	if busy {
		if last := len(t.blocks) - 1; last >= 0 && t.blocks[last].kind == blockSteps {
			t.blocks[last].live = true
		}
	}
	for i := range t.blocks {
		if b := &t.blocks[i]; b.kind == blockSteps && slices.ContainsFunc(b.steps, step.running) {
			b.live = true
		}
	}
	return t.blocks
}

// transcript collects blocks.
type transcript struct{ blocks []block }

func (t *transcript) item(it *ir.Item, d time.Duration) {
	switch it.Kind {
	case ir.ItemUserMessage:
		if text := strings.TrimSpace(it.Text); text != "" {
			t.blocks = append(t.blocks, block{kind: blockUser, key: it.ID, text: text})
		}
	case ir.ItemAgentMessage:
		if text := strings.TrimSpace(it.Text); text != "" {
			t.blocks = append(t.blocks, block{kind: blockReply, key: it.ID, text: text})
		}
	case ir.ItemReasoning:
		// Some agents keep their reasoning to themselves: with no text and
		// no time, there is nothing to show.
		if strings.TrimSpace(it.Text) != "" || d > 0 {
			t.addStep(step{id: it.ID, kind: ir.ItemReasoning, text: strings.TrimSpace(it.Text), status: it.Status, took: d})
		}
	case ir.ItemToolCall:
		if it.Tool != nil {
			t.addStep(step{id: it.ID, kind: ir.ItemToolCall, tool: it.Tool, status: it.Status, took: d})
		}
	case ir.ItemCompaction:
		t.note(block{key: it.ID, text: "The context was compacted."})
	}
}

// addStep adds s to the steps at the end, or replaces the step it
// completes.
func (t *transcript) addStep(s step) {
	last := len(t.blocks) - 1
	if last < 0 || t.blocks[last].kind != blockSteps {
		// The key is the first step's: it stays as the group grows.
		t.blocks = append(t.blocks, block{kind: blockSteps, key: "steps-" + s.id})
		last++
	}
	b := &t.blocks[last]
	if i := slices.IndexFunc(b.steps, func(o step) bool { return o.id == s.id }); i >= 0 {
		b.steps[i] = s
		return
	}
	b.steps = append(b.steps, s)
}

func (t *transcript) note(b block) {
	b.kind = blockNote
	t.blocks = append(t.blocks, b)
}

func noteKey(e ir.Event) string {
	return "note-" + e.SessionID + "-" + strings.TrimSpace(e.At.Format(time.RFC3339Nano))
}

func took(from, to time.Time) time.Duration {
	if from.IsZero() || to.Before(from) {
		return 0
	}
	return to.Sub(from)
}

// chatBlocks lays out the conversation with the Super Agent: its
// messages, with the steps of the agent folded above the reply they led
// to. The messages are the truth; the events of its session add the
// steps. A reply message matches the agent message it came from by its
// text (the server makes one from the other). Steps that lead to no reply
// yet come at the end.
func chatBlocks(msgs []protocol.Message, evs []ir.Event, live map[string]*liveText, busy bool) []block {
	// lead is a reply of the events and the steps before it.
	type lead struct {
		text  string
		steps []step
	}
	var (
		leads   []lead
		pending []step
		tail    []block
	)
	for _, b := range eventBlocks(evs, live, busy) {
		switch {
		case b.kind == blockSteps:
			pending = append(pending, b.steps...)
		case b.kind == blockReply && !b.live:
			leads = append(leads, lead{b.text, pending})
			pending = nil
		case b.kind == blockReply:
			tail = append(tail, b) // it streams: no message yet
		}
	}

	var out []block
	next := 0 // the first lead not used
	for _, m := range msgs {
		switch m.Author.Kind {
		case "human":
			out = append(out, block{kind: blockUser, key: m.ID, text: m.Text})
		case "system":
			// "[job <id>] agent coder: done"
			id := jobOf(m.Text)
			out = append(out, block{kind: blockNote, key: m.ID, text: strings.TrimPrefix(m.Text, "[job "+id+"] "), job: id})
		default:
			text := strings.TrimSpace(m.Text)
			if k := slices.IndexFunc(leads[next:], func(l lead) bool { return l.text == text }); k >= 0 {
				var steps []step
				for _, l := range leads[next : next+k+1] {
					steps = append(steps, l.steps...)
				}
				next += k + 1
				if len(steps) > 0 {
					out = append(out, block{kind: blockSteps, key: "steps-" + steps[0].id, steps: steps})
				}
			}
			out = append(out, block{kind: blockReply, key: m.ID, text: m.Text})
		}
	}
	// The steps of replies that have no message yet, and those after them.
	var rest []step
	for _, l := range leads[next:] {
		rest = append(rest, l.steps...)
	}
	rest = append(rest, pending...)
	if len(rest) > 0 {
		out = append(out, block{kind: blockSteps, key: "steps-" + rest[0].id, steps: rest,
			live: busy || slices.ContainsFunc(rest, step.running)})
	}
	return append(out, tail...)
}

// jobBlocks is the transcript of a job: its prompt, then what its session
// did. A job without events (an old one, or one that never ran) shows its
// result; one that failed ends with its error.
func jobBlocks(j protocol.Job, evs []ir.Event, live map[string]*liveText) []block {
	blocks := eventBlocks(evs, live, jobLive(j.State))
	prompt := strings.TrimSpace(j.Prompt)
	if prompt != "" && !slices.ContainsFunc(blocks, func(b block) bool { return b.kind == blockUser && b.text == prompt }) {
		blocks = slices.Insert(blocks, 0, block{kind: blockUser, key: "prompt-" + j.ID, text: prompt})
	}
	if len(evs) == 0 && j.Result != "" {
		blocks = append(blocks, block{kind: blockReply, key: "result-" + j.ID, text: j.Result})
	}
	if j.Error != "" {
		blocks = append(blocks, block{kind: blockNote, key: "error-" + j.ID, text: j.Error, warn: true})
	}
	return blocks
}

// --- how a step reads ---

// toolVerb and toolArg say what a tool call did: "Ran" "git status".
func toolVerb(t *ir.ToolCall) string {
	switch t.Class {
	case ir.ToolShell:
		return "Ran"
	case ir.ToolFileRead:
		return "Read"
	case ir.ToolFileSearch:
		return "Searched"
	case ir.ToolFileChange:
		return "Edited"
	case ir.ToolWeb:
		return "Fetched"
	case ir.ToolSubagent:
		return "Delegated"
	case ir.ToolScript:
		return "Ran script"
	}
	return "Used " + t.Name
}

// toolArg is the main input of a tool call, on one line.
func toolArg(t *ir.ToolCall) string {
	if len(t.Files) > 0 {
		paths := make([]string, len(t.Files))
		for i, f := range t.Files {
			paths[i] = f.Path
		}
		return clip(strings.Join(paths, ", "), 120)
	}
	var in map[string]any
	_ = json.Unmarshal(t.Input, &in)
	for _, k := range []string{"command", "cmd", "file_path", "path", "pattern", "query", "url", "description", "prompt"} {
		if v, ok := in[k].(string); ok && v != "" {
			return clip(v, 120)
		}
	}
	if t.Class == ir.ToolMCP || t.Class == ir.ToolOther {
		return ""
	}
	return t.Name
}

// stepsTitle is the line of a closed group: "4 tools used", "Thought
// for 2s", or what the agent does now.
func stepsTitle(b block) string {
	if b.live {
		for _, s := range slices.Backward(b.steps) {
			if s.running() && s.kind == ir.ItemToolCall {
				return strings.TrimSpace(presentVerb(s.tool) + " " + toolArg(s.tool))
			}
		}
		if n := len(b.steps); n > 0 && b.steps[n-1].kind == ir.ItemReasoning && b.steps[n-1].running() {
			return "Thinking"
		}
		return "Working"
	}
	tools, thought := 0, time.Duration(0)
	for _, s := range b.steps {
		if s.kind == ir.ItemToolCall {
			tools++
		} else {
			thought += s.took
		}
	}
	if tools == 0 {
		return thoughtFor(thought)
	}
	if tools == 1 {
		return "1 tool used"
	}
	return plural(tools, "tool") + " used"
}

// presentVerb is toolVerb as it happens: "Running".
func presentVerb(t *ir.ToolCall) string {
	switch t.Class {
	case ir.ToolShell, ir.ToolScript:
		return "Running"
	case ir.ToolFileRead:
		return "Reading"
	case ir.ToolFileSearch:
		return "Searching"
	case ir.ToolFileChange:
		return "Editing"
	case ir.ToolWeb:
		return "Fetching"
	case ir.ToolSubagent:
		return "Delegating"
	}
	return "Using " + t.Name
}

func thoughtFor(d time.Duration) string {
	if d < time.Second {
		return "Thought"
	}
	return "Thought for " + seconds(d)
}

// seconds writes a short duration: "4s", "2m 5s".
func seconds(d time.Duration) string {
	s := int(d.Round(time.Second) / time.Second)
	if s < 60 {
		return strconv.Itoa(s) + "s"
	}
	if s%60 == 0 {
		return strconv.Itoa(s/60) + "m"
	}
	return strconv.Itoa(s/60) + "m " + strconv.Itoa(s%60) + "s"
}
