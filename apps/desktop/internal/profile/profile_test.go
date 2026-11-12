package profile

import (
	"path/filepath"
	"testing"
)

type mem map[string]string

func (m mem) Get(p string) (string, error) { return m[p], nil }
func (m mem) Set(p, t string) error        { m[p] = t; return nil }
func (m mem) Delete(p string) error        { delete(m, p); return nil }

// Profiles survive a reopen; the token is in the secrets, not the file.
func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	sec := mem{}
	s, err := Open(path, sec)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Current(); err != ErrNoProfile {
		t.Fatalf("empty store Current = %v", err)
	}
	if err := s.Save(Profile{Name: "local", Server: "http://x/"}, "sxc_1"); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path, sec)
	if err != nil {
		t.Fatal(err)
	}
	p, tok, err := s2.Current()
	if err != nil || p.Server != "http://x" || tok != "sxc_1" {
		t.Fatalf("current = %+v %q %v", p, tok, err)
	}
	// An empty token keeps the stored one.
	if err := s2.Save(Profile{Name: "local", Server: "http://y"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, tok, _ := s2.Current(); tok != "sxc_1" {
		t.Fatalf("token = %q", tok)
	}
	if err := s2.Remove("local"); err != nil || len(s2.List()) != 0 || sec["local"] != "" {
		t.Fatalf("remove: %v %+v %v", err, s2.List(), sec)
	}
}
