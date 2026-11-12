package localconfig

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWithDefaultsExpandsHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	p := Paths{Dir: t.TempDir()}
	c := Config{
		Machine: Machine{StateDir: "~/x"},
	}
	got := c.withDefaults(p)

	if want := filepath.Join(home, "x"); got.Machine.StateDir != want {
		t.Errorf("StateDir = %q, want %q", got.Machine.StateDir, want)
	}
	if want := filepath.Join(home, "codes", "api"); ExpandHome("~/codes/api") != want {
		t.Errorf("ExpandHome = %q, want %q", ExpandHome("~/codes/api"), want)
	}
	if ExpandHome("/srv/code") != "/srv/code" {
		t.Errorf("absolute path changed")
	}
	// Defaults that are not paths stay.
	if got.Machine.MaxSessions != 4 {
		t.Errorf("MaxSessions = %d, want 4", got.Machine.MaxSessions)
	}
}

// Two processes that save the config at once must not overwrite each
// other's temp file.
func TestSaveConcurrent(t *testing.T) {
	p := Paths{Dir: t.TempDir()}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.Save(Config{Backend: Backend{URL: "https://example.com"}}); err != nil {
				t.Errorf("Save: %v", err)
			}
		}()
	}
	wg.Wait()

	b, err := os.ReadFile(p.Config())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "https://example.com") {
		t.Fatalf("config.yaml does not hold the value:\n%s", b)
	}
	// No temp file is left behind.
	entries, err := os.ReadDir(p.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".solax-") {
			t.Fatalf("left temp file %q", e.Name())
		}
	}
}

func TestWriteYAMLMode(t *testing.T) {
	p := Paths{Dir: t.TempDir()}
	if err := p.SaveCredentials(Credentials{ClientToken: "sxc_x"}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p.Credentials())
	if err != nil {
		t.Fatal(err)
	}
	if mode := st.Mode().Perm(); mode != 0o600 {
		t.Fatalf("credentials mode = %o, want 600", mode)
	}
}
