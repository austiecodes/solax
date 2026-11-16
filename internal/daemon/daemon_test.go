package daemon

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/agent/driver/claude"
	"github.com/austiecodes/solax/pkg/ir"
)

// A hello reports a driver whose binary is missing as not working, and
// as working once it is installed. The fingerprints change with the
// install.
func TestHelloInstalledDriver(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fork")
	ds := []agent.Driver{claude.New(claude.Config{Kind: "fork", Binary: bin})}
	d := &Daemon{drivers: ds, log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	h := d.hello(t.Context())
	if len(h.Drivers) != 1 || h.Drivers[0].Kind != "fork" || h.Drivers[0].OK {
		t.Fatalf("drivers = %+v, want the fork as not working", h.Drivers)
	}
	before := d.fingerprints()
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 1.2.3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if d.fingerprints() == before {
		t.Fatal("fingerprints did not change after the install")
	}
	h = d.hello(t.Context())
	if len(h.Drivers) != 1 || h.Drivers[0].Kind != ir.Kind("fork") || !h.Drivers[0].OK || h.Drivers[0].Version != "1.2.3" {
		t.Fatalf("drivers = %+v, want the fork as working", h.Drivers)
	}
	if d.fp != d.fingerprints() {
		t.Fatal("hello did not keep the fingerprints it probed")
	}
}
