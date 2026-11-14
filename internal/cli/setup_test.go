package cli

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/austiecodes/solax/internal/localconfig"
	"github.com/austiecodes/solax/internal/svc"
)

// TestSetupLocal runs the real binary: "setup local" starts background
// processes, and they must outlive the command.
func TestSetupLocal(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the solax binary")
	}
	bin := filepath.Join(t.TempDir(), "solax")
	if out, err := exec.Command("go", "build", "-o", bin, "github.com/austiecodes/solax/cmd/solax").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	home := t.TempDir()
	p := localconfig.Paths{Dir: home}
	addr := "127.0.0.1:" + strconv.Itoa(freePort(t))

	solax := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		// Only SOLAX_HOME: a SOLAX_SERVER or SOLAX_TOKEN of the caller
		// must not leak in.
		cmd.Env = append(cleanEnv(), localconfig.EnvHome+"="+home)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("solax %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	t.Cleanup(func() {
		for _, svc := range []string{"daemon", "server"} {
			cmd := exec.Command(bin, svc, "stop")
			cmd.Env = append(cleanEnv(), localconfig.EnvHome+"="+home)
			_ = cmd.Run()
		}
	})
	creds := func() localconfig.Credentials {
		t.Helper()
		c, err := p.LoadCredentials()
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	daemonPID := func() int {
		t.Helper()
		pid, ok, err := svc.Running(p.PID())
		if err != nil || !ok {
			t.Fatalf("daemon: running=%t, %v", ok, err)
		}
		return pid
	}

	// 1. First run: everything is new.
	out := solax("setup", "local", "--addr", addr)
	for _, want := range []string{"server  created", "server  started", "daemon  started"} {
		if !strings.Contains(out, want) {
			t.Errorf("first run: no %q in:\n%s", want, out)
		}
	}
	first := creds()
	if first.ClientToken == "" || first.MachineID == "" {
		t.Fatalf("first run: credentials %+v; want a client token and a machine", first)
	}
	pid1 := daemonPID()
	if out := solax("machine", "list"); !strings.Contains(out, first.MachineID) || !strings.Contains(out, "approved") {
		t.Errorf("machine list:\n%s\nwant %s approved", out, first.MachineID)
	}

	// 2. Run again: nothing changes.
	out = solax("setup", "local", "--addr", addr)
	if strings.Contains(out, "created") || strings.Contains(out, "started") {
		t.Errorf("second run changed something:\n%s", out)
	}
	if c := creds(); c != first {
		t.Errorf("second run: credentials %+v, want %+v", c, first)
	}
	if pid := daemonPID(); pid != pid1 {
		t.Errorf("second run: daemon pid %d, want %d", pid, pid1)
	}

	// 3. A bad saved token: a new token, the same machine and daemon.
	bad := first
	bad.ClientToken = "sxc_bad"
	if err := p.SaveCredentials(bad); err != nil {
		t.Fatal(err)
	}
	solax("setup", "local", "--addr", addr)
	c := creds()
	if c.ClientToken == "sxc_bad" || c.ClientToken == first.ClientToken || c.MachineID != first.MachineID {
		t.Errorf("bad token: credentials %+v; want a new token and machine %s", c, first.MachineID)
	}
	if pid := daemonPID(); pid != pid1 {
		t.Errorf("bad token: daemon pid %d, want %d (no restart)", pid, pid1)
	}
	solax("machine", "list") // the new token works

	// 4. A new database: a new machine identity and a restarted daemon.
	solax("server", "stop")
	if err := os.RemoveAll(p.ServerDir()); err != nil {
		t.Fatal(err)
	}
	out = solax("setup", "local", "--addr", addr)
	if !strings.Contains(out, "server  created") {
		t.Errorf("new database: no %q in:\n%s", "server  created", out)
	}
	c = creds()
	if c.MachineID == "" || c.MachineID == first.MachineID {
		t.Errorf("new database: machine %q; want a new one", c.MachineID)
	}
	if pid := daemonPID(); pid == pid1 {
		t.Errorf("new database: daemon pid %d; want a restart", pid)
	}
	if out := solax("machine", "list"); !strings.Contains(out, c.MachineID) {
		t.Errorf("machine list:\n%s\nwant %s", out, c.MachineID)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func cleanEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "SOLAX_") {
			env = append(env, kv)
		}
	}
	return env
}
