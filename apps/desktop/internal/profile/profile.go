// Package profile stores the server profiles of the desktop app. A profile
// names one Solax server. The token of a profile is kept in the OS keychain,
// never in the profile file and never in the page.
package profile

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// ErrNotImplemented marks a skeleton function.
var ErrNotImplemented = errors.New("profile: not implemented")

// Profile is one server configuration.
type Profile struct {
	Name   string `json:"name"`
	Server string `json:"server"` // base URL, for example https://solax.example.com
}

// Secrets keeps tokens. The v1 implementation uses the OS keychain.
type Secrets interface {
	Get(profile string) (string, error)
	Set(profile, token string) error
	Delete(profile string) error
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
	return &Store{path: path, secrets: s}, nil
}

// List returns all profiles.
func (s *Store) List() []Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Profile(nil), s.profiles...)
}

// Current returns the profile in use and its token.
func (s *Store) Current() (Profile, string, error) {
	return Profile{}, "", ErrNotImplemented
}

// Save adds or replaces a profile and stores its token.
func (s *Store) Save(p Profile, token string) error { return ErrNotImplemented }

// Use selects the profile in use.
func (s *Store) Use(name string) error { return ErrNotImplemented }

// Remove deletes a profile and its token.
func (s *Store) Remove(name string) error { return ErrNotImplemented }

// Keychain is the Secrets implementation on the OS keychain.
type Keychain struct {
	Service string // for example "dev.austiecodes.solax"
}

func (k Keychain) Get(profile string) (string, error) { return "", ErrNotImplemented }
func (k Keychain) Set(profile, token string) error    { return ErrNotImplemented }
func (k Keychain) Delete(profile string) error        { return ErrNotImplemented }

var _ Secrets = Keychain{}
