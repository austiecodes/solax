// Package workflow runs workflow scripts on the server (dynamic-workflow.md
// §5–7). Each run has its own sandbox (package js). Every wait of a script
// is a step that the service saves, so a run that starts again after a
// server restart replays the finished steps from SQLite and goes on where it
// stopped.
package workflow

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/austiecodes/solax/internal/js"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/internal/server/service"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Limits of one run, on top of js.Limits.
const (
	maxWall  = 6 * time.Hour // from the start of the run
	maxSteps = 200
	maxTasks = 8 // tasks that run at once; more wait
)

//go:embed prelude.js
var preludeSource string

// entrySource joins the script and the prelude. Only this module can
// import, so the script can import nothing.
const entrySource = `import * as script from "solax:script";
import { makeWf } from "solax:prelude";

export function meta() {
  return script.meta === undefined ? null : script.meta;
}

export async function main(host, args) {
  if (typeof script.run !== "function") {
    throw new Error("the script must export: async function run(wf, args)");
  }
  return await script.run(makeWf(host), args);
}
`

var (
	errStopped = errors.New("workflow: stopped")
	errWall    = fmt.Errorf("workflow: ran longer than %s", maxWall)
)

// Runner runs the workflows of the server.
type Runner struct {
	ctx    context.Context // ends when the runner closes
	cancel context.CancelFunc
	log    *slog.Logger
	lim    js.Limits
	wg     sync.WaitGroup

	mu   sync.Mutex
	runs map[model.ID]*run
}

// New returns a Runner. Close stops it.
func New(log *slog.Logger) *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	return &Runner{ctx: ctx, cancel: cancel, log: log, lim: js.DefaultLimits, runs: map[model.ID]*run{}}
}

// Close stops all runs and waits for them. Their workflows stay active, so
// the next server start resumes them.
func (r *Runner) Close() {
	r.cancel()
	r.wg.Wait()
}

// Check compiles a script and reads its meta.
func (r *Runner) Check(ctx context.Context, source string) (protocol.WorkflowMeta, error) {
	var meta protocol.WorkflowMeta
	mod, err := r.compile(ctx, source)
	if err != nil {
		return meta, err
	}
	vm, err := js.New(ctx, mod, r.lim)
	if err != nil {
		return meta, err
	}
	defer vm.Close()
	out, err := vm.Call(ctx, "meta")
	if err != nil {
		return meta, fmt.Errorf("meta: %w", err)
	}
	if string(out) != "null" {
		if err := json.Unmarshal(out, &meta); err != nil {
			return meta, fmt.Errorf("meta: %w", err)
		}
	}
	return meta, nil
}

func (r *Runner) compile(ctx context.Context, source string) (*js.Module, error) {
	script, err := js.Compile(ctx, "script.ts", source, r.lim)
	if err != nil {
		return nil, err
	}
	prelude, err := js.Compile(ctx, "solax:prelude", preludeSource, js.Limits{})
	if err != nil {
		return nil, fmt.Errorf("prelude: %w", err)
	}
	entry, err := js.Compile(ctx, "solax:entry", entrySource, js.Limits{})
	if err != nil {
		return nil, fmt.Errorf("entry: %w", err)
	}
	return js.Link(entry, map[string]*js.Module{"solax:script": script, "solax:prelude": prelude})
}

// Start runs a workflow, unless it runs already.
func (r *Runner) Start(id model.ID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.runs[id]; ok || r.ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithCancelCause(r.ctx)
	rn := &run{id: id, r: r, cancel: cancel, wake: make(chan struct{}), tasks: make(chan struct{}, maxTasks)}
	r.runs[id] = rn
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		rn.exec(ctx)
	}()
}

// Stop stops the run of a workflow. Its state stays as the service set
// it.
func (r *Runner) Stop(id model.ID) {
	r.mu.Lock()
	rn := r.runs[id]
	r.mu.Unlock()
	if rn != nil {
		rn.cancel(errStopped)
	}
}

// Wake tells a run that one of its waits may be over.
func (r *Runner) Wake(id model.ID) {
	r.mu.Lock()
	rn := r.runs[id]
	r.mu.Unlock()
	if rn != nil {
		rn.notify()
	}
}

// Resume starts the runs of all workflows that run or wait, after a server
// restart.
func (r *Runner) Resume(ctx context.Context) error {
	ws, err := service.ActiveWorkflows(ctx)
	if err != nil {
		return err
	}
	for _, w := range ws {
		r.Start(w.ID)
	}
	return nil
}

func (r *Runner) done(id model.ID) {
	r.mu.Lock()
	delete(r.runs, id)
	r.mu.Unlock()
}
