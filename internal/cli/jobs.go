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

// jobCmd gives work to agents. The Super Agent uses these commands from
// its shell, so the text output is short and says what happens next.
func jobCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "job", Short: "Give work to an agent; the result comes back later"}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "send <agent> <prompt>",
			Short: "Give a job to an agent and return at once",
			Long: `Send gives a job to an agent and returns the job ID at once. It does not
wait. When the agent finishes, the job is done and has a result. If the
Super Agent sent the job, the result comes back to it as a new message.`,
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
					fmt.Fprintf(w, "job %s queued for agent %s. Do not wait for it: the result comes back when the job is done.\n", j.ID, j.Agent)
				})
			},
		},
		&cobra.Command{
			Use:   "list",
			Short: "List recent jobs",
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := g.client()
				if err != nil {
					return err
				}
				js, err := c.Jobs(cmd.Context(), 30)
				if err != nil {
					return err
				}
				return g.print(cmd.OutOrStdout(), js, func(w io.Writer) {
					tw := table(w)
					fmt.Fprintln(tw, "ID\tAGENT\tSTATE\tAGE\tTITLE")
					for _, j := range js {
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", j.ID, j.Agent, j.State, age(j.CreatedAt), j.Title)
					}
					tw.Flush()
				})
			},
		},
		&cobra.Command{
			Use:   "show <job>",
			Short: "Show the state and result of a job",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := g.client()
				if err != nil {
					return err
				}
				j, err := c.Job(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return g.print(cmd.OutOrStdout(), j, func(w io.Writer) { printJob(w, j) })
			},
		},
		&cobra.Command{
			Use:   "reply <job> <text>",
			Short: "Send more input to the agent of a job (the job runs again)",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := g.client()
				if err != nil {
					return err
				}
				j, err := c.FollowUp(cmd.Context(), args[0], args[1])
				if err != nil {
					return err
				}
				return g.print(cmd.OutOrStdout(), j, func(w io.Writer) {
					fmt.Fprintf(w, "sent to job %s (%s). The new result comes back when the agent is done.\n", j.ID, j.Agent)
				})
			},
		},
		&cobra.Command{
			Use:   "cancel <job>",
			Short: "Stop a job",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := g.client()
				if err != nil {
					return err
				}
				j, err := c.CancelJob(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return g.print(cmd.OutOrStdout(), j, func(w io.Writer) { fmt.Fprintf(w, "job %s: %s\n", j.ID, j.State) })
			},
		},
		jobEventsCmd(g),
	)
	return cmd
}

func jobEventsCmd(g *globals) *cobra.Command {
	var deltas bool
	return &cobra.Command{
		Use:   "events <job>",
		Short: "Print the stored agent events of a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			j, err := c.Job(cmd.Context(), args[0])
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

func printJob(w io.Writer, j protocol.Job) {
	fmt.Fprintf(w, "job:     %s\nagent:   %s\nstate:   %s\ncreated: %s\n", j.ID, j.Agent, j.State, j.CreatedAt.Local().Format(time.DateTime))
	if j.Error != "" {
		fmt.Fprintf(w, "error:   %s\n", j.Error)
	}
	fmt.Fprintf(w, "prompt:\n%s\n", indent(j.Prompt))
	if j.Result != "" {
		fmt.Fprintf(w, "result:\n%s\n", indent(j.Result))
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

// watchCmd prints the live stream: agent events, jobs and the Super Agent
// conversation.
func watchCmd(g *globals) *cobra.Command {
	var deltas bool
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Follow all agent events, jobs and Super Agent messages live",
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
	cmd.Flags().BoolVar(&deltas, "deltas", false, "also print streamed deltas")
	return cmd
}

// follow reads the stream and reconnects until ctx ends.
func follow(ctx context.Context, c *client.Client, fn func(protocol.Push)) error {
	for {
		err := c.Stream(ctx, fn)
		if ctx.Err() != nil {
			return nil
		}
		fmt.Fprintf(os.Stderr, "stream: %v; reconnecting\n", err)
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
		if n.JobID != "" {
			who += "/" + n.JobID
		}
		var b strings.Builder
		printEvent(&b, n.Event, false, deltas)
		if b.Len() > 0 {
			fmt.Fprintf(w, "%-28s %s", short(who), b.String())
		}
	case "job":
		var j protocol.Job
		if json.Unmarshal(p.Payload, &j) == nil {
			fmt.Fprintf(w, "== job %s (%s): %s %s\n", j.ID, j.Agent, j.State, j.Error)
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
	who := map[string]string{"human": "you", "super": "super", "system": "system"}[m.Author.Kind]
	if who == "" {
		who = m.Author.Kind
	}
	fmt.Fprintf(w, "[%s] %s\n", who, m.Text)
}

// chatCmd talks to the Super Agent from the terminal: each line is a
// message; replies and job notes are printed as they come.
func chatCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "chat [message]",
		Short: "Talk to the Super Agent (each input line is one message)",
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
					case "job":
						var j protocol.Job
						if json.Unmarshal(p.Payload, &j) == nil {
							fmt.Fprintf(out, "  · job %s (%s): %s\n", j.ID, j.Agent, j.State)
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
						fmt.Fprintln(cmd.ErrOrStderr(), "send:", err)
					}
				}
			}
		},
	}
}
