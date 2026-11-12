// Command solax-server is the Solax server.
//
//	solax-server run                     serve the API
//	solax-server init                    new database: owner + first client token
//	solax-server token create --name N   create a client token (printed once)
//	solax-server token list | revoke ID
//
// The init and token commands run on the server host and open the database
// directly. They need no token.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/server"
)

func main() {
	var cfg server.Config
	root := &cobra.Command{Use: "solax-server", Short: "The Solax server", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringVar(&cfg.DBPath, "db", "solax.db", "SQLite database path")
	root.PersistentPreRun = func(*cobra.Command, []string) { cfg.Secret = []byte(os.Getenv("SOLAX_SECRET")) }

	run := &cobra.Command{
		Use:   "run",
		Short: "Serve the API",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return server.Run(ctx, cfg, slog.New(slog.NewTextHandler(os.Stderr, nil)))
		},
	}
	run.Flags().StringVar(&cfg.Addr, "addr", ":8740", "listen address")

	var initName string
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Create the owner and the first client token",
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := server.Init(cmd.Context(), cfg, initName)
			if err != nil {
				return err
			}
			printToken(cmd, token)
			return nil
		},
	}
	initCmd.Flags().StringVar(&initName, "name", "admin", "name of the first client token")

	root.AddCommand(run, initCmd, tokenCmd(&cfg))
	if err := root.ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "solax-server:", err)
		os.Exit(1)
	}
}

func tokenCmd(cfg *server.Config) *cobra.Command {
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
			for _, t := range ts {
				used := "never"
				if !t.LastUsed.IsZero() {
					used = t.LastUsed.Format("2006-01-02 15:04")
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\trevoked=%t\tlast used %s\n", t.ID, t.Name, t.Revoked, used)
			}
			return nil
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
