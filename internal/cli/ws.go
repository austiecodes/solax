package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/austiecodes/solax/pkg/protocol"
)

func wsCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "ws", Short: "Workspaces and their messages"}
	cmd.AddCommand(
		wsCreateCmd(g),
		wsListCmd(g),
		stub("show <ws>", "Show a workspace"),
		wsAddCmd(g),
		stub("remove <ws> <agent@machine>", "Remove an agent from a workspace"),
		wsSendCmd(g),
		wsReadCmd(g),
	)
	return cmd
}

func wsCreateCmd(g *globals) *cobra.Command {
	var leader string
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			w, err := c.CreateWorkspace(cmd.Context(), protocol.CreateWorkspaceRequest{Name: args[0], Leader: leader})
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), w, func(out io.Writer) { fmt.Fprintln(out, w.ID) })
		},
	}
	cmd.Flags().StringVar(&leader, "leader", "", "leader agent (agent@machine)")
	return cmd
}

func wsListCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List workspaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			ws, err := c.Workspaces(cmd.Context())
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), ws, func(out io.Writer) {
				for _, w := range ws {
					fmt.Fprintf(out, "%s\t%s\tmembers=%d\n", w.ID, w.Name, len(w.Members))
				}
			})
		},
	}
}

func wsAddCmd(g *globals) *cobra.Command {
	var leader bool
	cmd := &cobra.Command{
		Use:   "add <ws> <agent@machine>",
		Short: "Add an agent to a workspace",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			return c.AddMember(cmd.Context(), args[0], protocol.AddMemberRequest{Agent: args[1], Leader: leader})
		},
	}
	cmd.Flags().BoolVar(&leader, "leader", false, "make the agent the leader")
	return cmd
}

func wsSendCmd(g *globals) *cobra.Command {
	var (
		ws, task, kind, status string
		to                     []string
		checkpoint             bool
	)
	cmd := &cobra.Command{
		Use:   "send [text | -]",
		Short: "Post a message to a workspace task",
		Long:  "Post a message. The author comes from the token. Use - to read the text from stdin.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := args[0]
			if text == "-" {
				b, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return err
				}
				text = string(b)
			}
			c, err := g.client()
			if err != nil {
				return err
			}
			m, err := c.PostMessage(cmd.Context(), first(ws, os.Getenv(EnvWorkspace)), protocol.PostMessageRequest{
				TaskID:     first(task, os.Getenv(EnvTask)),
				Mentions:   trimAt(to),
				Kind:       kind,
				Status:     status,
				Checkpoint: checkpoint,
				Text:       text,
			})
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), m, func(out io.Writer) { fmt.Fprintf(out, "%s seq=%d\n", m.ID, m.Seq) })
		},
	}
	f := cmd.Flags()
	f.StringVar(&ws, "ws", "", "workspace (default $"+EnvWorkspace+")")
	f.StringVar(&task, "task", "", "task (default $"+EnvTask+", else the general task)")
	f.StringSliceVar(&to, "to", nil, "agents to mention, for example @backend,@infra")
	f.StringVar(&kind, "kind", "chat", "chat | report | summary")
	f.StringVar(&status, "status", "", "report status: progress | done | blocked | failed")
	f.BoolVar(&checkpoint, "checkpoint", false, "summary only: keep the task running")
	return cmd
}

func wsReadCmd(g *globals) *cobra.Command {
	var (
		ws, task string
		since    int64
		limit    int
	)
	cmd := &cobra.Command{
		Use:   "read",
		Short: "Read messages of a workspace task",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			ms, err := c.ReadMessages(cmd.Context(), first(ws, os.Getenv(EnvWorkspace)), protocol.ReadMessagesQuery{
				TaskID: first(task, os.Getenv(EnvTask)), Since: since, Limit: limit,
			})
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), ms, func(out io.Writer) {
				for _, m := range ms {
					fmt.Fprintf(out, "#%d %s %s/%s: %s\n", m.Seq, m.ID, m.Author.Kind, m.Author.ID, m.Text)
				}
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&ws, "ws", "", "workspace (default $"+EnvWorkspace+")")
	f.StringVar(&task, "task", "", "task (default $"+EnvTask+")")
	f.Int64Var(&since, "since", 0, "only messages with seq > since")
	f.IntVar(&limit, "limit", 50, "maximum number of messages")
	return cmd
}

func trimAt(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, strings.TrimPrefix(n, "@"))
	}
	return out
}
