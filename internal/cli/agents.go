package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

func table(w io.Writer) *tabwriter.Writer { return tabwriter.NewWriter(w, 0, 4, 2, ' ', 0) }

// agentCmd manages the agents. A person sets them up; the Super Agent
// reads the list to route jobs.
func agentCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "agent", Short: "Coding agents on your machines: set up, list, change"}
	cmd.AddCommand(agentListCmd(g), agentAddCmd(g), agentEditCmd(g), &cobra.Command{
		Use:   "rm <agent>",
		Short: "Remove an agent (its jobs stay)",
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
		Short: "List agents with their machine, driver and description",
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
				fmt.Fprintln(tw, "NAME\tMACHINE\tDRIVER\tONLINE\tDESCRIPTION")
				for _, a := range as {
					name := a.Name
					if a.Role == "super" {
						name += " (super)"
					}
					fmt.Fprintf(tw, "%s\t%s\t%s\t%t\t%s\n", name, a.Machine, a.Driver, a.Online, a.Description)
				}
				tw.Flush()
			})
		},
	}
}

func agentAddCmd(g *globals) *cobra.Command {
	var r protocol.CreateAgentRequest
	var driver string
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Set up an agent on a machine",
		Example: `  solax agent add reviewer --machine mac --driver claude --dir ~/src/app \
      --desc "Reviews Go code and finds bugs"
  solax agent add router --machine mac --driver pi --dir ~ --super`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			r.Name, r.Driver, r.Dir = args[0], ir.Kind(driver), expandDir(r.Dir)
			if r.Machine == "" {
				r.Machine = localMachine()
			}
			a, err := c.CreateAgent(cmd.Context(), r)
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), a, func(w io.Writer) {
				fmt.Fprintf(w, "agent %s on %s (%s)\n", a.Name, a.Machine, a.Driver)
				if !a.Online {
					fmt.Fprintln(w, "note: the machine is offline or has no working "+string(a.Driver)+" CLI yet")
				}
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&r.Machine, "machine", "", "machine ID or name (default: this machine)")
	f.StringVar(&driver, "driver", "", "claude, codex or pi")
	f.StringVar(&r.Description, "desc", "", "what this agent is good at; the Super Agent routes by it")
	f.StringVar(&r.Dir, "dir", "", "work folder on the machine")
	f.StringVar(&r.Model, "model", "", "model (default: the CLI default)")
	f.BoolVar(&r.Super, "super", false, "make this agent the Super Agent (the router)")
	_ = cmd.MarkFlagRequired("driver")
	_ = cmd.MarkFlagRequired("dir")
	return cmd
}

func agentEditCmd(g *globals) *cobra.Command {
	var desc, dir, model string
	var super bool
	cmd := &cobra.Command{
		Use:   "edit <agent>",
		Short: "Change the description, folder, model or role of an agent",
		Args:  cobra.ExactArgs(1),
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
			return g.print(cmd.OutOrStdout(), a, func(w io.Writer) { fmt.Fprintf(w, "updated %s\n", a.Name) })
		},
	}
	f := cmd.Flags()
	f.StringVar(&desc, "desc", "", "new description")
	f.StringVar(&dir, "dir", "", "new work folder")
	f.StringVar(&model, "model", "", "new model")
	f.BoolVar(&super, "super", false, "make it the Super Agent (--super=false to undo)")
	return cmd
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
