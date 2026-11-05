package core

import "github.com/austiecodes/solax/internal/errs"

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

// CheckBudget charges one wake against the task budget.
func CheckBudget(t *Task, m Message, wakes int) (BudgetVerdict, error) {
	return BudgetOK, errs.ErrNotImplemented
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
