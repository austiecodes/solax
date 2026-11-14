package profile

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/austiecodes/solax/internal/localconfig"
)

func TestReadLocalAt(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadLocalAt(dir); !errors.Is(err, ErrNoLocal) {
		t.Fatalf("empty dir: %v, want ErrNoLocal", err)
	}
	p := localconfig.Paths{Dir: dir}
	if err := p.Save(localconfig.Config{Backend: localconfig.Backend{URL: "http://127.0.0.1:8740"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLocalAt(dir); !errors.Is(err, ErrNoLocal) {
		t.Fatalf("no token: %v, want ErrNoLocal", err)
	}
	if err := p.SaveCredentials(localconfig.Credentials{ClientToken: "sxc_1"}); err != nil {
		t.Fatal(err)
	}
	l, err := ReadLocalAt(dir)
	if err != nil || l != (Local{Server: "http://127.0.0.1:8740", Token: "sxc_1"}) {
		t.Fatalf("ReadLocalAt = %+v, %v", l, err)
	}
}

func TestLoopback(t *testing.T) {
	for server, want := range map[string]bool{
		"http://127.0.0.1:8740":        true,
		"http://localhost:8740":        true,
		"http://[::1]:8740":            true,
		"https://solax.example.com":    false,
		"http://10.0.0.5:8740":         false,
		"http://127.0.0.1.example.com": false,
	} {
		if got := (Local{Server: server}).Loopback(); got != want {
			t.Errorf("Loopback(%q) = %t, want %t", server, got, want)
		}
	}
}

func TestAdoptLocal(t *testing.T) {
	local := Local{Server: "http://127.0.0.1:8740", Token: "sxc_1"}

	// A remote server of the CLI is not adopted on its own.
	s, _ := Open(filepath.Join(t.TempDir(), "p.json"), mem{})
	if ok, err := s.AdoptLocal(Local{Server: "https://solax.example.com", Token: "sxc_1"}); ok || err != nil {
		t.Errorf("remote: AdoptLocal = %t, %v; want false", ok, err)
	}

	if ok, err := s.AdoptLocal(local); !ok || err != nil {
		t.Fatalf("empty store: AdoptLocal = %t, %v; want true", ok, err)
	}
	p, tok, err := s.Current()
	if err != nil || p.Name != LocalName || p.Server != local.Server || tok != "sxc_1" {
		t.Fatalf("Current = %+v %q %v", p, tok, err)
	}

	// With a profile, the choice of the person stays.
	s2, _ := Open(filepath.Join(t.TempDir(), "p.json"), mem{})
	if err := s2.Save(Profile{Name: "work", Server: "https://solax.example.com"}, "sxc_w"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s2.AdoptLocal(local); ok {
		t.Error("store with a profile: AdoptLocal = true")
	}
}

func TestSyncLocal(t *testing.T) {
	sec := mem{}
	s, _ := Open(filepath.Join(t.TempDir(), "p.json"), sec)
	local := Local{Server: "http://127.0.0.1:8740", Token: "sxc_1"}

	if ok, _ := s.SyncLocal(local); ok {
		t.Error("no local profile: SyncLocal = true")
	}
	if err := s.UseLocal(local); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.SyncLocal(local); ok {
		t.Error("same token: SyncLocal = true")
	}
	if ok, err := s.SyncLocal(Local{Server: local.Server, Token: "sxc_2"}); !ok || err != nil || sec[LocalName] != "sxc_2" {
		t.Errorf("new token: SyncLocal = %t, %v, saved %q", ok, err, sec[LocalName])
	}
	// The CLI moved to another server: the profile is not changed.
	if ok, _ := s.SyncLocal(Local{Server: "http://127.0.0.1:9000", Token: "sxc_3"}); ok || sec[LocalName] != "sxc_2" {
		t.Errorf("other server: SyncLocal = %t, saved %q", ok, sec[LocalName])
	}
}
