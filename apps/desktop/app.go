package main

import (
	"errors"
	"sync"

	"github.com/austiecodes/solax/apps/desktop/internal/profile"
	"github.com/austiecodes/solax/pkg/client"
)

// errNoProfile is returned when no server profile is in use.
var errNoProfile = errors.New("no server profile: add one in Settings")

// app is the state that the bound services share. The token stays here. The
// page never sees it.
type app struct {
	profiles *profile.Store
	stream   *stream

	mu     sync.RWMutex
	client *client.Client
}

// api returns the client of the profile in use.
func (a *app) api() (*client.Client, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.client == nil {
		return nil, errNoProfile
	}
	return a.client, nil
}

// connect switches to the current profile: a new client and a new stream.
func (a *app) connect() error {
	p, token, err := a.profiles.Current()
	if err != nil {
		return err
	}
	c := client.New(p.Server, token)
	a.mu.Lock()
	a.client = c
	a.mu.Unlock()
	a.stream.Restart(p.Server, token)
	return nil
}
