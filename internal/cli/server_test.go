package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/austiecodes/solax/internal/auth"
	"github.com/austiecodes/solax/internal/localconfig"
)

// run runs the root command with args and returns its stdout.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := NewRoot()
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&errOut)
	err := root.Execute()
	return out.String(), err
}

func TestServerInitUsesHomeByDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv(localconfig.EnvHome, home)
	t.Setenv(EnvSecret, "")
	p := localconfig.Paths{Dir: home}

	out, err := run(t, "server", "init")
	if err != nil {
		t.Fatalf("server init: %v", err)
	}
	if token := strings.TrimSpace(out); !strings.HasPrefix(token, auth.PrefixClient) {
		t.Fatalf("server init printed %q, want a client token", token)
	}
	for _, f := range []string{p.ServerDB(), p.ServerDB() + ".secret"} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("want %s: %v", f, err)
		}
	}
	if st, err := os.Stat(p.ServerDir()); err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("server dir: %v, mode %v; want 0700", err, st.Mode().Perm())
	}

	if _, err := run(t, "server", "init"); err == nil {
		t.Error("second server init: want an error")
	}

	out, err = run(t, "server", "token", "list")
	if err != nil {
		t.Fatalf("server token list: %v", err)
	}
	if !strings.Contains(out, "admin") {
		t.Errorf("token list = %q, want the admin token", out)
	}
}

func TestServerDBFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv(localconfig.EnvHome, home)
	t.Setenv(EnvSecret, "")
	db := filepath.Join(t.TempDir(), "nested", "s.db")

	if _, err := run(t, "server", "--db", db, "init", "--name", "ci"); err != nil {
		t.Fatalf("server init: %v", err)
	}
	if _, err := os.Stat(db); err != nil {
		t.Errorf("want %s: %v", db, err)
	}
	if _, err := os.Stat(localconfig.Paths{Dir: home}.ServerDB()); err == nil {
		t.Error("--db set, but the default database was created too")
	}
}
