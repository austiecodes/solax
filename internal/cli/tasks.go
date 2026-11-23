package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/protocol"
)

// taskCmd gives work to agents. The Super Agent uses these commands from
// its shell, so the text output is short and says what happens next.
func taskCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "task", Short: T("Give work to an agent; the result comes back later")}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "send <agent> <prompt>",
			Short: T("Give a task to an agent and return at once"),
			Long: T(`Send gives a task to an agent and returns the task ID at once. It does not
wait. When the agent finishes, the task is done and has a result. If the
Super Agent sent the task, the result comes back to it as a new message.`),
			Args: cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := g.client()
				if err != nil {
					return err
				}
				j, err := c.Dispatch(cmd.Context(), args[0], args[1])
				if err != nil {
					return err
				}
				return g.print(cmd.OutOrStdout(), j, func(w io.Writer) {
					fmt.Fprintf(w, T("task %s queued for agent %s. Do not wait for it: the result comes back when the task is done.\n"), j.ID, j.Agent)
				})
			},
		},
		&cobra.Command{
			Use:   "list",
			Short: T("List recent tasks"),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := g.client()
				if err != nil {
					return err
				}
				js, err := c.Tasks(cmd.Context(), 30)
				if err != nil {
					return err
				}
				return g.print(cmd.OutOrStdout(), js, func(w io.Writer) {
					tw := table(w)
					fmt.Fprintln(tw, T("ID\tAGENT\tSTATE\tAGE\tTITLE"))
					for _, j := range js {
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", j.ID, j.Agent, T(j.State), age(j.CreatedAt), j.Title)
					}
					tw.Flush()
				})
			},
		},
		&cobra.Command{
			Use:   "show <task>",
			Short: T("Show the state and result of a task"),
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := g.client()
				if err != nil {
					return err
				}
				j, err := c.Task(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return g.print(cmd.OutOrStdout(), j, func(w io.Writer) { printTask(w, j) })
			},
		},
		taskReplyCmd(g),
		&cobra.Command{
			Use:   "cancel <task>",
			Short: T("Stop a task"),
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := g.client()
				if err != nil {
					return err
				}
				j, err := c.CancelTask(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return g.print(cmd.OutOrStdout(), j, func(w io.Writer) { fmt.Fprintf(w, T("task %s: %s\n"), j.ID, T(j.State)) })
			},
		},
		taskEventsCmd(g),
	)
	return cmd
}

func taskEventsCmd(g *globals) *cobra.Command {
	var deltas bool
	return &cobra.Command{
		Use:   "events <task>",
		Short: T("Print the stored agent events of a task"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			j, err := c.Task(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			evs, err := c.SessionEvents(cmd.Context(), j.SessionID, 0)
			if err != nil {
				return err
			}
			for _, e := range evs {
				printEvent(cmd.OutOrStdout(), e, g.json, deltas)
			}
			return nil
		},
	}
}

func printTask(w io.Writer, j protocol.Task) {
	fmt.Fprintf(w, T("task:     %s\nagent:   %s\nstate:   %s\ncreated: %s\n"), j.ID, j.Agent, T(j.State), j.CreatedAt.Local().Format(time.DateTime))
	if j.Error != "" {
		fmt.Fprintf(w, T("error:   %s\n"), j.Error)
	}
	fmt.Fprintf(w, T("prompt:\n%s\n"), indent(j.Prompt))
	if j.Result != "" {
		fmt.Fprintf(w, T("result:\n%s\n"), indent(j.Result))
	}
}

func indent(s string) string { return "  " + strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n  ") }

func age(t time.Time) string {
	d := time.Since(t).Round(time.Second)
	switch {
	case d < time.Minute:
		return d.String()
	case d < time.Hour:
		return d.Round(time.Minute).String()
	}
	return d.Round(time.Hour).String()
}

// watchCmd prints the live stream: agent events, tasks and the Super Agent
// conversation.
func watchCmd(g *globals) *cobra.Command {
	var deltas bool
	cmd := &cobra.Command{
		Use:   "watch",
		Short: T("Follow all agent events, tasks and Super Agent messages live"),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			out := cmd.OutOrStdout()
			return follow(ctx, c, func(p protocol.Push) { printPush(out, p, g.json, deltas) })
		},
	}
	cmd.Flags().BoolVar(&deltas, "deltas", false, T("also print streamed deltas"))
	return cmd
}

// follow reads the stream and reconnects until ctx ends.
func follow(ctx context.Context, c *client.Client, fn func(protocol.Push)) error {
	for {
		err := c.Stream(ctx, fn)
		if ctx.Err() != nil {
			return nil
		}
		fmt.Fprintf(os.Stderr, T("stream: %v; reconnecting\n"), err)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(2 * time.Second):
		}
	}
}

func printPush(w io.Writer, p protocol.Push, asJSON, deltas bool) {
	if asJSON {
		b, _ := json.Marshal(p)
		fmt.Fprintln(w, string(b))
		return
	}
	switch p.Event {
	case "session.event":
		var n protocol.SessionEventNote
		if json.Unmarshal(p.Payload, &n) != nil {
			return
		}
		who := n.Agent
		if n.TaskID != "" {
			who += "/" + n.TaskID
		}
		var b strings.Builder
		printEvent(&b, n.Event, false, deltas)
		if b.Len() > 0 {
			fmt.Fprintf(w, "%-28s %s", short(who), b.String())
		}
	case "task":
		var j protocol.Task
		if json.Unmarshal(p.Payload, &j) == nil {
			fmt.Fprintf(w, "== task %s (%s): %s %s\n", j.ID, j.Agent, j.State, j.Error)
		}
	case "super.message":
		var m protocol.Message
		if json.Unmarshal(p.Payload, &m) == nil {
			printMessage(w, m)
		}
	default:
		fmt.Fprintf(w, "== %s\n", p.Event)
	}
}

func printMessage(w io.Writer, m protocol.Message) {
	who := map[string]string{"human": T("you"), "super": T("super"), "system": T("system")}[m.Author.Kind]
	if who == "" {
		who = m.Author.Kind
	}
	fmt.Fprintf(w, "[%s] %s\n", who, m.Text)
}

// chatCmd talks to the Super Agent from the terminal: each line is a
// message; replies and task notes are printed as they come.
func chatCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "chat [message]",
		Short: T("Talk to the Super Agent (each input line is one message)"),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			out := cmd.OutOrStdout()
			go func() {
				_ = follow(ctx, c, func(p protocol.Push) {
					switch p.Event {
					case "super.message":
						var m protocol.Message
						if json.Unmarshal(p.Payload, &m) == nil && m.Author.Kind != "human" {
							printMessage(out, m)
						}
					case "task":
						var j protocol.Task
						if json.Unmarshal(p.Payload, &j) == nil {
							fmt.Fprintf(out, T("  · task %s (%s): %s\n"), j.ID, j.Agent, T(j.State))
						}
					}
				})
			}()
			send := func(text string) error {
				if strings.TrimSpace(text) == "" {
					return nil
				}
				_, err := c.SendToSuper(ctx, text)
				return err
			}
			if len(args) > 0 {
				if err := send(strings.Join(args, " ")); err != nil {
					return err
				}
			}
			lines := make(chan string)
			go func() {
				sc := bufio.NewScanner(cmd.InOrStdin())
				for sc.Scan() {
					lines <- sc.Text()
				}
				if err := sc.Err(); err != nil {
					fmt.Fprintln(cmd.ErrOrStderr(), err)
				}
				close(lines)
			}()
			for {
				select {
				case <-ctx.Done():
					return nil
				case l, ok := <-lines:
					if !ok {
						<-ctx.Done() // keep printing replies until Ctrl-C
						return nil
					}
					if err := send(l); err != nil {
						fmt.Fprintln(cmd.ErrOrStderr(), T("send:"), err)
					}
				}
			}
		},
	}
}

func taskReplyCmd(g *globals) *cobra.Command {
	var steer bool
	cmd := &cobra.Command{
		Use:   "reply <task> <text>",
		Short: T("Send more input to the agent of a task (the task runs again)"),
		Long: T(`Send more input to the agent of a task.

By default the input starts a new turn after the current one, and its
result comes back to you. With --steer, the input goes into the turn that
runs now, before the next model call; the turn keeps its owner. A task that
does not run gets a steer as a normal reply.`),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			send := c.FollowUp
			if steer {
				send = c.Steer
			}
			j, err := send(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), j, func(w io.Writer) {
				if steer && j.State == "running" {
					fmt.Fprintf(w, T("steered task %s (%s).\n"), j.ID, j.Agent)
					return
				}
				fmt.Fprintf(w, T("sent to task %s (%s). The new result comes back when the agent is done.\n"), j.ID, j.Agent)
			})
		},
	}
	cmd.Flags().BoolVar(&steer, "steer", false, T("add the input to the running turn"))
	return cmd
}
