package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/austiecodes/solax/internal/locale"
	"github.com/austiecodes/solax/pkg/discover"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// agentCmd manages the agents. A person sets them up; the Super Agent
// reads the list to route jobs.
func agentCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "agent", Short: T("Coding agents on your machines: set up, list, change")}
	cmd.AddCommand(agentListCmd(g), agentAddCmd(g), agentDiscoverCmd(g), agentEditCmd(g), &cobra.Command{
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
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: T("Set up an agent on a machine"),
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
	_ = cmd.MarkFlagRequired("driver")
	_ = cmd.MarkFlagRequired("dir")
	return cmd
}

// agentDiscoverCmd lists the agent CLIs that the daemons found and that
// no agent uses yet, and sets them up as agents.
func agentDiscoverCmd(g *globals) *cobra.Command {
	var (
		add, noSuper bool
		dir          string
	)
	cmd := &cobra.Command{
		Use:   "discover [name...]",
		Short: T("Find the agent CLIs on your machines that no agent uses yet, and add them"),
		Long: T(`List an agent for each agent CLI that the daemon of an online machine
found and that no agent of that machine uses yet: claude, codex, pi
and the forks of Claude Code in drivers.claude_compatible of config.yaml.

With --add, set them up: all of them, or the ones named. With no Super
Agent yet, one of them becomes the Super Agent (pi first), unless
--no-super. A daemon looks for new CLIs every 30 seconds; run
"solax daemon reload" to look at once.`),
		Example: `  solax agent discover
  solax agent discover --add
  solax agent discover --add claude codex --dir ~/src/app`,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			machines, err := c.Machines(ctx)
			if err != nil {
				return err
			}
			agents, err := c.Agents(ctx)
			if err != nil {
				return err
			}
			found := discover.Suggest(machines, agents)
			if noSuper {
				for i := range found {
					found[i].Super = false
				}
			}
			if len(args) > 0 {
				if !add {
					return errors.New(T("name agents only with --add"))
				}
				if found, err = pick(found, args); err != nil {
					return err
				}
			}
			out := cmd.OutOrStdout()
			if !add {
				return g.print(out, found, func(w io.Writer) { printSuggestions(w, found) })
			}
			var made []protocol.Agent
			for _, s := range found {
				a, err := c.CreateAgent(ctx, s.Request(expandDir(dir), ""))
				if err != nil {
					return fmt.Errorf(T("add %s: %w"), s.Name, err)
				}
				made = append(made, a)
			}
			return g.print(out, made, func(w io.Writer) {
				if len(made) == 0 {
					fmt.Fprintln(w, T("nothing to add: each agent CLI that the daemons found has an agent"))
				}
				for _, a := range made {
					if a.Role == "super" {
						fmt.Fprintf(w, T("added %s (%s on %s) as the Super Agent\n"), a.Name, a.Driver, a.Machine)
					} else {
						fmt.Fprintf(w, T("added %s (%s on %s)\n"), a.Name, a.Driver, a.Machine)
					}
				}
			})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&add, "add", false, T("set the agents up"))
	f.StringVar(&dir, "dir", discover.DefaultDir, T("work folder of the new agents"))
	f.BoolVar(&noSuper, "no-super", false, T("do not make one of them the Super Agent"))
	return cmd
}

// pick keeps the suggestions with the given names or drivers, in order.
func pick(found []discover.Suggestion, names []string) ([]discover.Suggestion, error) {
	var out []discover.Suggestion
	for _, n := range names {
		i := slices.IndexFunc(found, func(s discover.Suggestion) bool { return s.Name == n || string(s.Driver) == n })
		if i < 0 {
			return nil, fmt.Errorf(T("%s: no such agent CLI to add; run \"solax agent discover\" to list them"), n)
		}
		out = append(out, found[i])
		found = slices.Delete(found, i, i+1)
	}
	return out, nil
}

func printSuggestions(w io.Writer, found []discover.Suggestion) {
	if len(found) == 0 {
		fmt.Fprintln(w, T("No new agent CLIs: each one that the daemons found has an agent."))
		return
	}
	tw := table(w)
	fmt.Fprintln(tw, T("NAME\tDRIVER\tVERSION\tMACHINE\tROLE"))
	for _, s := range found {
		role := T("worker")
		if s.Super {
			role = T("super")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", s.Name, s.Driver, s.Version, s.Machine, role)
	}
	tw.Flush()
	fmt.Fprintln(w, T("\nAdd them with: solax agent discover --add [name...]"))
}

func agentEditCmd(g *globals) *cobra.Command {
	var desc, dir, model string
	var super bool
	cmd := &cobra.Command{
		Use:   "edit <agent>",
		Short: T("Change the description, folder, model or role of an agent"),
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
			return g.print(cmd.OutOrStdout(), a, func(w io.Writer) { fmt.Fprintf(w, T("updated %s\n"), a.Name) })
		},
	}
	f := cmd.Flags()
	f.StringVar(&desc, "desc", "", T("new description"))
	f.StringVar(&dir, "dir", "", T("new work folder"))
	f.StringVar(&model, "model", "", T("new model"))
	f.BoolVar(&super, "super", false, T("make it the Super Agent (--super=false to undo)"))
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
