package proc

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeExe(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 1.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// Off the PATH, Find looks in the install folders, the newest nvm first.
func TestFindInstallDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	old := InstallDirs
	t.Cleanup(func() { InstallDirs = old })
	InstallDirs = []string{"~/.local/bin", "~/.nvm/versions/node/*/bin"}

	for _, v := range []string{"v9.1.0", "v24.19.0", "v22.13.0"} {
		writeExe(t, filepath.Join(home, ".nvm/versions/node", v, "bin", "agent-x"))
	}
	got, err := Find("agent-x")
	if want := filepath.Join(home, ".nvm/versions/node/v24.19.0/bin/agent-x"); err != nil || got != want {
		t.Fatalf("Find = %q, %v; want %q", got, err, want)
	}
	writeExe(t, filepath.Join(home, ".local/bin/agent-x"))
	if got, _ := Find("agent-x"); got != filepath.Join(home, ".local/bin/agent-x") {
		t.Fatalf("Find = %q, want the first folder", got)
	}
	if _, err := Find("agent-y"); err == nil || !strings.Contains(err.Error(), "agent-y") {
		t.Fatalf("Find(missing) = %v", err)
	}
	// A file that is not executable is not a CLI.
	if err := os.WriteFile(filepath.Join(home, ".local/bin/agent-z"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Find("agent-z"); err == nil {
		t.Fatal("Find found a file that is not executable")
	}
}

// Fingerprint changes when the binary appears, and Start runs a binary
// that is not on the PATH, with its folder added to the PATH.
func TestFingerprintAndStartOffPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	old := InstallDirs
	t.Cleanup(func() { InstallDirs = old })
	InstallDirs = []string{"~/tools"}

	before := Fingerprint("agent-x")
	writeExe(t, filepath.Join(home, "tools/agent-x"))
	if after := Fingerprint("agent-x"); after == before {
		t.Fatalf("fingerprint did not change: %q", after)
	}
	out, err := Output(t.Context(), "agent-x")
	if err != nil || strings.TrimSpace(string(out)) != "1.0" {
		t.Fatalf("Output = %q, %v", out, err)
	}
	if _, err := Start(Config{Binary: "agent-missing"}); err == nil || !strings.HasPrefix(err.Error(), "start agent-missing") {
		t.Fatalf("Start(missing) = %v", err)
	}
}

func TestPathWith(t *testing.T) {
	sep := string(os.PathListSeparator)
	env := []string{"HOME=/h", "PATH=/a", "PATH=/b" + sep + "/c"}
	got := pathWith(env, "/d")
	if !slices.Equal(got, []string{"HOME=/h", "PATH=/a", "PATH=/b" + sep + "/c" + sep + "/d"}) {
		t.Fatalf("pathWith = %q", got)
	}
	if env[2] != "PATH=/b"+sep+"/c" {
		t.Fatal("pathWith changed its input")
	}
	if got := pathWith(env, "/c"); !slices.Equal(got, env) {
		t.Fatalf("pathWith added a folder the PATH has: %q", got)
	}
	if got := pathWith([]string{"HOME=/h"}, "/d"); !slices.Equal(got, []string{"HOME=/h", "PATH=/d"}) {
		t.Fatalf("pathWith without PATH = %q", got)
	}
}
