package cli

import (
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

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/agent/driver"
	"github.com/austiecodes/solax/pkg/ir"
)

// tryCmd runs one local agent session without a server or daemon and
// prints its IR events. It is the quickest way to see how a driver maps
// the native stream of an agent.
func tryCmd(g *globals) *cobra.Command {
	var (
		dir, model, instructions string
		follow                   []string
		raw                      bool
	)
	cmd := &cobra.Command{
		Use:   "try <pi|codex|claude> <prompt>",
		Short: "Run one local agent session and print its unified events",
		Example: `  solax try claude "list the files here"
  solax try codex --dir ~/src/app "run the tests" --json
  solax try pi "say hi" --follow-up "now say bye"`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			drv, err := driver.Registry("").Get(ir.Kind(args[0]))
			if err != nil {
				return err
			}
			if dir == "" {
				if dir, err = os.Getwd(); err != nil {
					return err
				}
			}
			sess, err := drv.Start(ctx, agent.SessionSpec{
				SessionID: fmt.Sprintf("try_%d", time.Now().Unix()),
				Dir:       dir, Model: model, Instructions: instructions,
			})
			if err != nil {
				return err
			}
			defer sess.Close(context.WithoutCancel(ctx))

			inputs := append([]string{args[1]}, follow...)
			out := cmd.OutOrStdout()
			if err := send(ctx, sess, inputs[0]); err != nil {
				return err
			}
			inputs = inputs[1:]
			for {
				select {
				case <-ctx.Done():
					fmt.Fprintln(cmd.ErrOrStderr(), "interrupt: stopping the session")
					return nil
				case e, ok := <-sess.Events():
					if !ok {
						return nil
					}
					printEvent(out, e, g.json, raw)
					if e.Type != ir.TurnCompleted {
						continue
					}
					if len(inputs) == 0 {
						return nil
					}
					if err := send(ctx, sess, inputs[0]); err != nil {
						return err
					}
					inputs = inputs[1:]
				}
			}
		},
	}
	f := cmd.Flags()
	f.StringVar(&dir, "dir", "", "working folder of the agent (default: current folder)")
	f.StringVar(&model, "model", "", "model of the agent")
	f.StringVar(&instructions, "instructions", "", "text appended to the system prompt")
	f.StringArrayVar(&follow, "follow-up", nil, "another prompt, sent after the previous turn ends (repeatable)")
	f.BoolVar(&raw, "deltas", false, "also print streamed deltas")
	return cmd
}

func send(ctx context.Context, s agent.Session, text string) error {
	_, err := s.Send(ctx, ir.Input{Mode: ir.ModePrompt, Text: text})
	return err
}

// printEvent prints one IR event as JSON or as one short line.
func printEvent(w io.Writer, e ir.Event, asJSON, deltas bool) {
	if e.Type == ir.ItemDelta && !deltas {
		return
	}
	if asJSON {
		b, _ := json.Marshal(e)
		fmt.Fprintln(w, string(b))
		return
	}
	line := fmt.Sprintf("%4d %-16s", e.Seq, e.Type)
	switch {
	case e.Session != nil:
		line += fmt.Sprintf(" %s %s model=%s", e.Session.Ref.Kind, e.Session.Ref.NativeID, e.Session.Model)
		if e.Session.ExitCode != nil {
			line += fmt.Sprintf(" exit=%d %s", *e.Session.ExitCode, e.Session.Reason)
		}
	case e.Turn != nil:
		line += fmt.Sprintf(" %s%s", e.Turn.Cause, e.Turn.Status)
		if e.Turn.Error != nil {
			line += ": " + e.Turn.Error.Message
		}
	case e.Item != nil:
		line += fmt.Sprintf(" %-13s %-11s", e.Item.Kind, e.Item.Status)
		if t := e.Item.Tool; t != nil {
			line += fmt.Sprintf(" %s[%s] %s", t.Name, t.Class, short(string(t.Input)))
			if t.Output != "" {
				line += " → " + short(t.Output)
			}
		} else {
			line += " " + short(e.Item.Text)
		}
	case e.Delta != nil:
		line += fmt.Sprintf(" %s %q", e.Delta.Kind, e.Delta.Text)
	case e.Usage != nil:
		line += fmt.Sprintf(" in=%d out=%d cache=%d $%.4f", e.Usage.Input, e.Usage.Output, e.Usage.CacheRead, e.Usage.CostUSD)
	case e.Notice != nil:
		line += fmt.Sprintf(" [%s] %s: %s", e.Notice.Level, e.Notice.Code, e.Notice.Message)
	}
	fmt.Fprintln(w, line)
}

func short(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return s
}
