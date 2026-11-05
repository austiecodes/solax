package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func machineCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "machine", Short: "Machines that run a daemon"}
	cmd.AddCommand(&cobra.Command{
		Use:   "approve <pair-code>",
		Short: "Approve a pending machine by the code that its daemon printed",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			m, err := c.ApproveMachine(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), m, func(w io.Writer) { fmt.Fprintf(w, "approved %s (%s)\n", m.Name, m.ID) })
		},
	}, &cobra.Command{
		Use:   "revoke <machine-id>",
		Short: "Make the token of a machine invalid",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			return c.RevokeMachine(cmd.Context(), args[0])
		},
	}, &cobra.Command{
		Use:   "list",
		Short: "List machines",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			ms, err := c.Machines(cmd.Context())
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), ms, func(w io.Writer) {
				for _, m := range ms {
					fmt.Fprintf(w, "%s\t%s\t%s\tonline=%t\n", m.ID, m.Name, m.State, m.Online)
				}
			})
		},
	})
	return cmd
}

func agentCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "agent", Short: "Agents of all machines, as the server knows them"}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List agents",
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
				for _, a := range as {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.ID, a.Name, a.MachineID, a.Driver)
				}
			})
		},
	})
	return cmd
}

func recallCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "recall <id>",
		Short: "Recall any object by ID, with the IDs of related objects",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			o, err := c.Recall(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), o, func(w io.Writer) {
				fmt.Fprintf(w, "%s (%s)\n%s\n", o.ID, o.Type, o.Summary)
				for rel, id := range o.Links {
					fmt.Fprintf(w, "  %s: %s\n", rel, id)
				}
			})
		},
	}
}
