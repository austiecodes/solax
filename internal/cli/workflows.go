package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/austiecodes/solax/pkg/protocol"
)

func workflowCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "workflow", Short: T("Run TypeScript workflows that start and combine tasks on the server")}
	cmd.AddCommand(workflowRunCmd(g), workflowSaveCmd(g), workflowListCmd(g), workflowCancelCmd(g), workflowAnswerCmd(g))
	return cmd
}

func workflowRunCmd(g *globals) *cobra.Command {
	var name, args, resume string
	cmd := &cobra.Command{
		Use:   "run [file]",
		Short: T("Start a workflow script; it runs on the server and the result comes back later"),
		Long: T(`Start a workflow: a TypeScript file that exports
"async function run(wf, args)". "-" reads the script from stdin. The
command prints the workflow ID and returns at once; the server runs the
script and reports when it ends.

--name runs a saved workflow instead of a file. --resume reuses the
finished steps of an earlier run, for example after you fix the script.`),
		Example: `  solax workflow run review.ts --args '{"pkg":"internal/auth"}'
  solax workflow run - <<'EOF'
  export async function run(wf) { return (await wf.task("coder", "fix the build")).result; }
  EOF
  solax workflow run --name fix-until-pass --args '{"goal":"add tests"}'
  solax workflow run --resume wfl_01J... review.ts`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, a []string) error {
			r := protocol.StartWorkflowRequest{Name: name, Resume: resume}
			switch {
			case name != "" && len(a) > 0:
				return errors.New(T("give a file or --name, not both"))
			case name == "" && len(a) == 0:
				return errors.New(T("give a script file (\"-\": stdin) or --name"))
			case len(a) > 0:
				src, err := readSource(cmd, a[0])
				if err != nil {
					return err
				}
				r.Source = src
			}
			if args != "" {
				if !json.Valid([]byte(args)) {
					return errors.New(T("--args is not valid JSON"))
				}
				r.Args = json.RawMessage(args)
			}
			c, err := g.client()
			if err != nil {
				return err
			}
			w, err := c.StartWorkflow(cmd.Context(), r)
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), w, func(w2 io.Writer) {
				fmt.Fprintf(w2, T("workflow %s started (%s). Do not wait for it: the result comes back when it ends.\n"), w.ID, w.Title)
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", T("run the saved workflow with this name"))
	f.StringVar(&args, "args", "", T("the args of run(wf, args), as JSON"))
	f.StringVar(&resume, "resume", "", T("a workflow ID whose finished steps this run reuses"))
	return cmd
}

func workflowSaveCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "save <file>",
		Short: T("Keep a workflow script under its meta.name, to run again with --name"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, a []string) error {
			src, err := readSource(cmd, a[0])
			if err != nil {
				return err
			}
			c, err := g.client()
			if err != nil {
				return err
			}
			sw, err := c.SaveWorkflow(cmd.Context(), src)
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), sw, func(w io.Writer) {
				fmt.Fprintf(w, T("saved workflow %s.\n"), sw.Name)
			})
		},
	}
}

func workflowListCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "list [name | workflow]",
		Short: T("List saved workflows and recent runs, or show one of them"),
		Long: T(`With no argument, list the saved workflows and the recent runs. With a
workflow ID (wfl_...), show that run: its state, steps, log and result.
With a name, show that saved workflow: its description, source and runs.`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, a []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(a) == 0 {
				l, err := c.Workflows(cmd.Context())
				if err != nil {
					return err
				}
				return g.print(out, l, func(w io.Writer) { printWorkflowList(w, l) })
			}
			item, err := c.Workflow(cmd.Context(), a[0])
			if err != nil {
				return err
			}
			return g.print(out, item, func(w io.Writer) {
				if item.Run != nil {
					printRun(w, *item.Run)
				} else if item.Saved != nil {
					printSaved(w, *item.Saved, item.Runs)
				}
			})
		},
	}
}

func workflowCancelCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <workflow>",
		Short: T("Stop a workflow and its active tasks"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, a []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			w, err := c.CancelWorkflow(cmd.Context(), a[0])
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), w, func(w2 io.Writer) {
				fmt.Fprintf(w2, T("workflow %s is %s.\n"), w.ID, T(w.State))
			})
		},
	}
}

func workflowAnswerCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "answer <workflow> <choice> [text]",
		Short: T("Answer the question of a workflow (wf.ask)"),
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, a []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			text := ""
			if len(a) == 3 {
				text = a[2]
			}
			w, err := c.AnswerWorkflow(cmd.Context(), a[0], a[1], text)
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), w, func(w2 io.Writer) {
				fmt.Fprintf(w2, T("answered workflow %s; it goes on.\n"), w.ID)
			})
		},
	}
}

// readSource reads a script file; "-" is stdin.
func readSource(cmd *cobra.Command, path string) (string, error) {
	var (
		b   []byte
		err error
	)
	if path == "-" {
		b, err = io.ReadAll(cmd.InOrStdin())
	} else {
		b, err = os.ReadFile(path)
	}
	return string(b), err
}

func printWorkflowList(w io.Writer, l protocol.WorkflowList) {
	fmt.Fprintln(w, T("Saved workflows:"))
	if len(l.Saved) == 0 {
		fmt.Fprintln(w, T("  (none)"))
	} else {
		tw := table(w)
		fmt.Fprintln(tw, T("NAME\tDESCRIPTION"))
		for _, s := range l.Saved {
			fmt.Fprintf(tw, "%s\t%s\n", s.Name, s.Description)
		}
		tw.Flush()
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, T("Recent runs:"))
	if len(l.Runs) == 0 {
		fmt.Fprintln(w, T("  (none)"))
		return
	}
	tw := table(w)
	fmt.Fprintln(tw, T("ID\tSTATE\tAGE\tTITLE"))
	for _, r := range l.Runs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.ID, T(r.State), age(r.CreatedAt), r.Title)
	}
	tw.Flush()
}

func printRun(w io.Writer, r protocol.Workflow) {
	fmt.Fprintf(w, T("workflow %s (%s): %s\n"), r.ID, r.Title, T(r.State))
	if r.ResumeFrom != "" {
		fmt.Fprintf(w, T("resumes: %s\n"), r.ResumeFrom)
	}
	if len(r.Steps) > 0 {
		fmt.Fprintln(w)
		tw := table(w)
		fmt.Fprintln(tw, T("STEP\tKIND\tSTATE\tPHASE\tROUND\tTARGET"))
		for _, s := range r.Steps {
			target := s.Target
			if s.Kind == "ask" {
				target = ""
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", first(s.Label, s.Key), s.Kind, T(s.State), s.Phase, roundText(s), target)
		}
		tw.Flush()
	}
	if r.Logs != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, T("log:"))
		for _, line := range strings.Split(r.Logs, "\n") {
			fmt.Fprintln(w, "  "+line)
		}
	}
	if r.Error != "" {
		fmt.Fprintln(w)
		fmt.Fprintf(w, T("error: %s\n"), r.Error)
	}
	if len(r.Result) > 0 && string(r.Result) != "null" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, T("result:"))
		fmt.Fprintln(w, resultText(r.Result))
	}
}

// resultText shows a JSON string result as plain text, and other JSON as
// it is.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

func printSaved(w io.Writer, s protocol.SavedWorkflow, runs []protocol.Workflow) {
	fmt.Fprintf(w, T("saved workflow %s: %s\n"), s.Name, s.Description)
	if s.Meta.WhenToUse != "" {
		fmt.Fprintf(w, T("when to use: %s\n"), s.Meta.WhenToUse)
	}
	for _, p := range s.Meta.Phases {
		fmt.Fprintf(w, T("phase: %s %s\n"), p.Title, p.Detail)
	}
	if len(runs) > 0 {
		fmt.Fprintln(w)
		tw := table(w)
		fmt.Fprintln(tw, T("ID\tSTATE\tAGE\tTITLE"))
		for _, r := range runs {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.ID, T(r.State), age(r.CreatedAt), r.Title)
		}
		tw.Flush()
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, T("source:"))
	fmt.Fprintln(w, s.Source)
}

// roundText is the loop round of a step, as "fix 2/5"; "" outside a loop.
func roundText(s protocol.WorkflowStep) string {
	switch {
	case s.Loop == "":
		return ""
	case s.MaxRounds > 0:
		return fmt.Sprintf("%s %d/%d", s.Loop, s.Round, s.MaxRounds)
	}
	return fmt.Sprintf("%s %d", s.Loop, s.Round)
}
