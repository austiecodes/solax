package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/localconfig"
	"github.com/austiecodes/solax/internal/server"
	"github.com/austiecodes/solax/internal/svc"
	"github.com/austiecodes/solax/pkg/client"
)

// localClientName names the client token that "setup local" creates.
const localClientName = "local-cli"

func setupCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "setup", Short: "Set up Solax on this machine"}
	cmd.AddCommand(setupLocalCmd(g))
	return cmd
}

func setupLocalCmd(g *globals) *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "local",
		Short: "Run the server and the daemon on this machine",
		Long: `Set up Solax with no other host: the server and the daemon both run on
this machine, as two background processes.

  1. Create the server database, if there is none (~/.solax/server/).
  2. Start the server, if it does not run.
  3. Use it from this CLI: save its URL and a client token.
  4. Start the daemon, if it does not run. The machine is approved at once.

You can run it again at any time. It changes only what is missing.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return setupLocal(cmd, g, addr)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", addrStart, "listen address of the server")
	return cmd
}

func setupLocal(cmd *cobra.Command, g *globals, addr string) error {
	ctx, out := cmd.Context(), cmd.OutOrStdout()
	url, err := localURL(addr)
	if err != nil {
		return err
	}
	p, err := paths()
	if err != nil {
		return err
	}
	f := &serverFlags{}
	f.cfg.DBPath = p.ServerDB()
	if err := f.open(); err != nil {
		return err
	}

	// 1. Database. Init fails with ErrConflict when it has an owner.
	token, err := server.Init(ctx, f.cfg, localClientName)
	fresh := err == nil
	switch {
	case fresh:
		step(out, "server", "created %s", f.cfg.DBPath)
	case !errors.Is(err, errs.ErrConflict):
		return err
	}

	// 2. Server process.
	pid, err := f.service(addr).Start(ctx)
	var running *svc.RunningError
	switch {
	case err == nil:
		step(out, "server", "started on %s (pid %d)", url, pid)
	case errors.As(err, &running):
		if client.New(url, "").Ping(ctx) != nil {
			return fmt.Errorf(`the server runs (pid %d), but not on %s: run "solax server stop", or set --addr`, running.PID, url)
		}
		step(out, "server", "runs on %s (pid %d)", url, running.PID)
	default:
		return err
	}

	// 3. This CLI uses the server.
	if token == "" {
		if token, err = localToken(ctx, p, f, url); err != nil {
			return err
		}
	}
	old, err := p.Load()
	if err != nil {
		return err
	}
	// A new database does not know the machine of an old one.
	machineReset, err := saveBackend(p, url, token, fresh)
	if err != nil {
		return err
	}
	if old.Backend.URL != "" && old.Backend.URL != url {
		step(out, "client", "uses %s (before: %s)", url, old.Backend.URL)
	} else {
		step(out, "client", "uses %s", url)
	}

	// 4. Daemon. It reads its machine identity on start, so restart it
	// when the identity changed.
	d := daemonService(p)
	dpid, ok, err := svc.Running(p.PID())
	if err != nil {
		return err
	}
	if ok && machineReset {
		if err := d.Stop(ctx); err != nil && !errors.Is(err, svc.ErrNotRunning) {
			return err
		}
		ok = false
	}
	if ok {
		step(out, "daemon", "runs (pid %d)", dpid)
	} else {
		if dpid, err = startDaemon(cmd, g, p); err != nil {
			return err
		}
		step(out, "daemon", "started (pid %d)", dpid)
	}

	fmt.Fprint(out, `
Solax runs on this machine. Add agents next, for example:

  solax agent add coder --driver codex --dir ~/src/app --desc "Writes code"
  solax agent add reviewer --driver claude --dir ~/src/app --desc "Reviews code"
  solax agent add router --driver pi --dir ~ --super

Stop it with "solax daemon stop" and "solax server stop".
`)
	return nil
}

// localToken returns a client token for the local server. It keeps the
// saved token if the server accepts it. Else it creates a new one in the
// database: the server keeps only token hashes, so a lost token cannot be
// read back.
func localToken(ctx context.Context, p localconfig.Paths, f *serverFlags, url string) (string, error) {
	cfg, err := p.Load()
	if err != nil {
		return "", err
	}
	creds, err := p.LoadCredentials()
	if err != nil {
		return "", err
	}
	if cfg.Backend.URL == url && creds.ClientToken != "" {
		err := client.New(url, creds.ClientToken).Ping(ctx)
		var ce *client.Error
		switch {
		case err == nil:
			return creds.ClientToken, nil
		case !errors.As(err, &ce) || (ce.Status != http.StatusUnauthorized && ce.Status != http.StatusForbidden):
			return "", fmt.Errorf("check the saved client token: %w", err)
		}
		// Unknown (401) or revoked (403): create a new one.
	}
	admin, closeDB, err := f.admin(ctx)
	if err != nil {
		return "", err
	}
	defer closeDB()
	token, _, err := admin.CreateClient(ctx, "", localClientName)
	return token, err
}

// step prints one line of setup progress.
func step(w io.Writer, what, format string, args ...any) {
	fmt.Fprintf(w, "  %-7s %s\n", what, fmt.Sprintf(format, args...))
}
