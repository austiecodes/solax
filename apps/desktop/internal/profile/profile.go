// Package profile stores the server profiles of the desktop app. A profile
// names one Solax server. The token of a profile is kept in the OS keychain,
// never in the profile file.
package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// ErrNoProfile is returned when no profile is in use.
var ErrNoProfile = errors.New("no server profile: add one in Settings")

// Profile is one server configuration.
type Profile struct {
	Name   string `json:"name"`
	Server string `json:"server"` // base URL, for example http://127.0.0.1:8740
}

// Secrets keeps tokens. The v1 implementation uses the OS keychain.
type Secrets interface {
	Get(profile string) (string, error)
	Set(profile, token string) error
	Delete(profile string) error
}

// file is the JSON form of the profile file.
type file struct {
	Current  string    `json:"current"`
	Profiles []Profile `json:"profiles"`
}

// Store keeps the profiles in <user config dir>/solax/desktop/profiles.json.
type Store struct {
	path    string
	secrets Secrets

	mu       sync.Mutex
	profiles []Profile
	current  string
}

// DefaultPath returns the path of the profile file.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "solax", "desktop", "profiles.json"), nil
}

// Open loads the profile file at path. A missing file is an empty store.
func Open(path string, s Secrets) (*Store, error) {
	st := &Store{path: path, secrets: s}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	st.profiles, st.current = f.Profiles, f.Current
	return st, nil
}

// List returns all profiles.
func (s *Store) List() []Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Profile(nil), s.profiles...)
}

// Current returns the profile in use and its token.
func (s *Store) Current() (Profile, string, error) {
	s.mu.Lock()
	p, ok := s.find(s.current)
	s.mu.Unlock()
	if !ok {
		return Profile{}, "", ErrNoProfile
	}
	token, err := s.secrets.Get(p.Name)
	if err != nil {
		return p, "", fmt.Errorf("token of profile %q: %w", p.Name, err)
	}
	return p, token, nil
}

// Save adds or replaces a profile and stores its token. An empty token
// keeps the stored one.
func (s *Store) Save(p Profile, token string) error {
	p.Name, p.Server = strings.TrimSpace(p.Name), strings.TrimRight(strings.TrimSpace(p.Server), "/")
	if p.Name == "" || p.Server == "" {
		return errors.New("a profile needs a name and a server URL")
	}
	if token = strings.TrimSpace(token); token != "" {
		if err := s.secrets.Set(p.Name, token); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	replaced := false
	for i := range s.profiles {
		if s.profiles[i].Name == p.Name {
			s.profiles[i], replaced = p, true
		}
	}
	if !replaced {
		s.profiles = append(s.profiles, p)
	}
	if s.current == "" {
		s.current = p.Name
	}
	return s.write()
}

// Use selects the profile in use.
func (s *Store) Use(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.find(name); !ok {
		return fmt.Errorf("no profile %q", name)
	}
	s.current = name
	return s.write()
}

// Remove deletes a profile and its token.
func (s *Store) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.profiles[:0]
	for _, p := range s.profiles {
		if p.Name != name {
			out = append(out, p)
		}
	}
	s.profiles = out
	if s.current == name {
		s.current = ""
	}
	if err := s.write(); err != nil {
		return err
	}
	return s.secrets.Delete(name)
}

func (s *Store) find(name string) (Profile, bool) {
	for _, p := range s.profiles {
		if p.Name == name {
			return p, true
		}
	}
	return Profile{}, false
}

// write saves the file atomically. The caller holds mu.
func (s *Store) write() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(file{Current: s.current, Profiles: s.profiles}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Keychain is the Secrets implementation on the macOS keychain. It calls
// /usr/bin/security, so the app needs no cgo.
type Keychain struct {
	Service string // for example "dev.austiecodes.solax"
}

func (k Keychain) Get(profile string) (string, error) {
	out, err := exec.Command("/usr/bin/security", "find-generic-password", "-s", k.Service, "-a", profile, "-w").Output()
	if err != nil {
		return "", fmt.Errorf("keychain: no token for %q", profile)
	}
	return strings.TrimSpace(string(out)), nil
}

func (k Keychain) Set(profile, token string) error {
	// -U updates an existing item. The token is passed as an argument,
	// which other local users could see in the process list for a moment;
	// acceptable for a single-user desktop.
	out, err := exec.Command("/usr/bin/security", "add-generic-password", "-U", "-s", k.Service, "-a", profile, "-w", token).CombinedOutput()
	if err != nil {
		return fmt.Errorf("keychain: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (k Keychain) Delete(profile string) error {
	_ = exec.Command("/usr/bin/security", "delete-generic-password", "-s", k.Service, "-a", profile).Run()
	return nil
}

var _ Secrets = Keychain{}
