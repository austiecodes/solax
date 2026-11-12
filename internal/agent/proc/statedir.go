package proc

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SessionDir is the per-session state folder of a driver: the role prompt
// file and the stderr log. It never lives in the repository of the user.
type SessionDir struct {
	Path string // "" when the driver has no state folder
}

// NewSessionDir creates <stateDir>/<sessionID>. With an empty stateDir or
// sessionID it returns a SessionDir that keeps nothing on disk.
func NewSessionDir(stateDir, sessionID string) (SessionDir, error) {
	if stateDir == "" || sessionID == "" {
		return SessionDir{}, nil
	}
	p := filepath.Join(stateDir, sessionID)
	return SessionDir{Path: p}, os.MkdirAll(p, 0o700)
}

// WriteFile writes a file in the folder and returns its path. ok is false
// when the folder keeps nothing on disk.
func (d SessionDir) WriteFile(name, content string) (path string, ok bool, err error) {
	if d.Path == "" {
		return "", false, nil
	}
	path = filepath.Join(d.Path, name)
	return path, true, os.WriteFile(path, []byte(content), 0o600)
}

// Log opens the stderr log of the session for append. It returns
// io.Discard when the folder keeps nothing on disk.
func (d SessionDir) Log() io.Writer {
	if d.Path == "" {
		return io.Discard
	}
	f, err := os.OpenFile(filepath.Join(d.Path, "stderr.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return io.Discard
	}
	return f
}

// LastLine returns the last line of s.
func LastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}
