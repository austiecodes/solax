package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/js"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/internal/server/service"
)

// run is one run of a workflow script.
type run struct {
	id     model.ID
	r      *Runner
	cancel context.CancelCauseFunc
	w      model.Workflow // set when exec starts
	tasks  chan struct{}  // a slot for each task that runs
	steps  atomic.Int32

	mu   sync.Mutex
	wake chan struct{} // closed and replaced by notify
}

// exec runs the script to its end and reports the end to the service. A
// stop or a server shutdown leaves the workflow as it is, to resume later.
func (rn *run) exec(ctx context.Context) {
	defer rn.r.done(rn.id)
	w, err := model.GetWorkflow(ctx, model.ReadDB, rn.id)
	if err != nil {
		rn.r.log.Error("workflow: load", "workflow", rn.id, "err", err)
		return
	}
	if !w.State.Active() {
		return
	}
	rn.w = w
	ctx, cancel := context.WithDeadlineCause(ctx, w.CreatedAt.Add(maxWall), errWall)
	defer cancel()

	out, err := rn.script(ctx)
	cause := context.Cause(ctx)
	if errors.Is(cause, errStopped) || rn.r.ctx.Err() != nil {
		return
	}
	if err != nil && cause != nil {
		err = cause
	}
	var je *js.Error
	if errors.As(err, &je) && je.Stack != "" {
		err = errors.New(clip(je.Stack, 2000))
	}
	if err := service.FinishWorkflow(context.WithoutCancel(ctx), rn.id, out, err); err != nil {
		rn.r.log.Error("workflow: finish", "workflow", rn.id, "err", err)
	}
}

func (rn *run) script(ctx context.Context) (json.RawMessage, error) {
	mod, err := rn.r.compile(ctx, rn.w.Source)
	if err != nil {
		return nil, err
	}
	vm, err := js.New(ctx, mod, rn.r.lim)
	if err != nil {
		return nil, err
	}
	defer vm.Close()
	host := js.Host{
		"agents": js.AsyncFunc(rn.agents),
		"recall": js.AsyncFunc(rn.recall),
		"task":   js.AsyncFunc(rn.task),
		"reply":  js.AsyncFunc(rn.reply),
		"ask":    js.AsyncFunc(rn.ask),
		"sleep":  js.AsyncFunc(rn.sleep),
		"log":    js.Func(rn.log),
	}
	return vm.Call(ctx, "main", host, json.RawMessage(rn.w.ArgsJSON))
}

func (rn *run) notify() {
	rn.mu.Lock()
	close(rn.wake)
	rn.wake = make(chan struct{})
	rn.mu.Unlock()
}

// waitFor calls done until it reports true. It checks again after each
// notify, and each minute in case a notify was lost.
func (rn *run) waitFor(ctx context.Context, done func() (bool, error)) error {
	for {
		rn.mu.Lock()
		ch := rn.wake
		rn.mu.Unlock()
		if ok, err := done(); ok || err != nil {
			return err
		}
		select {
		case <-ch:
		case <-time.After(time.Minute):
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
}

// Steps.

// stepSpec names a step. Without a custom key, the key is the kind, the
// fingerprint and the occurrence n, so a step keeps its key when the order
// of the calls changes between runs.
type stepSpec struct {
	kind, ident string
	n           int
	key         string // opts.stepKey
	phase       string
	label       string
}

// step returns the output of a step: saved, copied from the run that this
// run resumes, or made by do. do gets a new step (State "") or a pending
// step to wait for again after a restart.
func (rn *run) step(ctx context.Context, sp stepSpec, seq int, do func(model.WorkflowStep) (json.RawMessage, error)) (json.RawMessage, error) {
	fp := fingerprint(sp.kind, sp.ident)
	key := sp.key
	if key == "" {
		key = fmt.Sprintf("%s_%s_%d", sp.kind, fp[:12], sp.n)
	}
	if rn.steps.Add(1) > maxSteps {
		return nil, fmt.Errorf("the workflow has more than %d steps", maxSteps)
	}
	st, err := model.GetStep(ctx, model.ReadDB, rn.id, key)
	switch {
	case err == nil:
		if st.Fingerprint != fp {
			err := fmt.Errorf("step %q: the script took another path on replay (its kind or arguments changed)", key)
			rn.cancel(err)
			return nil, err
		}
		if st.State != model.StepPending {
			return saved(st)
		}
		return do(st)
	case !errors.Is(err, errs.ErrNotFound):
		return nil, err
	}
	if rn.w.ResumeFrom != "" {
		old, err := model.GetStep(ctx, model.ReadDB, rn.w.ResumeFrom, key)
		if err == nil && old.Fingerprint == fp && old.State != model.StepPending {
			old.WorkflowID, old.Seq, old.CreatedAt = rn.id, seq, time.Time{}
			if err := service.RecordStep(ctx, old); err != nil {
				return nil, err
			}
			return saved(old)
		}
	}
	st = model.WorkflowStep{WorkflowID: rn.id, Key: key, Seq: seq, Kind: sp.kind, Fingerprint: fp, Phase: sp.phase, Label: sp.label}
	out, err := do(st)
	if err != nil && ctx.Err() == nil {
		st.State, st.OutputJSON = model.StepFailed, jsonString(err.Error())
		if rerr := service.RecordStep(ctx, st); rerr != nil {
			return nil, rerr
		}
	}
	return out, err
}

// done saves st as done with v as its output.
func done(ctx context.Context, st model.WorkflowStep, v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	st.State, st.OutputJSON = model.StepDone, string(b)
	return b, service.RecordStep(ctx, st)
}

// saved returns the output of a finished step: a done step's output, or a
// failed step's error.
func saved(st model.WorkflowStep) (json.RawMessage, error) {
	if st.State == model.StepFailed {
		var msg string
		_ = json.Unmarshal([]byte(st.OutputJSON), &msg)
		return nil, errors.New(msg)
	}
	return json.RawMessage(st.OutputJSON), nil
}

func fingerprint(kind, ident string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + ident))
	return hex.EncodeToString(sum[:])
}

// The host functions. Each step call gets its arguments, then the ident
// and n from the prelude (prelude.js).

type taskOpts struct {
	Schema      json.RawMessage `json:"schema"`
	Phase       string          `json:"phase"`
	Label       string          `json:"label"`
	StepKey     string          `json:"stepKey"`
	FailOnError *bool           `json:"failOnError"`
}

// outcome is TaskOutcome in workflow.d.ts.
type outcome struct {
	ID        string          `json:"id"`
	SessionID string          `json:"sessionId"`
	Agent     string          `json:"agent"`
	State     string          `json:"state"`
	Result    string          `json:"result"`
	Data      json.RawMessage `json:"data,omitempty"`
	Error     string          `json:"error,omitempty"`
}

const resultClip = 16 << 10

func (rn *run) task(ctx context.Context, c js.Call) (any, error) {
	var agent, prompt, ident string
	var o taskOpts
	var n int
	if err := args(c, &agent, &prompt, &o, &ident, &n); err != nil {
		return nil, err
	}
	if o.Schema != nil {
		if err := checkSchemaRoot(o.Schema); err != nil {
			return nil, err
		}
	}
	sp := stepSpec{kind: "task", ident: ident, n: n, key: o.StepKey, phase: o.Phase, label: o.Label}
	out, err := rn.step(ctx, sp, c.Seq, func(st model.WorkflowStep) (json.RawMessage, error) {
		release, err := rn.slot(ctx)
		if err != nil {
			return nil, err
		}
		defer release()
		if st.State == "" {
			p := prompt
			if o.Schema != nil {
				p += schemaPrompt(o.Schema)
			}
			j, err := service.StepTask(ctx, rn.w, st, agent, p)
			if err != nil {
				return nil, err
			}
			st.TargetID, st.State = string(j.ID), model.StepPending
		}
		return rn.await(ctx, st, o.Schema)
	})
	return result(out, err, o.FailOnError)
}

func (rn *run) reply(ctx context.Context, c js.Call) (any, error) {
	var taskID, text, ident string
	var o taskOpts
	var n int
	if err := args(c, &taskID, &text, &o, &ident, &n); err != nil {
		return nil, err
	}
	if o.Schema != nil {
		if err := checkSchemaRoot(o.Schema); err != nil {
			return nil, err
		}
	}
	sp := stepSpec{kind: "reply", ident: ident, n: n, key: o.StepKey, phase: o.Phase, label: o.Label}
	out, err := rn.step(ctx, sp, c.Seq, func(st model.WorkflowStep) (json.RawMessage, error) {
		release, err := rn.slot(ctx)
		if err != nil {
			return nil, err
		}
		defer release()
		if st.State == "" {
			t := text
			if o.Schema != nil {
				t += schemaPrompt(o.Schema)
			}
			st.TargetID = taskID
			if _, err := service.StepReply(ctx, rn.w, st, t); err != nil {
				return nil, err
			}
			st.State = model.StepPending
		}
		return rn.await(ctx, st, o.Schema)
	})
	return result(out, err, o.FailOnError)
}

// await waits until the task of st ends its turn and saves the outcome. An
// answer that does not match schema gets up to 2 follow-ups.
func (rn *run) await(ctx context.Context, st model.WorkflowStep, schema json.RawMessage) (json.RawMessage, error) {
	for {
		var j model.Task
		err := rn.waitFor(ctx, func() (bool, error) {
			var err error
			j, err = model.GetTask(ctx, model.ReadDB, model.ID(st.TargetID))
			return j.State.Terminal(), err
		})
		if err != nil {
			return nil, err
		}
		ag, _ := model.GetAgent(ctx, model.ReadDB, j.AgentID)
		oc := outcome{ID: string(j.ID), SessionID: string(j.SessionID), Agent: ag.Name, State: string(j.State),
			Result: clip(j.Result, resultClip), Error: j.Error}
		if j.State == model.TaskDone && schema != nil {
			data, err := answer(j.Result, schema)
			if err != nil {
				if n := retries(st); n < 2 {
					st.OutputJSON = fmt.Sprintf(`{"retries":%d}`, n+1)
					if _, err := service.StepReply(ctx, rn.w, st, schemaRetry(err)); err != nil {
						return nil, err
					}
					continue
				}
				oc.State, oc.Error = string(model.TaskFailed), "the answer does not match the schema: "+err.Error()
			}
			oc.Data = data
		}
		return done(ctx, st, oc)
	}
}

func answer(text string, schema json.RawMessage) (json.RawMessage, error) {
	data, ok := lastJSON(text)
	if !ok {
		return nil, errors.New("the answer has no JSON object")
	}
	return data, validate(schema, data)
}

// retries is the count of schema follow-ups that a pending step made.
func retries(st model.WorkflowStep) int {
	var p struct{ Retries int }
	_ = json.Unmarshal([]byte(st.OutputJSON), &p)
	return p.Retries
}

// result rejects the outcome of a task that did not end done, unless
// failOnError is false.
func result(out json.RawMessage, err error, failOnError *bool) (any, error) {
	if err != nil {
		return nil, err
	}
	var oc outcome
	if err := json.Unmarshal(out, &oc); err != nil {
		return nil, err
	}
	if oc.State != string(model.TaskDone) && (failOnError == nil || *failOnError) {
		msg := oc.Error
		if msg == "" {
			msg = oc.State
		}
		return nil, fmt.Errorf("task %s (agent %s) %s: %s", oc.ID, oc.Agent, oc.State, msg)
	}
	return out, nil
}

// slot takes one of the task slots of the run.
func (rn *run) slot(ctx context.Context) (release func(), err error) {
	select {
	case rn.tasks <- struct{}{}:
		return func() { <-rn.tasks }, nil
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	}
}

func (rn *run) ask(ctx context.Context, c js.Call) (any, error) {
	var prompt, ident string
	var options []string
	var n int
	if err := args(c, &prompt, &options, &ident, &n); err != nil {
		return nil, err
	}
	return rn.step(ctx, stepSpec{kind: "ask", ident: ident, n: n}, c.Seq, func(st model.WorkflowStep) (json.RawMessage, error) {
		if st.State == "" {
			if err := service.StepAsk(ctx, rn.w, st, prompt, options); err != nil {
				return nil, err
			}
		}
		var cur model.WorkflowStep
		err := rn.waitFor(ctx, func() (bool, error) {
			var err error
			cur, err = model.GetStep(ctx, model.ReadDB, rn.id, st.Key)
			return cur.State != model.StepPending, err
		})
		if err != nil {
			return nil, err
		}
		return saved(cur)
	})
}

func (rn *run) sleep(ctx context.Context, c js.Call) (any, error) {
	var ms float64
	var ident string
	var n int
	if err := args(c, &ms, &ident, &n); err != nil {
		return nil, err
	}
	return rn.step(ctx, stepSpec{kind: "sleep", ident: ident, n: n}, c.Seq, func(st model.WorkflowStep) (json.RawMessage, error) {
		if st.State == "" {
			d := min(time.Duration(max(ms, 0))*time.Millisecond, maxWall)
			st.TargetID, st.State = strconv.FormatInt(time.Now().Add(d).UnixMilli(), 10), model.StepPending
			if err := service.RecordStep(ctx, st); err != nil {
				return nil, err
			}
		}
		at, err := strconv.ParseInt(st.TargetID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("sleep step %s: bad wake time %q", st.Key, st.TargetID)
		}
		t := time.NewTimer(time.Until(time.UnixMilli(at)))
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		}
		return done(ctx, st, nil)
	})
}

// agentInfo is AgentInfo in workflow.d.ts.
type agentInfo struct {
	Name        string `json:"name"`
	Machine     string `json:"machine"`
	Runtime     string `json:"runtime"`
	Online      bool   `json:"online"`
	Description string `json:"description"`
}

func (rn *run) agents(ctx context.Context, c js.Call) (any, error) {
	var ident string
	var n int
	if err := args(c, &ident, &n); err != nil {
		return nil, err
	}
	return rn.step(ctx, stepSpec{kind: "agents", ident: ident, n: n}, c.Seq, func(st model.WorkflowStep) (json.RawMessage, error) {
		views, err := service.Agents(ctx, service.WorkflowActor(rn.w))
		if err != nil {
			return nil, err
		}
		out := []agentInfo{}
		for _, v := range views {
			if v.Role == model.RoleSuper {
				continue
			}
			out = append(out, agentInfo{Name: v.Name, Machine: v.MachineName, Runtime: string(v.Runtime), Online: v.Online, Description: v.Description})
		}
		return done(ctx, st, out)
	})
}

func (rn *run) recall(ctx context.Context, c js.Call) (any, error) {
	var id, ident string
	var n int
	if err := args(c, &id, &ident, &n); err != nil {
		return nil, err
	}
	return rn.step(ctx, stepSpec{kind: "recall", ident: ident, n: n}, c.Seq, func(st model.WorkflowStep) (json.RawMessage, error) {
		obj, err := service.Recall(ctx, service.WorkflowActor(rn.w), model.ID(id))
		if err != nil {
			return nil, err
		}
		return done(ctx, st, obj)
	})
}

// log is a Func: it runs on the script's goroutine and does not wait for
// the script.
func (rn *run) log(c js.Call) (any, error) {
	var msg string
	if err := args(c, &msg); err != nil {
		return nil, err
	}
	if err := service.WorkflowLog(rn.r.ctx, rn.id, c.Seq, msg); err != nil {
		rn.r.log.Warn("workflow: log", "workflow", rn.id, "err", err)
	}
	return nil, nil
}

// args decodes the arguments of a host call into ptrs, in order. A
// missing argument leaves its target as it is.
func args(c js.Call, ptrs ...any) error {
	for i, p := range ptrs {
		if i >= len(c.Args) || string(c.Args[i]) == "null" {
			continue
		}
		if err := json.Unmarshal(c.Args[i], p); err != nil {
			return fmt.Errorf("argument %d: %w", i+1, err)
		}
	}
	return nil
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "…"
}
