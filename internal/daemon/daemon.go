// Package daemon builds the "solax daemon run" process: drivers,
// supervisor, the connection to the server and the local control socket.
package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/agent/driver/claude"
	"github.com/austiecodes/solax/internal/agent/driver/codex"
	"github.com/austiecodes/solax/internal/agent/driver/pi"
	"github.com/austiecodes/solax/internal/daemon/conn"
	"github.com/austiecodes/solax/internal/daemon/control"
	"github.com/austiecodes/solax/internal/daemon/supervisor"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/localconfig"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Version is the daemon version. The build sets it.
var Version = "dev"

// Daemon is one running daemon process.
type Daemon struct {
	paths localconfig.Paths
	cfg   localconfig.Config
	creds localconfig.Credentials
	log   *slog.Logger

	reg  *agent.Registry
	sup  *supervisor.Supervisor
	conn *conn.Conn
	stop context.CancelFunc
}

// New builds a daemon from the local state. It needs the machine token that
// "solax daemon start" generated. A pending machine runs, but the server
// rejects its connection until a client approves it; conn retries.
func New(paths localconfig.Paths, log *slog.Logger) (*Daemon, error) {
	cfg, err := paths.LoadEffective()
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	creds, err := paths.LoadCredentials()
	if err != nil {
		return nil, err
	}
	if creds.MachineToken == "" || creds.MachineID == "" {
		return nil, errors.New(`machine is not set up: run "solax daemon start"`)
	}

	state := cfg.Machine.StateDir
	d := &Daemon{
		paths: paths, cfg: cfg, creds: creds, log: log,
		reg: agent.NewRegistry(
			pi.New(pi.Config{StateDir: filepath.Join(state, "pi")}),
			codex.New(codex.Config{StateDir: filepath.Join(state, "codex")}),
			claude.New(claude.Config{StateDir: filepath.Join(state, "claude")}),
		),
	}
	specs, err := d.agentSpecs()
	if err != nil {
		return nil, err
	}
	d.conn = conn.New(cfg.Backend.URL, creds.MachineToken, d.hello, d.handle)
	d.sup = supervisor.New(specs, cfg.Machine.MaxSessions, d.conn)
	return d, nil
}

// Run runs the daemon in the foreground until ctx ends or Stop is called.
func (d *Daemon) Run(ctx context.Context) error {
	ctx, d.stop = context.WithCancel(ctx)
	defer d.stop()

	ctl := control.NewServer(d.paths.Socket(), d)
	errc := make(chan error, 2)
	go func() { errc <- ctl.Serve(ctx) }()
	go func() { errc <- d.conn.Run(ctx) }()

	var err error
	select {
	case <-ctx.Done():
	case err = <-errc:
	}
	return errors.Join(err, d.sup.Shutdown(context.WithoutCancel(ctx)))
}

// control.Daemon.

func (d *Daemon) Status() control.Status {
	st := control.Status{
		PID: os.Getpid(), Version: Version, Machine: d.cfg.Machine.Name, Backend: d.cfg.Backend.URL,
		Connected: d.conn.Connected(), FreeSlots: d.sup.FreeSlots(),
	}
	for _, a := range d.cfg.Agents {
		st.Agents = append(st.Agents, a.Name)
	}
	return st
}

// Reload reads config.yaml again, updates the agents and sends a new hello.
func (d *Daemon) Reload(ctx context.Context) error { return errs.ErrNotImplemented }

// Stop ends Run.
func (d *Daemon) Stop() {
	if d.stop != nil {
		d.stop()
	}
}

// Logs copies the daemon log to w.
func (d *Daemon) Logs(ctx context.Context, w io.Writer, follow bool) error {
	return errs.ErrNotImplemented
}

var _ control.Daemon = (*Daemon)(nil)

func (d *Daemon) handle(ctx context.Context, env protocol.Envelope) protocol.Ack {
	return d.sup.Handle(ctx, env)
}

func (d *Daemon) agentSpecs() ([]supervisor.AgentSpec, error) {
	specs := make([]supervisor.AgentSpec, 0, len(d.cfg.Agents))
	for _, a := range d.cfg.Agents {
		drv, err := d.reg.Get(a.Driver)
		if err != nil {
			return nil, err
		}
		specs = append(specs, supervisor.AgentSpec{
			Name:   a.Name,
			Driver: drv,
			Template: agent.SessionSpec{
				Dir:        a.Dir,
				Model:      a.Model,
				Tools:      agent.ToolPolicy{Allow: a.Tools},
				Extensions: a.Extensions,
				Options:    a.Options,
			},
		})
	}
	return specs, nil
}

// hello builds the Hello record for each connect.
func (d *Daemon) hello() protocol.Hello {
	ctx := context.Background()
	h := protocol.Hello{
		MachineID: d.creds.MachineID, MachineName: d.cfg.Machine.Name,
		DaemonVersion: Version, FreeSlots: d.sup.FreeSlots(),
	}
	for _, drv := range d.reg.All() {
		p, err := drv.Probe(ctx)
		dp := protocol.DriverProbe{Kind: drv.Kind(), Version: p.Version, OK: p.OK && err == nil, Problem: p.Problem, Caps: p.Caps}
		if err != nil {
			dp.Problem = err.Error()
			d.log.Warn("driver probe failed", "driver", drv.Kind(), "err", err)
		}
		h.Drivers = append(h.Drivers, dp)
	}
	for _, a := range d.cfg.Agents {
		h.Agents = append(h.Agents, protocol.AgentInfo{Name: a.Name, Driver: a.Driver, Role: a.Role, Dir: a.Dir, Model: a.Model})
	}
	return h
}

// Start runs "solax daemon run" as a detached background process. It
// writes the PID file and sends the output to the daemon log.
func Start(paths localconfig.Paths) (pid int, err error) {
	return 0, errs.ErrNotImplemented
}
