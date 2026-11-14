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
	"github.com/austiecodes/solax/internal/agent/driver"
	"github.com/austiecodes/solax/internal/assets"
	"github.com/austiecodes/solax/internal/daemon/conn"
	"github.com/austiecodes/solax/internal/daemon/control"
	"github.com/austiecodes/solax/internal/daemon/supervisor"
	"github.com/austiecodes/solax/internal/localconfig"
	"github.com/austiecodes/solax/internal/svc"
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
	bin    string // folder of the solax binary, put first on the PATH of sessions

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
//
// The daemon defines no agents: the server sends the settings of an agent
// with each session.ensure. The daemon reports which agent CLIs it found.
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
	bin := ""
	if exe, err := os.Executable(); err == nil {
		bin = filepath.Dir(exe)
	}
	d := &Daemon{paths: paths, cfg: cfg, creds: creds, log: log, assets: assetsDir, bin: bin, reg: driver.Registry(state)}
	d.conn = conn.New(cfg.Backend.URL, creds.MachineToken, d.hello, d.heartbeat, d.handle, log)
	return d, nil
}

// config returns the current config.
func (d *Daemon) config() localconfig.Config {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cfg
}

// Run runs the daemon in the foreground until ctx ends or Stop is called.
func (d *Daemon) Run(ctx context.Context) error {
	ctx, d.stop = context.WithCancel(ctx)
	defer d.stop()
	d.sup = supervisor.New(ctx, supervisor.Config{
		Registry: d.reg,
		Prompt:   func(role protocol.SessionRole) (string, error) { return assets.Prompt(d.assets, role) },
		Env:      d.sessionEnv,
		Dir:      localconfig.ExpandHome,
		MaxSlots: d.config().Machine.MaxSessions,
		Sink:     d.conn,
		Log:      d.log,
	})

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

// sessionEnv is the environment of one session. The agent calls the
// solax CLI with it: the server URL, its own agent token and its job.
func (d *Daemon) sessionEnv(e protocol.SessionEnsure) map[string]string {
	env := map[string]string{
		"SOLAX_SERVER":  d.config().Backend.URL,
		"SOLAX_TOKEN":   e.AgentToken,
		"SOLAX_AGENT":   e.Agent,
		"SOLAX_SESSION": e.SessionID,
	}
	if e.Job != "" {
		env["SOLAX_JOB"] = e.Job
	}
	if d.bin != "" {
		env["PATH"] = d.bin + string(os.PathListSeparator) + os.Getenv("PATH")
	}
	return env
}

// control.Daemon.

func (d *Daemon) Status() control.Status {
	cfg := d.config()
	st := control.Status{
		PID: os.Getpid(), Version: Version, Machine: cfg.Machine.Name, Backend: cfg.Backend.URL,
		Connected: d.conn.Connected(),
	}
	if d.sup != nil {
		st.FreeSlots, st.Sessions = d.sup.FreeSlots(), d.sup.States()
	}
	return st
}

// Reload reads config.yaml again and sends a new hello with fresh driver
// probes, for example after a person installed an agent CLI. The backend
// URL cannot change under a running daemon: the machine identity belongs
// to one server.
func (d *Daemon) Reload(ctx context.Context) error {
	cfg, err := d.paths.LoadEffective()
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if d.config().Backend.URL != cfg.Backend.URL {
		return errors.New(`config: backend.url changed: run "solax daemon restart"`)
	}
	d.mu.Lock()
	d.cfg = cfg
	d.mu.Unlock()
	if d.sup != nil {
		d.sup.SetMaxSlots(cfg.Machine.MaxSessions)
	}
	return d.conn.Send(ctx, protocol.Envelope{Type: protocol.TypeHello, Payload: mustJSON(d.hello(ctx))})
}

// Stop ends Run.
func (d *Daemon) Stop() {
	if d.stop != nil {
		d.stop()
	}
}

// Logs copies the daemon log to w. With follow, it keeps copying new
// lines until ctx ends.
func (d *Daemon) Logs(ctx context.Context, w io.Writer, follow bool) error {
	return svc.Tail(ctx, d.paths.Log(), w, follow)
}

var _ control.Daemon = (*Daemon)(nil)

func (d *Daemon) handle(ctx context.Context, env protocol.Envelope) protocol.Ack {
	return d.sup.Handle(ctx, env)
}

// hello builds the Hello record for each connect. Each driver probe gets
// its own timeout, so one hung CLI cannot stall the handshake.
func (d *Daemon) hello(ctx context.Context) protocol.Hello {
	cfg := d.config()
	h := protocol.Hello{MachineID: d.creds.MachineID, MachineName: cfg.Machine.Name, DaemonVersion: Version}
	if d.sup != nil {
		h.FreeSlots, h.Sessions = d.sup.FreeSlots(), d.sup.States()
	}
	for _, drv := range d.reg.All() {
		pctx, cancel := context.WithTimeout(ctx, probeTimeout)
		p, err := drv.Probe(pctx)
		cancel()
		dp := protocol.DriverProbe{Kind: drv.Kind(), Version: p.Version, OK: p.OK && err == nil, Problem: p.Problem, Caps: p.Caps}
		if err != nil {
			dp.OK, dp.Problem = false, err.Error()
		}
		if !dp.OK {
			d.log.Info("agent CLI not available", "driver", drv.Kind(), "problem", dp.Problem)
		}
		h.Drivers = append(h.Drivers, dp)
	}
	return h
}

func (d *Daemon) heartbeat() protocol.Heartbeat {
	return protocol.Heartbeat{FreeSlots: d.sup.FreeSlots(), Sessions: d.sup.States()}
}

// mustJSON encodes a wire value for an Envelope payload.
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return b
}
