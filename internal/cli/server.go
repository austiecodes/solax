package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/server"
)

// EnvSecret overrides the secret that signs agent tokens.
const EnvSecret = "SOLAX_SECRET"

// serverCmd runs and manages a Solax server. The init and token commands
// open the database directly, so they run on the server host and need no
// token.
func serverCmd() *cobra.Command {
	var cfg server.Config
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run and manage a Solax server",
		Long: `The server owns all state. Clients and daemons connect to it.

On a laptop, the server and the daemon can run on the same machine.
The server files are in ~/.solax/server/ unless you set --db.`,
	}
	cmd.PersistentFlags().StringVar(&cfg.DBPath, "db", "", "SQLite database path (default ~/.solax/server/solax.db)")
	cmd.PersistentPreRunE = func(*cobra.Command, []string) error {
		if cfg.DBPath == "" {
			p, err := paths()
			if err != nil {
				return err
			}
			cfg.DBPath = p.ServerDB()
		}
		if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o700); err != nil {
			return err
		}
		var err error
		cfg.Secret, err = server.LoadSecret(os.Getenv(EnvSecret), cfg.DBPath+".secret")
		return err
	}
	cmd.AddCommand(serverRunCmd(&cfg), serverInitCmd(&cfg), serverTokenCmd(&cfg))
	return cmd
}

func serverRunCmd(cfg *server.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the server in the foreground",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return server.Run(ctx, *cfg, slog.New(slog.NewTextHandler(os.Stderr, nil)))
		},
	}
	cmd.Flags().StringVar(&cfg.Addr, "addr", ":8740", "listen address")
	return cmd
}

func serverInitCmd(cfg *server.Config) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create the owner and the first client token",
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := server.Init(cmd.Context(), *cfg, name)
			if err != nil {
				return err
			}
			printToken(cmd, token)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "admin", "name of the first client token")
	return cmd
}

func serverTokenCmd(cfg *server.Config) *cobra.Command {
	cmd := &cobra.Command{Use: "token", Short: "Manage client tokens"}

	var name string
	create := &cobra.Command{
		Use:   "create",
		Short: "Create a client token for one CLI or desktop app",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, closeDB, err := server.Admin(cmd.Context(), *cfg)
			if err != nil {
				return err
			}
			defer closeDB()
			token, _, err := svc.CreateClient(cmd.Context(), "", name)
			if err != nil {
				return err
			}
			printToken(cmd, token)
			return nil
		},
	}
	create.Flags().StringVar(&name, "name", "", "client name, for example macbook-cli")
	_ = create.MarkFlagRequired("name")

	list := &cobra.Command{
		Use:   "list",
		Short: "List client tokens",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, closeDB, err := server.Admin(cmd.Context(), *cfg)
			if err != nil {
				return err
			}
			defer closeDB()
			ts, err := svc.Clients(cmd.Context(), "")
			if err != nil {
				return err
			}
			tw := table(cmd.OutOrStdout())
			fmt.Fprintln(tw, "ID\tNAME\tREVOKED\tLAST USED")
			for _, t := range ts {
				used := "never"
				if !t.LastUsed.IsZero() {
					used = t.LastUsed.Format("2006-01-02 15:04")
				}
				fmt.Fprintf(tw, "%s\t%s\t%t\t%s\n", t.ID, t.Name, t.Revoked, used)
			}
			return tw.Flush()
		},
	}

	revoke := &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke a client token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, closeDB, err := server.Admin(cmd.Context(), *cfg)
			if err != nil {
				return err
			}
			defer closeDB()
			return svc.RevokeClient(cmd.Context(), core.ID(args[0]))
		},
	}

	cmd.AddCommand(create, list, revoke)
	return cmd
}

func printToken(cmd *cobra.Command, token string) {
	fmt.Fprintln(cmd.OutOrStdout(), token)
	fmt.Fprintln(cmd.ErrOrStderr(), "This token is shown once. Give it to a client with:")
	fmt.Fprintln(cmd.ErrOrStderr(), "  solax set backend --url URL --token-stdin")
}
