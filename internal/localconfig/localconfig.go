// Package localconfig reads and writes the local state of one machine in
// ~/.solax (or $SOLAX_HOME). The CLI and the daemon share it.
package localconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// EnvHome overrides the local state directory.
const EnvHome = "SOLAX_HOME"

const (
	configFile      = "config.yaml"
	credentialsFile = "credentials.yaml"
)

// Home returns the local state directory.
func Home() (string, error) {
	if h := os.Getenv(EnvHome); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".solax"), nil
}

// Paths are the files in the local state directory.
type Paths struct{ Dir string }

func (p Paths) Config() string      { return filepath.Join(p.Dir, configFile) }
func (p Paths) Credentials() string { return filepath.Join(p.Dir, credentialsFile) }
func (p Paths) Socket() string      { return filepath.Join(p.Dir, "daemon.sock") }
func (p Paths) PID() string         { return filepath.Join(p.Dir, "daemon.pid") }
func (p Paths) Log() string         { return filepath.Join(p.Dir, "daemon.log") }
func (p Paths) State() string       { return filepath.Join(p.Dir, "state") }

// Config is config.yaml. It holds no secrets.
type Config struct {
	Backend Backend `yaml:"backend,omitempty" json:"backend"`
	Machine Machine `yaml:"machine,omitempty" json:"machine"`
}

// Backend is the server that this machine uses.
type Backend struct {
	URL string `yaml:"url,omitempty" json:"url,omitempty"`
}

// Machine holds the daemon settings of this machine.
type Machine struct {
	Name        string `yaml:"name,omitempty" json:"name,omitempty"`
	MaxSessions int    `yaml:"max_sessions,omitempty" json:"max_sessions,omitempty"`
	StateDir    string `yaml:"state_dir,omitempty" json:"state_dir,omitempty"`
}

// Credentials is credentials.yaml (mode 0600).
//
// ClientToken comes from the server admin ("solax-server token create").
// MachineToken is generated on this machine by "solax daemon start". The
// server keeps only its hash.
type Credentials struct {
	ClientToken  string `yaml:"client_token,omitempty" json:"client_token,omitempty"`
	MachineID    string `yaml:"machine_id,omitempty" json:"machine_id,omitempty"`
	MachineToken string `yaml:"machine_token,omitempty" json:"machine_token,omitempty"`
}

// Load reads config.yaml as written. A missing file gives an empty
// config. Commands that change the file use Load and Save, so defaults are
// never written to the file.
func (p Paths) Load() (Config, error) {
	var c Config
	return c, readYAML(p.Config(), &c)
}

// LoadEffective reads config.yaml and fills in the defaults.
func (p Paths) LoadEffective() (Config, error) {
	c, err := p.Load()
	if err != nil {
		return Config{}, err
	}
	return c.withDefaults(p), nil
}

// Save writes config.yaml.
func (p Paths) Save(c Config) error { return writeYAML(p.Config(), c, 0o644) }

// LoadCredentials reads credentials.yaml. A missing file gives no credentials.
func (p Paths) LoadCredentials() (Credentials, error) {
	var c Credentials
	return c, readYAML(p.Credentials(), &c)
}

// SaveCredentials writes credentials.yaml with mode 0600.
func (p Paths) SaveCredentials(c Credentials) error { return writeYAML(p.Credentials(), c, 0o600) }

func (c Config) withDefaults(p Paths) Config {
	if c.Machine.Name == "" {
		c.Machine.Name, _ = os.Hostname()
	}
	if c.Machine.MaxSessions <= 0 {
		c.Machine.MaxSessions = 4
	}
	c.Machine.StateDir = expandHome(c.Machine.StateDir)
	if c.Machine.StateDir == "" {
		c.Machine.StateDir = p.State()
	}
	return c
}

// expandHome turns a leading "~/" into the home directory. Any other path
// is returned as written. A path that cannot be expanded stays as is; the
// daemon then fails on it with a clear error.
func expandHome(p string) string {
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return p
	}
	if !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[2:])
}

// Validate checks what the daemon needs: a backend.
func (c Config) Validate() error {
	if c.Backend.URL == "" {
		return errors.New(`no backend: run "solax set backend --url URL"`)
	}
	return nil
}

// ExpandHome turns a leading "~/" into the home directory. The daemon uses
// it for the work folder of an agent, which a person may write as "~/src".
func ExpandHome(p string) string { return expandHome(p) }

func readYAML(path string, v any) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// writeYAML writes through a uniquely named temp file and a rename, so a
// crash never leaves a partial file and two processes never write the same
// temp file.
func writeYAML(path string, v any, mode fs.FileMode) error {
	b, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".solax-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	if _, err = f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
