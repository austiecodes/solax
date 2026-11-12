package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/austiecodes/solax/pkg/protocol"
)

func taskCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "task", Short: "Tasks in workspaces"}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "create <ws> <brief>",
			Short: "Create a task. This wakes the leader",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := g.client()
				if err != nil {
					return err
				}
				t, err := c.CreateTask(cmd.Context(), args[0], protocol.CreateTaskRequest{Brief: args[1]})
				if err != nil {
					return err
				}
				return g.print(cmd.OutOrStdout(), t, func(w io.Writer) { fmt.Fprintln(w, t.ID) })
			},
		},
		taskAction(g, "show", "Show a task"),
		taskAction(g, "cancel", "Cancel a task"),
		taskAction(g, "summarize", "Ask the leader for a summary"),
		taskListCmd(g),
	)
	return cmd
}

func taskListCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "list <ws>",
		Short: "List the tasks of a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			ts, err := c.Tasks(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), ts, func(w io.Writer) {
				for _, t := range ts {
					fmt.Fprintf(w, "%s\t%s\t%s\n", t.ID, t.State, t.Title)
				}
			})
		},
	}
}

func taskAction(g *globals, name, short string) *cobra.Command {
	return &cobra.Command{
		Use:   name + " <task>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			switch name {
			case "show":
				t, err := c.Task(ctx, args[0])
				if err != nil {
					return err
				}
				return g.print(cmd.OutOrStdout(), t, func(w io.Writer) {
					fmt.Fprintf(w, "%s\t%s\t%s\n", t.ID, t.State, t.Title)
				})
			case "cancel":
				return c.CancelTask(ctx, args[0])
			default:
				return c.SummarizeTask(ctx, args[0])
			}
		},
	}
}
