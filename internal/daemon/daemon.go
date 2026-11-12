// Package daemon builds the "solax daemon run" process: drivers,
// supervisor, the connection to the server and the local control socket.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/agent/driver/claude"
	"github.com/austiecodes/solax/internal/agent/driver/codex"
	"github.com/austiecodes/solax/internal/agent/driver/pi"
	"github.com/austiecodes/solax/internal/assets"
	"github.com/austiecodes/solax/internal/daemon/conn"
	"github.com/austiecodes/solax/internal/daemon/control"
	"github.com/austiecodes/solax/internal/daemon/supervisor"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/localconfig"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Version is the daemon version. The build sets it.
var Version = "dev"

// probeTimeout bounds one driver probe. A hung external CLI must not hang
// the WebSocket handshake.
const probeTimeout = 5 * time.Second

// Daemon is one running daemon process.
type Daemon struct {
	paths  localconfig.Paths
	creds  localconfig.Credentials
	log    *slog.Logger
	assets string // the released embeds.FS dir: prompts, skills, extensions

	// mu guards cfg, which Reload replaces while Status and hello read it.
	mu  sync.RWMutex
	cfg localconfig.Config

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
	// The role prompts, the solax skill and the pi-solax extension travel in
	// the binary. Release them next to the driver state, so a single binary
	// on a fresh machine finds them.
	assetsDir := filepath.Join(state, "assets")
	if err := assets.Release(assetsDir); err != nil {
		return nil, fmt.Errorf("release assets: %w", err)
	}
	d := &Daemon{
		paths: paths, cfg: cfg, creds: creds, log: log, assets: assetsDir,
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

// config returns the current config. The caller must not keep the copy's
// slices across a Reload without copying them.
func (d *Daemon) config() localconfig.Config {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cfg
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
	cfg := d.config()
	st := control.Status{
		PID: os.Getpid(), Version: Version, Machine: cfg.Machine.Name, Backend: cfg.Backend.URL,
		Connected: d.conn.Connected(), FreeSlots: d.sup.FreeSlots(),
	}
	for _, a := range cfg.Agents {
		st.Agents = append(st.Agents, a.Name)
	}
	return st
}

// Reload reads config.yaml again, swaps the agent definitions of the
// supervisor and sends a new hello. The backend URL cannot change under a
// running daemon: the machine identity belongs to one server.
func (d *Daemon) Reload(ctx context.Context) error {
	cfg, err := d.paths.LoadEffective()
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	prev := d.config()
	if prev.Backend.URL != cfg.Backend.URL {
		return errors.New(`config: backend.url changed: run "solax daemon restart"`)
	}

	d.mu.Lock()
	saved := d.cfg
	d.cfg = cfg
	d.mu.Unlock()

	specs, err := d.agentSpecs()
	if err != nil {
		d.mu.Lock()
		d.cfg = saved
		d.mu.Unlock()
		return err
	}
	// UpdateSpecs closes the sessions of removed agents.
	if err := d.sup.UpdateSpecs(ctx, specs, cfg.Machine.MaxSessions); err != nil {
		return err
	}
	return d.conn.Send(ctx, protocol.Envelope{Type: protocol.TypeHello, Payload: mustJSON(d.hello(ctx))})
}

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
	cfg := d.config()
	specs := make([]supervisor.AgentSpec, 0, len(cfg.Agents))
	for _, a := range cfg.Agents {
		drv, err := d.reg.Get(a.Driver)
		if err != nil {
			return nil, err
		}
		exts := make([]string, len(a.Extensions))
		for i, e := range a.Extensions {
			exts[i] = assets.Extension(d.assets, e)
		}
		specs = append(specs, supervisor.AgentSpec{
			Name:   a.Name,
			Driver: drv,
			Prompt: func(role protocol.SessionRole) (string, error) {
				return assets.Prompt(d.assets, role)
			},
			Template: agent.SessionSpec{
				Dir:        a.Dir,
				Model:      a.Model,
				Tools:      agent.ToolPolicy{Allow: a.Tools},
				Extensions: exts,
				Options:    a.Options,
			},
		})
	}
	return specs, nil
}

// hello builds the Hello record for each connect. Each driver probe gets
// its own timeout, so one hung CLI cannot stall the handshake.
func (d *Daemon) hello(ctx context.Context) protocol.Hello {
	cfg := d.config()
	h := protocol.Hello{
		MachineID: d.creds.MachineID, MachineName: cfg.Machine.Name,
		DaemonVersion: Version, FreeSlots: d.sup.FreeSlots(),
	}
	for _, drv := range d.reg.All() {
		pctx, cancel := context.WithTimeout(ctx, probeTimeout)
		p, err := drv.Probe(pctx)
		cancel()
		dp := protocol.DriverProbe{Kind: drv.Kind(), Version: p.Version, OK: p.OK && err == nil, Problem: p.Problem, Caps: p.Caps}
		if err != nil {
			dp.OK, dp.Problem = false, err.Error()
			d.log.Warn("driver probe failed", "driver", drv.Kind(), "err", err)
		}
		h.Drivers = append(h.Drivers, dp)
	}
	for _, a := range cfg.Agents {
		h.Agents = append(h.Agents, protocol.AgentInfo{Name: a.Name, Driver: a.Driver, Role: a.Role, Dir: a.Dir, Model: a.Model})
	}
	return h
}

// mustJSON encodes a wire value for an Envelope payload.
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return b
}

// Start runs "solax daemon run" as a detached background process. It
// writes the PID file and sends the output to the daemon log.
func Start(paths localconfig.Paths) (pid int, err error) {
	return 0, errs.ErrNotImplemented
}
