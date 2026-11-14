package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/austiecodes/solax/internal/auth"
	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/server"
	"github.com/austiecodes/solax/internal/svc"
	"github.com/austiecodes/solax/pkg/client"
)

// EnvSecret overrides the secret that signs agent tokens.
const EnvSecret = "SOLAX_SECRET"

// Listen addresses. "server start" is for a laptop, so it listens only on
// the loopback interface. "server run" is for a server host.
const (
	addrRun   = ":8740"
	addrStart = "127.0.0.1:8740"
)

// serverFlags holds the flags that all "solax server" commands share. The
// files of a server are next to its database:
//
//	solax.db, solax.db.secret, server.pid, server.log
type serverFlags struct{ cfg server.Config }

func (f *serverFlags) dir() string     { return filepath.Dir(f.cfg.DBPath) }
func (f *serverFlags) pidFile() string { return filepath.Join(f.dir(), "server.pid") }
func (f *serverFlags) logFile() string { return filepath.Join(f.dir(), "server.log") }

// open makes the server directory and loads the secret. Commands that
// only look at the process (stop, status, logs) do not call it.
func (f *serverFlags) open() error {
	if err := os.MkdirAll(f.dir(), 0o700); err != nil {
		return err
	}
	var err error
	f.cfg.Secret, err = server.LoadSecret(os.Getenv(EnvSecret), f.cfg.DBPath+".secret")
	return err
}

// service is "solax server run --addr addr" as a background service. It
// is ready when the server answers on addr.
func (f *serverFlags) service(addr string) svc.Service {
	s := svc.Service{
		Name:    "server",
		PIDFile: f.pidFile(),
		LogFile: f.logFile(),
		Args:    []string{"server", "--db", f.cfg.DBPath, "run", "--addr", addr},
	}
	if url, err := localURL(addr); err == nil {
		s.Ready = func(ctx context.Context) error { return client.New(url, "").Ping(ctx) }
	}
	return s
}

// localURL returns the URL that reaches a server on addr from this
// machine. An empty or wildcard host becomes 127.0.0.1.
func localURL(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("--addr %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

// serverCmd runs and manages a Solax server. The init and token commands
// open the database directly, so they run on the server host and need no
// token.
func serverCmd(g *globals) *cobra.Command {
	f := &serverFlags{}
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run and manage a Solax server",
		Long: `The server owns all state. Clients and daemons connect to it.

On a laptop, the server and the daemon can run on the same machine:
"solax server start" runs the server in the background.

The server files are in ~/.solax/server/ unless you set --db. The pid
file and the log are next to the database.`,
	}
	cmd.PersistentFlags().StringVar(&f.cfg.DBPath, "db", "", "SQLite database path (default ~/.solax/server/solax.db)")
	cmd.PersistentPreRunE = func(*cobra.Command, []string) error {
		if f.cfg.DBPath == "" {
			p, err := paths()
			if err != nil {
				return err
			}
			f.cfg.DBPath = p.ServerDB()
		}
		// Absolute, because "server start" gives it to a new process.
		var err error
		f.cfg.DBPath, err = filepath.Abs(f.cfg.DBPath)
		return err
	}
	cmd.AddCommand(
		serverRunCmd(f), serverStartCmd(f), serverStopCmd(f), serverStatusCmd(g, f), serverLogsCmd(f),
		serverInitCmd(f), serverTokenCmd(f),
	)
	return cmd
}

func serverRunCmd(f *serverFlags) *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the server in the foreground",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := f.open(); err != nil {
				return err
			}
			// Listen, then lock, then open the database. A busy port fails
			// before the lock, so "server start" never takes another
			// program on the port for this server.
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return err
			}
			lock, err := svc.Acquire("server", f.pidFile())
			if err != nil {
				ln.Close()
				return err
			}
			defer lock.Release()
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return server.Serve(ctx, f.cfg, ln, slog.New(slog.NewTextHandler(os.Stderr, nil)))
		},
	}
	cmd.Flags().StringVar(&addr, "addr", addrRun, "listen address")
	return cmd
}

func serverStartCmd(f *serverFlags) *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Run the server in the background",
		Long: `Run "solax server run" in the background and wait until it answers.

By default the server listens only on 127.0.0.1, so only this machine
can reach it. To accept other machines, set --addr, for example :8740.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			url, err := localURL(addr)
			if err != nil {
				return err
			}
			pid, err := f.service(addr).Start(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "server started (pid %d) on %s, log: %s\n", pid, url, f.logFile())
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", addrStart, "listen address")
	return cmd
}

func serverStopCmd(f *serverFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the background server and wait until it ends",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := f.service("").Stop(cmd.Context()); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "server stopped")
			return nil
		},
	}
}

// serverStatus is the output of "solax server status".
type serverStatus struct {
	Running bool   `json:"running"`
	PID     int    `json:"pid,omitempty"`
	DB      string `json:"db"`
	Log     string `json:"log"`
}

func serverStatusCmd(g *globals, f *serverFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show if the server runs on this machine",
		RunE: func(cmd *cobra.Command, args []string) error {
			pid, ok, err := svc.Running(f.pidFile())
			if err != nil {
				return err
			}
			st := serverStatus{Running: ok, PID: pid, DB: f.cfg.DBPath, Log: f.logFile()}
			if err := g.print(cmd.OutOrStdout(), st, func(w io.Writer) {
				if ok {
					fmt.Fprintf(w, "running (pid %d)\n", pid)
				} else {
					fmt.Fprintln(w, "not running")
				}
				fmt.Fprintf(w, "db:  %s\nlog: %s\n", st.DB, st.Log)
			}); err != nil {
				return err
			}
			if !ok {
				// A non-zero exit code, for scripts. The text is printed.
				return errSilent
			}
			return nil
		},
	}
}

func serverLogsCmd(f *serverFlags) *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show the server log",
		RunE: func(cmd *cobra.Command, args []string) error {
			return svc.Tail(cmd.Context(), f.logFile(), cmd.OutOrStdout(), follow)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow the log")
	return cmd
}

func serverInitCmd(f *serverFlags) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create the owner and the first client token",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := f.open(); err != nil {
				return err
			}
			token, err := server.Init(cmd.Context(), f.cfg, name)
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

func serverTokenCmd(f *serverFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "token", Short: "Manage client tokens"}

	var name string
	create := &cobra.Command{
		Use:   "create",
		Short: "Create a client token for one CLI or desktop app",
		RunE: func(cmd *cobra.Command, args []string) error {
			admin, closeDB, err := f.admin(cmd.Context())
			if err != nil {
				return err
			}
			defer closeDB()
			token, _, err := admin.CreateClient(cmd.Context(), "", name)
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
			admin, closeDB, err := f.admin(cmd.Context())
			if err != nil {
				return err
			}
			defer closeDB()
			ts, err := admin.Clients(cmd.Context(), "")
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
			admin, closeDB, err := f.admin(cmd.Context())
			if err != nil {
				return err
			}
			defer closeDB()
			return admin.RevokeClient(cmd.Context(), core.ID(args[0]))
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

// admin opens the database for the token commands.
func (f *serverFlags) admin(ctx context.Context) (*auth.Service, func() error, error) {
	if err := f.open(); err != nil {
		return nil, nil, err
	}
	return server.Admin(ctx, f.cfg)
}

// errSilent makes the command fail without a message. The command printed
// its output already.
var errSilent = errors.New("")
