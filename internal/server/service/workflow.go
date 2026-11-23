package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Workflows (dynamic-workflow.md). A workflow is a TypeScript script that
// the server runs. The runner (package workflow) runs scripts; this file
// holds the rules and is the only writer of workflow state. The runner
// calls the Step functions below for each wait of a script.

// The service reaches the runner through these functions. The server sets
// them at start; the defaults act as if no runner exists.
var (
	// CheckWorkflow compiles a script and reads its meta.
	CheckWorkflow = func(ctx context.Context, source string) (protocol.WorkflowMeta, error) {
		return protocol.WorkflowMeta{}, errs.ErrUnavailable
	}
	// RunWorkflow starts the runner of a workflow.
	RunWorkflow = func(id model.ID) {}
	// StopWorkflow stops the runner of a workflow.
	StopWorkflow = func(id model.ID) {}
	// WakeWorkflow tells the runner of a workflow that one of its waits
	// may be over: a task ended a turn, or a question has an answer.
	WakeWorkflow = func(id model.ID) {}
)

const (
	logLines   = 200      // wf.log lines kept for each workflow
	resultClip = 16 << 10 // bytes of a workflow result in a Super Agent input
)

var savedName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// StartWorkflow starts a run of a script, or of a saved workflow, and
// returns at once. With r.Resume, the run reuses the finished steps of an
// earlier run while the script makes the same calls.
func StartWorkflow(ctx context.Context, a model.Actor, r protocol.StartWorkflowRequest) (model.Workflow, error) {
	if err := requireCaller(a); err != nil {
		return model.Workflow{}, err
	}
	source, name := r.Source, ""
	if r.Name != "" {
		sw, err := model.GetSavedWorkflow(ctx, model.ReadDB, a.OwnerID, r.Name)
		if err != nil {
			return model.Workflow{}, fmt.Errorf("saved workflow %q: %w", r.Name, err)
		}
		source, name = sw.Source, sw.Name
	}
	if strings.TrimSpace(source) == "" {
		return model.Workflow{}, fmt.Errorf("%w: no script", errs.ErrInvalid)
	}
	meta, err := CheckWorkflow(ctx, source)
	if err != nil {
		return model.Workflow{}, scriptError(err)
	}
	if name == "" {
		name = meta.Name
	}
	args := "null"
	if len(r.Args) > 0 {
		if !json.Valid(r.Args) {
			return model.Workflow{}, fmt.Errorf("%w: args are not JSON", errs.ErrInvalid)
		}
		args = string(r.Args)
	}
	var w model.Workflow
	err = model.Atomic(ctx, func(tx *sql.Tx) error {
		now := clock()
		w = model.Workflow{
			ID: model.NewIDAt(model.PrefixWorkflow, now), OwnerID: a.OwnerID, RequestedBy: a.Kind, Name: name, Title: name,
			Source: source, ArgsJSON: args, State: model.WorkflowRunning, CreatedAt: now, UpdatedAt: now,
		}
		if w.Title == "" {
			w.Title = "untitled"
		}
		if r.Resume != "" {
			old, err := ownedWorkflow(ctx, tx, a, model.ID(r.Resume))
			if err != nil {
				return err
			}
			w.ResumeFrom = old.ID
		}
		return model.SaveWorkflow(ctx, tx, w)
	})
	if err != nil {
		return model.Workflow{}, err
	}
	publishWorkflow(ctx, w)
	RunWorkflow(w.ID)
	return w, nil
}

// SaveWorkflowScript keeps a script under meta.name. The same name
// replaces the old script.
func SaveWorkflowScript(ctx context.Context, a model.Actor, source string) (model.SavedWorkflow, error) {
	if err := requireCaller(a); err != nil {
		return model.SavedWorkflow{}, err
	}
	meta, err := CheckWorkflow(ctx, source)
	if err != nil {
		return model.SavedWorkflow{}, scriptError(err)
	}
	switch {
	case meta.Name == "" || meta.Description == "":
		return model.SavedWorkflow{}, fmt.Errorf("%w: a saved workflow needs meta.name and meta.description", errs.ErrInvalid)
	case strings.HasPrefix(meta.Name, string(model.PrefixWorkflow)+"_"):
		return model.SavedWorkflow{}, fmt.Errorf("%w: meta.name must not start with %q", errs.ErrInvalid, "wfl_")
	case !savedName.MatchString(meta.Name):
		return model.SavedWorkflow{}, fmt.Errorf("%w: meta.name %q: use up to 64 letters, digits, '.', '_' and '-'", errs.ErrInvalid, meta.Name)
	}
	b, err := json.Marshal(meta)
	if err != nil {
		return model.SavedWorkflow{}, err
	}
	now := clock()
	sw := model.SavedWorkflow{OwnerID: a.OwnerID, Name: meta.Name, Description: meta.Description, MetaJSON: string(b),
		Source: source, CreatedAt: now, UpdatedAt: now}
	err = model.Atomic(ctx, func(tx *sql.Tx) error { return model.SaveSavedWorkflow(ctx, tx, sw) })
	return sw, err
}

// Workflows returns the recent runs, newest first, and the saved workflows.
func Workflows(ctx context.Context, a model.Actor, limit int) ([]model.Workflow, []model.SavedWorkflow, error) {
	if err := requireCaller(a); err != nil {
		return nil, nil, err
	}
	runs, err := model.ListWorkflows(ctx, model.ReadDB, a.OwnerID, limit)
	if err != nil {
		return nil, nil, err
	}
	saved, err := model.ListSavedWorkflows(ctx, model.ReadDB, a.OwnerID)
	return runs, saved, err
}

// WorkflowRun returns one run with its steps.
func WorkflowRun(ctx context.Context, a model.Actor, id model.ID) (model.Workflow, []model.WorkflowStep, error) {
	if err := requireCaller(a); err != nil {
		return model.Workflow{}, nil, err
	}
	w, err := ownedWorkflow(ctx, model.ReadDB, a, id)
	if err != nil {
		return w, nil, err
	}
	steps, err := model.ListSteps(ctx, model.ReadDB, id)
	return w, steps, err
}

// SavedWorkflow returns a saved workflow and its recent runs.
func SavedWorkflow(ctx context.Context, a model.Actor, name string) (model.SavedWorkflow, []model.Workflow, error) {
	if err := requireCaller(a); err != nil {
		return model.SavedWorkflow{}, nil, err
	}
	sw, err := model.GetSavedWorkflow(ctx, model.ReadDB, a.OwnerID, name)
	if err != nil {
		return sw, nil, fmt.Errorf("saved workflow %q: %w", name, err)
	}
	all, err := model.ListWorkflows(ctx, model.ReadDB, a.OwnerID, 500)
	var runs []model.Workflow
	for _, w := range all {
		if w.Name == name && len(runs) < 20 {
			runs = append(runs, w)
		}
	}
	return sw, runs, err
}

// CancelWorkflow stops a workflow and cancels its active tasks.
func CancelWorkflow(ctx context.Context, a model.Actor, id model.ID) (model.Workflow, error) {
	if err := requireCaller(a); err != nil {
		return model.Workflow{}, err
	}
	var (
		w    model.Workflow
		post afterCommit
	)
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		post = nil
		var err error
		if w, err = ownedWorkflow(ctx, tx, a, id); err != nil || !w.State.Active() {
			return err
		}
		tasks, err := model.ListWorkflowTasks(ctx, tx, w.ID)
		if err != nil {
			return err
		}
		for _, j := range tasks {
			if !j.State.Terminal() {
				if _, err := cancelTask(ctx, tx, a, j, &post); err != nil {
					return err
				}
			}
		}
		w.State, w.UpdatedAt = model.WorkflowCancelled, clock()
		if err := model.SaveWorkflow(ctx, tx, w); err != nil {
			return err
		}
		if a.Kind != model.AuthorSuper && w.RequestedBy == model.AuthorSuper {
			return toSuper(ctx, tx, w.OwnerID, workflowHead(w, "cancelled by the user"), "", ir.ModeNote, &post)
		}
		return nil
	})
	if err != nil {
		return model.Workflow{}, err
	}
	post.run()
	StopWorkflow(w.ID)
	publishWorkflow(ctx, w)
	return w, nil
}

// askTarget is what a pending ask step keeps in TargetID.
type askTarget struct {
	Prompt  string   `json:"prompt"`
	Options []string `json:"options,omitempty"`
}

// AnswerWorkflow answers the oldest open question (wf.ask) of a workflow.
// With options, choice must be one of them.
func AnswerWorkflow(ctx context.Context, a model.Actor, id model.ID, choice, text string) (model.Workflow, error) {
	if err := requireCaller(a); err != nil {
		return model.Workflow{}, err
	}
	choice = strings.TrimSpace(choice)
	var w model.Workflow
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		var err error
		if w, err = ownedWorkflow(ctx, tx, a, id); err != nil {
			return err
		}
		steps, err := model.ListSteps(ctx, tx, w.ID)
		if err != nil {
			return err
		}
		var open []model.WorkflowStep
		for _, st := range steps {
			if st.Kind == "ask" && st.State == model.StepPending {
				open = append(open, st)
			}
		}
		if len(open) == 0 || !w.State.Active() {
			return fmt.Errorf("%w: workflow %s has no open question", errs.ErrConflict, w.ID)
		}
		st := open[0]
		var q askTarget
		if err := json.Unmarshal([]byte(st.TargetID), &q); err != nil {
			return err
		}
		if len(q.Options) > 0 && !slices.Contains(q.Options, choice) {
			return fmt.Errorf("%w: choice %q: want one of %s", errs.ErrInvalid, choice, strings.Join(q.Options, ", "))
		}
		out, err := json.Marshal(map[string]string{"choice": choice, "text": strings.TrimSpace(text)})
		if err != nil {
			return err
		}
		now := clock()
		st.State, st.OutputJSON, st.UpdatedAt = model.StepDone, string(out), now
		if err := model.SaveStep(ctx, tx, st); err != nil {
			return err
		}
		if len(open) == 1 {
			w.State = model.WorkflowRunning
		}
		w.UpdatedAt = now
		return model.SaveWorkflow(ctx, tx, w)
	})
	if err != nil {
		return model.Workflow{}, err
	}
	publishWorkflow(ctx, w)
	WakeWorkflow(w.ID)
	return w, nil
}

// The runner side.

// WorkflowActor is the identity of a workflow when it acts on tasks.
func WorkflowActor(w model.Workflow) model.Actor {
	return model.Actor{Kind: model.AuthorWorkflow, ID: w.ID, OwnerID: w.OwnerID}
}

// ActiveWorkflows returns the workflows that run or wait, to resume when
// the server starts.
func ActiveWorkflows(ctx context.Context) ([]model.Workflow, error) {
	return model.ListActiveWorkflows(ctx, model.ReadDB)
}

// StepTask starts a task for a step of w and saves the step as pending
// with the task as its target, in one transaction: a crash cannot start a
// task that the step does not know.
func StepTask(ctx context.Context, w model.Workflow, st model.WorkflowStep, agent, prompt string) (model.Task, error) {
	var j model.Task
	err := model.Atomic(ctx, func(tx *sql.Tx) (err error) {
		if j, err = dispatch(ctx, tx, WorkflowActor(w), agent, prompt, w.ID); err != nil {
			return err
		}
		st.TargetID, st.State = string(j.ID), model.StepPending
		return saveStep(ctx, tx, st)
	})
	if err != nil {
		return model.Task{}, err
	}
	publishTask(ctx, j)
	Wake()
	return j, nil
}

// StepReply sends text as a follow-up to the task st.TargetID and saves st,
// in one transaction. It serves a new reply step and the retry of a step
// whose answer did not match its schema.
func StepReply(ctx context.Context, w model.Workflow, st model.WorkflowStep, text string) (model.Task, error) {
	var j model.Task
	err := model.Atomic(ctx, func(tx *sql.Tx) (err error) {
		if j, err = followUp(ctx, tx, WorkflowActor(w), model.ID(st.TargetID), text, ir.ModeFollowUp); err != nil {
			return err
		}
		st.State = model.StepPending
		return saveStep(ctx, tx, st)
	})
	if err != nil {
		return model.Task{}, err
	}
	publishTask(ctx, j)
	Wake()
	return j, nil
}

// StepAsk saves a question of w as a pending step, sets w waiting, and asks
// whoever started w: the Super Agent wakes, a person sees it in the
// conversation.
func StepAsk(ctx context.Context, w model.Workflow, st model.WorkflowStep, prompt string, options []string) error {
	var post afterCommit
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		post = nil
		cur, err := model.GetWorkflow(ctx, tx, w.ID)
		if err != nil {
			return err
		}
		if !cur.State.Active() {
			return fmt.Errorf("workflow %s is %s", w.ID, cur.State)
		}
		b, err := json.Marshal(askTarget{Prompt: prompt, Options: options})
		if err != nil {
			return err
		}
		st.TargetID, st.State = string(b), model.StepPending
		if err := saveStep(ctx, tx, st); err != nil {
			return err
		}
		cur.State, cur.UpdatedAt = model.WorkflowWaiting, clock()
		if err := model.SaveWorkflow(ctx, tx, cur); err != nil {
			return err
		}
		post.add(func() { publishWorkflow(context.WithoutCancel(ctx), cur) })
		var b2 strings.Builder
		head := workflowHead(cur, "asks: "+clip(prompt, 200))
		fmt.Fprintf(&b2, "%s\n\n%s\n", head, prompt)
		if len(options) > 0 {
			fmt.Fprintf(&b2, "options: %s\n", strings.Join(options, " | "))
		}
		fmt.Fprintf(&b2, "answer with: solax workflow answer %s \"<choice>\" [\"<text>\"]", cur.ID)
		return tellRequester(ctx, tx, cur, head, b2.String(), &post)
	})
	post.run()
	if err == nil {
		Wake()
	}
	return err
}

// RecordStep saves a step as the runner sets it: done or failed with its
// output, or a copy from the run that w resumes.
func RecordStep(ctx context.Context, st model.WorkflowStep) error {
	return model.Atomic(ctx, func(tx *sql.Tx) error { return saveStep(ctx, tx, st) })
}

func saveStep(ctx context.Context, tx *sql.Tx, st model.WorkflowStep) error {
	now := clock()
	if st.CreatedAt.IsZero() {
		st.CreatedAt = now
	}
	st.UpdatedAt = now
	return model.SaveStep(ctx, tx, st)
}

// WorkflowLog adds a wf.log line. seq is the order of the line in the
// script; a replayed script sends old lines again, and they are dropped.
func WorkflowLog(ctx context.Context, id model.ID, seq int, line string) error {
	var w model.Workflow
	changed := false
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		var err error
		if w, err = model.GetWorkflow(ctx, tx, id); err != nil {
			return err
		}
		if seq <= w.LogSeq {
			return nil
		}
		lines := strings.Split(w.Logs, "\n")
		if w.Logs == "" {
			lines = nil
		}
		lines = append(lines, clip(strings.ReplaceAll(line, "\n", " "), 500))
		if len(lines) > logLines {
			lines = lines[len(lines)-logLines:]
		}
		w.Logs, w.LogSeq, w.UpdatedAt = strings.Join(lines, "\n"), seq, clock()
		changed = true
		return model.SaveWorkflow(ctx, tx, w)
	})
	if err == nil && changed {
		publishWorkflow(ctx, w)
	}
	return err
}

// FinishWorkflow ends a workflow: done with its result (JSON), or failed
// with failure. A workflow that is no longer active keeps its state. The
// one who started it hears once: the Super Agent wakes, a person sees it in
// the conversation.
func FinishWorkflow(ctx context.Context, id model.ID, result json.RawMessage, failure error) error {
	var (
		w    model.Workflow
		post afterCommit
	)
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		post = nil
		var err error
		if w, err = model.GetWorkflow(ctx, tx, id); err != nil || !w.State.Active() {
			return err
		}
		w.State, w.UpdatedAt = model.WorkflowDone, clock()
		w.Result = string(result)
		if failure != nil {
			w.State, w.Error, w.Result = model.WorkflowFailed, failure.Error(), ""
		}
		if err := model.SaveWorkflow(ctx, tx, w); err != nil {
			return err
		}
		head := workflowHead(w, string(w.State))
		var b strings.Builder
		b.WriteString(head)
		if w.Error != "" {
			b.WriteString("\nerror: ")
			b.WriteString(w.Error)
		}
		if w.Result != "" && w.Result != "null" {
			b.WriteString("\nresult:\n")
			b.WriteString(clip(w.Result, resultClip))
		}
		fmt.Fprintf(&b, "\ndetails: solax workflow list %s", w.ID)
		return tellRequester(ctx, tx, w, head, b.String(), &post)
	})
	post.run()
	if err == nil {
		publishWorkflow(ctx, w)
		Wake()
	}
	return err
}

// tellRequester wakes the Super Agent with text when it started w. For a
// person, head goes to the conversation as a system line.
func tellRequester(ctx context.Context, tx *sql.Tx, w model.Workflow, head, text string, post *afterCommit) error {
	if w.RequestedBy == model.AuthorSuper {
		return toSuper(ctx, tx, w.OwnerID, head, text, ir.ModeFollowUp, post)
	}
	msg, err := appendSuper(ctx, tx, w.OwnerID, model.Author{Kind: model.AuthorSystem}, model.MessageSystem, head)
	if err != nil {
		return err
	}
	post.add(func() { Publish(context.WithoutCancel(ctx), w.OwnerID, PushMessage, model.WireMessage(msg)) })
	return nil
}

func workflowHead(w model.Workflow, what string) string {
	return fmt.Sprintf("[workflow %s] %s: %s", w.ID, w.Title, what)
}

func ownedWorkflow(ctx context.Context, db model.Querier, a model.Actor, id model.ID) (model.Workflow, error) {
	w, err := model.GetWorkflow(ctx, db, id)
	if err != nil {
		return w, fmt.Errorf("workflow %s: %w", id, err)
	}
	if err := owned(a, w.OwnerID); err != nil {
		return w, fmt.Errorf("workflow %s: %w", id, err)
	}
	return w, nil
}

func publishWorkflow(ctx context.Context, w model.Workflow) {
	Publish(ctx, w.OwnerID, PushWorkflow, model.WireWorkflow(w, nil, false))
}

// requireCaller lets a person and the Super Agent use workflows.
func requireCaller(a model.Actor) error {
	if a.Kind != model.AuthorHuman && a.Kind != model.AuthorSuper {
		return errs.ErrForbidden
	}
	return nil
}

// scriptError makes a script error an invalid request; its text says where
// the script is wrong.
func scriptError(err error) error {
	if errors.Is(err, errs.ErrUnavailable) {
		return err
	}
	return fmt.Errorf("%w: %v", errs.ErrInvalid, err)
}
