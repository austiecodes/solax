package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/austiecodes/solax/internal/locale"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// agentCmd manages the agents. A person sets them up; the Super Agent
// reads the list to route jobs.
func agentCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "agent", Short: T("Coding agents on your machines: set up, list, change")}
	cmd.AddCommand(agentListCmd(g), agentAddCmd(g), agentEditCmd(g), &cobra.Command{
		Use:   "rm <agent>",
		Short: T("Remove an agent (its jobs stay)"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			return c.DeleteAgent(cmd.Context(), args[0])
		},
	})
	return cmd
}

func agentListCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: T("List agents with their machine, driver and description"),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			as, err := c.Agents(cmd.Context())
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), as, func(w io.Writer) {
				tw := table(w)
				fmt.Fprintln(tw, T("NAME\tMACHINE\tDRIVER\tONLINE\tDESCRIPTION"))
				for _, a := range as {
					name := a.Name
					if a.Role == "super" {
						name += T(" (super)")
					}
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", name, a.Machine, a.Driver, locale.Bool(a.Online), a.Description)
				}
				tw.Flush()
			})
		},
	}
}

func agentAddCmd(g *globals) *cobra.Command {
	var r protocol.CreateAgentRequest
	var driver string
	var instr instructionsFlags
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: T("Set up an agent on a machine"),
		Example: `  solax agent add reviewer --machine mac --driver claude --dir ~/src/app \
      --desc "Reviews Go code and finds bugs" --instructions-file review.md
  solax agent add router --machine mac --driver pi --dir ~ --super`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			r.Name, r.Driver, r.Dir = args[0], ir.Kind(driver), expandDir(r.Dir)
			if r.Instructions, _, err = instr.read(cmd); err != nil {
				return err
			}
			if r.Machine == "" {
				r.Machine = localMachine()
			}
			a, err := c.CreateAgent(cmd.Context(), r)
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), a, func(w io.Writer) {
				fmt.Fprintf(w, T("agent %s on %s (%s)\n"), a.Name, a.Machine, a.Driver)
				if !a.Online {
					fmt.Fprintf(w, T("note: the machine is offline or has no working %s CLI yet\n"), a.Driver)
				}
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&r.Machine, "machine", "", T("machine ID or name (default: this machine)"))
	f.StringVar(&driver, "driver", "", T("claude, codex, pi, or a fork of Claude Code that the machine reports"))
	f.StringVar(&r.Description, "desc", "", T("what this agent is good at; the Super Agent routes by it"))
	f.StringVar(&r.Dir, "dir", "", T("work folder on the machine"))
	f.StringVar(&r.Model, "model", "", T("model (default: the CLI default)"))
	f.BoolVar(&r.Super, "super", false, T("make this agent the Super Agent (the router)"))
	instr.add(cmd)
	_ = cmd.MarkFlagRequired("driver")
	_ = cmd.MarkFlagRequired("dir")
	return cmd
}

func agentEditCmd(g *globals) *cobra.Command {
	var desc, dir, model string
	var super bool
	var instr instructionsFlags
	cmd := &cobra.Command{
		Use:   "edit <agent>",
		Short: T("Change the description, instructions, folder, model or role of an agent"),
		Example: `  solax agent edit reviewer --instructions-file review.md
  solax agent edit reviewer --instructions ""   # remove the instructions`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			var r protocol.UpdateAgentRequest
			f := cmd.Flags()
			if f.Changed("desc") {
				r.Description = &desc
			}
			if text, ok, err := instr.read(cmd); err != nil {
				return err
			} else if ok {
				r.Instructions = &text
			}
			if f.Changed("dir") {
				r.Dir = &dir
			}
			if f.Changed("model") {
				r.Model = &model
			}
			if f.Changed("super") {
				r.Super = &super
			}
			a, err := c.UpdateAgent(cmd.Context(), args[0], r)
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), a, func(w io.Writer) { fmt.Fprintf(w, T("updated %s\n"), a.Name) })
		},
	}
	f := cmd.Flags()
	f.StringVar(&desc, "desc", "", T("new description"))
	f.StringVar(&dir, "dir", "", T("new work folder"))
	f.StringVar(&model, "model", "", T("new model"))
	f.BoolVar(&super, "super", false, T("make it the Super Agent (--super=false to undo)"))
	instr.add(cmd)
	return cmd
}

// instructionsFlags are --instructions and --instructions-file: the system
// prompt of an agent, which its sessions get after the Solax role prompt.
type instructionsFlags struct{ text, file string }

func (fl *instructionsFlags) add(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&fl.text, "instructions", "", T("instructions of the agent: its system prompt, after the Solax role prompt"))
	f.StringVar(&fl.file, "instructions-file", "", T(`read the instructions from a file ("-": stdin)`))
	cmd.MarkFlagsMutuallyExclusive("instructions", "instructions-file")
}

// read returns the instructions that the flags give, and false when the
// command line has neither flag.
func (fl *instructionsFlags) read(cmd *cobra.Command) (string, bool, error) {
	f := cmd.Flags()
	switch {
	case f.Changed("instructions"):
		return fl.text, true, nil
	case !f.Changed("instructions-file"):
		return "", false, nil
	}
	var (
		b   []byte
		err error
	)
	if fl.file == "-" {
		b, err = io.ReadAll(cmd.InOrStdin())
	} else {
		b, err = os.ReadFile(fl.file)
	}
	if err != nil {
		return "", false, err
	}
	return string(b), true, nil
}

// localMachine returns the machine ID of this machine, if it is set up.
func localMachine() string {
	p, err := paths()
	if err != nil {
		return ""
	}
	creds, err := p.LoadCredentials()
	if err != nil {
		return ""
	}
	return creds.MachineID
}

// expandDir makes a relative folder absolute; "~" stays for the daemon.
func expandDir(d string) string {
	if d == "" || strings.HasPrefix(d, "~") || filepath.IsAbs(d) {
		return d
	}
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, d)
	}
	return d
}
