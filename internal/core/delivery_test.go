package core

import (
	"testing"
	"time"

	"github.com/austiecodes/solax/internal/errs"
)

func budgetTask(state TaskState) *Task {
	return &Task{
		State:     state,
		Budget:    DefaultBudget,
		CreatedAt: time.Unix(1_700_000_000, 0),
	}
}

func TestCheckBudget(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).Add(time.Minute)
	agentMsg := Message{Author: Author{Kind: AuthorAgent}}
	humanMsg := Message{Author: Author{Kind: AuthorHuman}}

	t.Run("within limits", func(t *testing.T) {
		got, err := CheckBudget(budgetTask(TaskRunning), agentMsg, 1, now)
		if err != nil || got != BudgetOK {
			t.Fatalf("got (%v, %v), want (BudgetOK, nil)", got, err)
		}
	})

	t.Run("turns over budget asks for the summary", func(t *testing.T) {
		tk := budgetTask(TaskRunning)
		tk.Spent.AgentTurns = tk.Budget.MaxAgentTurns
		if got, _ := CheckBudget(tk, agentMsg, 1, now); got != BudgetSummarize {
			t.Fatalf("got %v, want BudgetSummarize", got)
		}
	})

	t.Run("over budget while summarizing is exhausted", func(t *testing.T) {
		tk := budgetTask(TaskSummarizing)
		tk.Spent.AgentTurns = tk.Budget.MaxAgentTurns
		if got, _ := CheckBudget(tk, agentMsg, 1, now); got != BudgetExhausted {
			t.Fatalf("got %v, want BudgetExhausted", got)
		}
	})

	t.Run("wall time uses now, not the message", func(t *testing.T) {
		tk := budgetTask(TaskRunning)
		late := tk.CreatedAt.Add(tk.Budget.MaxWallTime + time.Second)
		m := Message{Author: Author{Kind: AuthorAgent}, CreatedAt: now} // an old message must not hide the overrun
		if got, _ := CheckBudget(tk, m, 1, late); got != BudgetSummarize {
			t.Fatalf("got %v, want BudgetSummarize", got)
		}
	})

	t.Run("hops without a human", func(t *testing.T) {
		tk := budgetTask(TaskRunning)
		tk.Spent.HopsWithoutHuman = tk.Budget.MaxHopsWithoutHuman
		if got, _ := CheckBudget(tk, agentMsg, 1, now); got != BudgetSummarize {
			t.Fatalf("agent message: got %v, want BudgetSummarize", got)
		}
		if got, _ := CheckBudget(tk, humanMsg, 1, now); got != BudgetOK {
			t.Fatalf("human message: got %v, want BudgetOK", got)
		}
	})

	t.Run("a closed task has no budget", func(t *testing.T) {
		if got, _ := CheckBudget(budgetTask(TaskDone), agentMsg, 1, now); got != BudgetOK {
			t.Fatalf("got %v, want BudgetOK", got)
		}
	})

	t.Run("invalid input", func(t *testing.T) {
		if _, err := CheckBudget(nil, agentMsg, 1, now); err != errs.ErrInvalid {
			t.Fatalf("nil task: err = %v, want ErrInvalid", err)
		}
	})
}

func TestScopeGuards(t *testing.T) {
	worker := Actor{Kind: AuthorAgent, ID: "agt_1", Workspace: "ws_1", Task: "tsk_1"}
	human := Actor{Kind: AuthorHuman}
	super := Actor{Kind: AuthorSuper}

	if err := requireManager(worker); err != errs.ErrForbidden {
		t.Fatalf("worker as manager: %v, want ErrForbidden", err)
	}
	for _, a := range []Actor{human, super} {
		if err := requireManager(a); err != nil {
			t.Fatalf("%s as manager: %v", a.Kind, err)
		}
	}

	cases := []struct {
		name string
		a    Actor
		ws   ID
		task ID
		want error
	}{
		{"own workspace and task", worker, "ws_1", "tsk_1", nil},
		{"own workspace, no task given", worker, "ws_1", "", nil},
		{"other workspace", worker, "ws_2", "tsk_1", errs.ErrForbidden},
		{"other task in own workspace", worker, "ws_1", "tsk_2", errs.ErrForbidden},
		{"human anywhere", human, "ws_9", "tsk_9", nil},
		{"super anywhere", super, "ws_9", "tsk_9", nil},
	}
	for _, c := range cases {
		if got := checkScope(c.a, c.ws, c.task); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
