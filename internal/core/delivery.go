package core

import (
	"time"

	"github.com/austiecodes/solax/internal/errs"
)

// Wake is one session that a message must wake.
type Wake struct {
	AgentID ID
	Urgent  bool // steer instead of follow_up when the session is busy
}

// WakeTarget tells whether the Super Agent wakes or only gets a notice.
type WakeTarget struct {
	Members []Wake
	Super   bool // wake the Super Agent
	Notice  bool // send the Super Agent a note without a wake
}

// Wakes computes the wake set of message m in its task:
//
//   - The author never wakes.
//   - Mentioned members wake.
//   - The leader wakes for messages from a human, the Super Agent or the
//     system, and for reports.
//   - Other members do not wake. They get m in their next unread digest.
//   - The Super Agent wakes for summaries, blocked reports with a question,
//     and failed tasks. It gets a notice for other messages.
//
// It is a pure function, so tests can cover each rule.
func Wakes(m Message, ws Workspace, members []Member) (WakeTarget, error) {
	return WakeTarget{}, errs.ErrNotImplemented
}

// BudgetVerdict is the result of CheckBudget.
type BudgetVerdict int

const (
	BudgetOK        BudgetVerdict = iota
	BudgetSummarize               // tell the leader to write the summary now
	BudgetExhausted               // move the task to blocked and wake the Super Agent
)

// CheckBudget charges one wake against the task budget. It is pure: now
// comes from the caller, so a test clock gives stable results. The wall
// time runs from the creation of the task. Each message from an agent or
// the system spends one hop of the hops without a human; a message from a
// human resets the count.
//
// A task that is summarizing and goes over again is exhausted: the leader
// did not deliver the summary in time.
func CheckBudget(t *Task, m Message, wakes int, now time.Time) (BudgetVerdict, error) {
	if t == nil || wakes < 0 {
		return BudgetOK, errs.ErrInvalid
	}
	if t.State.Terminal() {
		return BudgetOK, nil
	}
	over := func() BudgetVerdict {
		if t.State == TaskSummarizing {
			return BudgetExhausted
		}
		return BudgetSummarize
	}
	if t.Budget.MaxAgentTurns > 0 && t.Spent.AgentTurns+wakes > t.Budget.MaxAgentTurns {
		return over(), nil
	}
	if t.Budget.MaxWallTime > 0 && now.Sub(t.CreatedAt) > t.Budget.MaxWallTime {
		return over(), nil
	}
	if m.Author.Kind == AuthorHuman {
		return BudgetOK, nil
	}
	if t.Budget.MaxHopsWithoutHuman > 0 && t.Spent.HopsWithoutHuman+1 > t.Budget.MaxHopsWithoutHuman {
		return over(), nil
	}
	return BudgetOK, nil
}

// RenderLimits is the Inline & Spill budget of one batch.
type RenderLimits struct {
	MaxTokens      int // whole batch, default 4000
	WakeMaxChars   int // the waking message, default 2000
	DigestMaxChars int // each digest line, default 200
}

// DefaultRenderLimits are the limits of a batch.
var DefaultRenderLimits = RenderLimits{MaxTokens: 4000, WakeMaxChars: 2000, DigestMaxChars: 200}

// RenderBatch renders the waking messages and the unread digest as text.
// When the batch is over budget, it adds a pointer such as
// "N more messages. Run: solax ws read --task tsk_x --since 1234".
func RenderBatch(wake []Message, unread []Message, lim RenderLimits) (string, error) {
	return "", errs.ErrNotImplemented
}
