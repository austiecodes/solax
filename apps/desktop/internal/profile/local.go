package profile

import (
	"errors"
	"net"
	"net/url"

	"github.com/austiecodes/solax/internal/localconfig"
)

// LocalName is the name of the profile for the server that the solax CLI
// of this Mac uses.
const LocalName = "local"

// ErrNoLocal is returned when the solax CLI of this Mac has no server or
// no client token.
var ErrNoLocal = errors.New(`no local setup: in a terminal, run "solax setup local"`)

// Local is the server and the client token that the solax CLI of this Mac
// uses. "solax setup local" writes them to ~/.solax.
type Local struct {
	Server string
	Token  string
}

// ReadLocal reads the CLI settings in ~/.solax (or $SOLAX_HOME).
func ReadLocal() (Local, error) {
	dir, err := localconfig.Home()
	if err != nil {
		return Local{}, err
	}
	return ReadLocalAt(dir)
}

// ReadLocalAt reads the CLI settings in dir.
func ReadLocalAt(dir string) (Local, error) {
	p := localconfig.Paths{Dir: dir}
	cfg, err := p.Load()
	if err != nil {
		return Local{}, err
	}
	creds, err := p.LoadCredentials()
	if err != nil {
		return Local{}, err
	}
	if cfg.Backend.URL == "" || creds.ClientToken == "" {
		return Local{}, ErrNoLocal
	}
	return Local{Server: cfg.Backend.URL, Token: creds.ClientToken}, nil
}

// Loopback reports if the server is on this Mac.
func (l Local) Loopback() bool {
	u, err := url.Parse(l.Server)
	if err != nil {
		return false
	}
	if u.Hostname() == "localhost" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return ip != nil && ip.IsLoopback()
}

// UseLocal saves l as the "local" profile and uses it. The caller checks
// that l is on this Mac (Loopback).
func (s *Store) UseLocal(l Local) error {
	if err := s.Save(Profile{Name: LocalName, Server: l.Server}, l.Token); err != nil {
		return err
	}
	return s.Use(LocalName)
}

// AdoptLocal uses l when the store has no profiles and l is on this Mac.
// So a person who ran "solax setup local" needs no setup in the app.
func (s *Store) AdoptLocal(l Local) (bool, error) {
	if len(s.List()) > 0 || !l.Loopback() {
		return false, nil
	}
	return true, s.UseLocal(l)
}

// SyncLocal updates the token of the "local" profile when the CLI got a
// new one for the same server, for example after "solax setup local"
// made a new token. It reports if the token changed.
func (s *Store) SyncLocal(l Local) (bool, error) {
	s.mu.Lock()
	p, ok := s.find(LocalName)
	s.mu.Unlock()
	if !ok || p.Server != l.Server || l.Token == "" {
		return false, nil
	}
	if old, err := s.secrets.Get(LocalName); err == nil && old == l.Token {
		return false, nil
	}
	return true, s.secrets.Set(LocalName, l.Token)
}
