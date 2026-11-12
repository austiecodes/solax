package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/austiecodes/solax/internal/auth"
	"github.com/austiecodes/solax/internal/daemon"
	"github.com/austiecodes/solax/internal/daemon/control"
	"github.com/austiecodes/solax/internal/localconfig"
	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/protocol"
)

func daemonCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "daemon", Short: "Control the daemon of this machine"}
	cmd.AddCommand(
		daemonRunCmd(),
		daemonStartCmd(g),
		daemonCtlCmd("stop", "Stop the daemon", func(ctx context.Context, c *control.Client, _ io.Writer) error {
			return c.Stop(ctx)
		}),
		daemonCtlCmd("reload", "Reload config.yaml and probe the agent CLIs again", func(ctx context.Context, c *control.Client, _ io.Writer) error {
			return c.Reload(ctx)
		}),
		daemonStatusCmd(g),
		daemonLogsCmd(),
		stub("restart", "Stop and start the daemon"),
	)
	return cmd
}

func daemonRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the daemon in the foreground",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := paths()
			if err != nil {
				return err
			}
			log := slog.New(slog.NewTextHandler(os.Stderr, nil))
			d, err := daemon.New(p, log)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return d.Run(ctx)
		},
	}
}

// daemonStartCmd sets up the machine identity on first use, then starts
// "solax daemon run" in the background.
func daemonStartCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Set up this machine on first use, and start the daemon in the background",
		Long: `On first use, start generates the machine token on this machine and
sends only its hash to the server:

  - With a client token on this machine, the machine is approved at once.
  - Without one, the machine enrolls as pending and start prints a pairing
    code. Approve it from any client: solax machine approve <code>.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := paths()
			if err != nil {
				return err
			}
			cfg, err := p.LoadEffective()
			if err != nil {
				return err
			}
			// Resolve the server the same way the client commands do:
			// flag, then environment, then config.yaml.
			if s := first(g.server, os.Getenv(EnvServer)); s != "" {
				cfg.Backend.URL = strings.TrimRight(s, "/")
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			creds, err := p.LoadCredentials()
			if err != nil {
				return err
			}
			if creds.MachineID == "" {
				if err := setupMachine(cmd, g, p, cfg, &creds); err != nil {
					return fmt.Errorf("set up machine: %w", err)
				}
			}
			pid, err := daemon.Start(p)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "daemon started (pid %d), log: %s\n", pid, p.Log())
			return nil
		},
	}
}

// setupMachine generates the machine token if there is none, then
// registers (with a client token) or enrolls (without one) its hash.
func setupMachine(cmd *cobra.Command, g *globals, p localconfig.Paths, cfg localconfig.Config, creds *localconfig.Credentials) error {
	if creds.MachineToken == "" {
		t, err := auth.NewToken(auth.PrefixMachine)
		if err != nil {
			return err
		}
		// Save first, so a retry after a network error keeps the same identity.
		creds.MachineToken = t
		if err := p.SaveCredentials(*creds); err != nil {
			return err
		}
	}
	req := protocol.MachineEnrollment{Name: cfg.Machine.Name, TokenHash: auth.Hash(creds.MachineToken)}

	var (
		res protocol.MachineEnrolled
		err error
	)
	if c, cerr := g.client(); cerr == nil {
		res, err = c.RegisterMachine(cmd.Context(), req)
	} else {
		res, err = client.New(cfg.Backend.URL, "").EnrollMachine(cmd.Context(), req)
	}
	if err != nil {
		return err
	}
	creds.MachineID = res.MachineID
	if err := p.SaveCredentials(*creds); err != nil {
		return err
	}
	if res.State == protocol.MachinePending {
		fmt.Fprintf(cmd.OutOrStdout(), "machine %s waits for approval. From any client run:\n  solax machine approve %s\n(valid until %s)\n",
			res.MachineID, res.PairCode, res.PairUntil.Format("2006-01-02 15:04"))
	}
	return nil
}

func daemonStatusCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := ctlClient()
			if err != nil {
				return err
			}
			st, err := c.Status(cmd.Context())
			if err != nil {
				return err
			}
			return g.print(cmd.OutOrStdout(), st, func(w io.Writer) {
				fmt.Fprintf(w, "pid %d, version %s\n", st.PID, st.Version)
				fmt.Fprintf(w, "machine %s, backend %s, connected=%t\n", st.Machine, st.Backend, st.Connected)
				fmt.Fprintf(w, "free slots %d, sessions %d\n", st.FreeSlots, len(st.Sessions))
				for id, state := range st.Sessions {
					fmt.Fprintf(w, "  %s  %s\n", id, state)
				}
			})
		},
	}
}

func daemonLogsCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show the daemon log",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := ctlClient()
			if err != nil {
				return err
			}
			return c.Logs(cmd.Context(), cmd.OutOrStdout(), follow)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow the log")
	return cmd
}

func daemonCtlCmd(use, short string, fn func(context.Context, *control.Client, io.Writer) error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := ctlClient()
			if err != nil {
				return err
			}
			return fn(cmd.Context(), c, cmd.OutOrStdout())
		},
	}
}

func ctlClient() (*control.Client, error) {
	p, err := paths()
	if err != nil {
		return nil, err
	}
	return control.NewClient(p.Socket()), nil
}

// Local agent definitions.

func expandHome(p string) string {
	if len(p) > 1 && p[:2] == "~/" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
