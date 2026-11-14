package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// prefs are what the window remembers between runs. The data itself
// (jobs, agents, the conversation) is the server's.
type prefs struct {
	Page          string  `json:"page,omitempty"`
	Job           string  `json:"job,omitempty"` // the job open on the jobs page
	SidebarWidth  float32 `json:"sidebarWidth,omitempty"`
	SidebarHidden bool    `json:"sidebarHidden,omitempty"`
	// Appearance is "light", "dark" or "" for the system's; Theme is one
	// of themeNames (theme.go), "" for the default.
	Appearance string `json:"appearance,omitempty"`
	Theme      string `json:"theme,omitempty"`
	// Unread are the jobs that ended unseen; SeenAt is when the app last
	// knew all jobs, so a job that ended after it, while the app was
	// closed, is unread too (unread.go).
	Unread []string  `json:"unread,omitempty"`
	SeenAt time.Time `json:"seenAt,omitzero"`
}

const (
	defaultSidebar = 268
	minSidebar     = 200
	maxSidebar     = 440
)

// loadPrefs reads the prefs at path. A missing or bad file gives the
// defaults: prefs are a convenience, never a reason to fail.
func loadPrefs(path string) prefs {
	var p prefs
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(b, &p)
		}
	}
	if p.SidebarWidth == 0 {
		p.SidebarWidth = defaultSidebar
	}
	p.SidebarWidth = min(max(p.SidebarWidth, minSidebar), maxSidebar)
	if _, ok := looks[p.Theme]; !ok {
		p.Theme = defaultTheme
	}
	if !validPage(p.Page) {
		p.Page = pageChat
	}
	return p
}

// save writes the prefs to path through a temporary file, so a crash
// leaves the old file or the new one. An empty path saves nothing.
func (p prefs) save(path string) error {
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
