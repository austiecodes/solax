package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/austiecodes/solax/internal/locale"
	"github.com/austiecodes/solax/pkg/protocol"
)

// projectCmd manages the projects: where agents work. A person sets them
// up; the Super Agent reads the list to choose where a task works.
func projectCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "project", Short: T("Projects where agents work: set up, list, change")}
	cmd.AddCommand(projectListCmd(g), projectAddCmd(g), projectEditCmd(g), &cobra.Command{
		Use:   "rm <project>",
		Short: T("Remove a project (its tasks and files stay)"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			return c.DeleteProject(cmd.Context(), args[0])
		},
	})
	return cmd
}

func projectListCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: T("List projects with their machine, folder and description"),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			ps, err := c.Projects(cmd.Context())
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), ps, func(w io.Writer) {
				tw := table(w)
				fmt.Fprintln(tw, T("NAME\tMACHINE\tFOLDER\tONLINE\tDESCRIPTION"))
				for _, p := range ps {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", p.Name, orDash(p.Machine), p.Path, locale.Bool(p.Online), p.Description)
				}
				tw.Flush()
			})
		},
	}
}

func projectAddCmd(g *globals) *cobra.Command {
	var r protocol.CreateProjectRequest
	cmd := &cobra.Command{
		Use:   "add <name> <folder>",
		Short: T("Set up a project: a folder on a machine"),
		Example: `  solax project add app ~/src/app --desc "The web app: Go API and React UI"
  solax project add infra /srv/infra --machine dev`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			r.Name, r.Path = args[0], expandDir(args[1])
			if r.Machine == "" {
				r.Machine = localMachine()
			}
			p, err := c.CreateProject(cmd.Context(), r)
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), p, func(w io.Writer) {
				fmt.Fprintf(w, T("project %s: %s on %s\n"), p.Name, p.Path, p.Machine)
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&r.Machine, "machine", "", T("machine ID or name (default: this machine)"))
	f.StringVar(&r.Description, "desc", "", T("what the project is; the Super Agent chooses projects by it"))
	return cmd
}

func projectEditCmd(g *globals) *cobra.Command {
	var name, desc, folder string
	cmd := &cobra.Command{
		Use:   "edit <project>",
		Short: T("Change the name, description or folder of a project"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.client()
			if err != nil {
				return err
			}
			var r protocol.UpdateProjectRequest
			f := cmd.Flags()
			if f.Changed("name") {
				r.Name = &name
			}
			if f.Changed("desc") {
				r.Description = &desc
			}
			if f.Changed("folder") {
				folder = expandDir(folder)
				r.Path = &folder
			}
			p, err := c.UpdateProject(cmd.Context(), args[0], r)
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), p, func(w io.Writer) { fmt.Fprintf(w, T("updated %s\n"), p.Name) })
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", T("new name"))
	f.StringVar(&desc, "desc", "", T("new description"))
	f.StringVar(&folder, "folder", "", T("new folder on the same machine"))
	return cmd
}
