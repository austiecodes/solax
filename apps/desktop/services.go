package main

import (
	"context"

	"github.com/austiecodes/solax/apps/desktop/internal/profile"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Services are called directly by native Go views through pkg/client.

// Profiles manages server profiles.
type Profiles struct{ a *app }

// List returns all profiles.
func (s Profiles) List() []profile.Profile { return s.a.profiles.List() }

// Current returns the profile in use.
func (s Profiles) Current() (profile.Profile, error) {
	p, _, err := s.a.profiles.Current()
	return p, err
}

// Save adds or replaces a profile. The token goes to the OS keychain.
func (s Profiles) Save(p profile.Profile, token string) error { return s.a.profiles.Save(p, token) }

// Use selects a profile and connects to its server.
func (s Profiles) Use(name string) error {
	if err := s.a.profiles.Use(name); err != nil {
		return err
	}
	return s.a.connect()
}

// Remove deletes a profile.
func (s Profiles) Remove(name string) error { return s.a.profiles.Remove(name) }

// Test checks the server and the token of the profile in use.
func (s Profiles) Test(ctx context.Context) error {
	c, err := s.a.api()
	if err != nil {
		return err
	}
	return c.Ping(ctx)
}

// Super is the conversation with the Super Agent.
type Super struct{ a *app }

// History returns the messages after seq since.
func (s Super) History(ctx context.Context, since int64) ([]protocol.Message, error) {
	c, err := s.a.api()
	if err != nil {
		return nil, err
	}
	return c.SuperMessages(ctx, since)
}

// Send sends a user message to the Super Agent.
func (s Super) Send(ctx context.Context, text string) (protocol.Message, error) {
	c, err := s.a.api()
	if err != nil {
		return protocol.Message{}, err
	}
	return c.SendToSuper(ctx, text)
}

// Workspaces reads workspaces and posts into their tasks.
type Workspaces struct{ a *app }

func (s Workspaces) List(ctx context.Context) ([]protocol.Workspace, error) {
	c, err := s.a.api()
	if err != nil {
		return nil, err
	}
	return c.Workspaces(ctx)
}

func (s Workspaces) Get(ctx context.Context, ws string) (protocol.Workspace, error) {
	c, err := s.a.api()
	if err != nil {
		return protocol.Workspace{}, err
	}
	return c.Workspace(ctx, ws)
}

// Messages reads the messages of a task with seq > since.
func (s Workspaces) Messages(ctx context.Context, ws, task string, since int64) ([]protocol.Message, error) {
	c, err := s.a.api()
	if err != nil {
		return nil, err
	}
	return c.ReadMessages(ctx, ws, protocol.ReadMessagesQuery{TaskID: task, Since: since, Limit: 200})
}

// Post posts into a task directly. This is the fast path: the message
// does not go through the Super Agent, which gets only a notice.
func (s Workspaces) Post(ctx context.Context, ws string, req protocol.PostMessageRequest) (protocol.Message, error) {
	c, err := s.a.api()
	if err != nil {
		return protocol.Message{}, err
	}
	return c.PostMessage(ctx, ws, req)
}

// Tasks reads and controls tasks.
type Tasks struct{ a *app }

// List returns the tasks of a workspace, including the general task.
func (s Tasks) List(ctx context.Context, ws string) ([]protocol.Task, error) {
	c, err := s.a.api()
	if err != nil {
		return nil, err
	}
	return c.Tasks(ctx, ws)
}

func (s Tasks) Get(ctx context.Context, task string) (protocol.Task, error) {
	c, err := s.a.api()
	if err != nil {
		return protocol.Task{}, err
	}
	return c.Task(ctx, task)
}

func (s Tasks) Cancel(ctx context.Context, task string) error {
	c, err := s.a.api()
	if err != nil {
		return err
	}
	return c.CancelTask(ctx, task)
}

func (s Tasks) Summarize(ctx context.Context, task string) error {
	c, err := s.a.api()
	if err != nil {
		return err
	}
	return c.SummarizeTask(ctx, task)
}

// Machines lists the machines and their agents.
type Machines struct{ a *app }

func (s Machines) List(ctx context.Context) ([]protocol.Machine, error) {
	c, err := s.a.api()
	if err != nil {
		return nil, err
	}
	return c.Machines(ctx)
}

// Approve approves a pending machine by the pairing code that its daemon
// printed.
func (s Machines) Approve(ctx context.Context, code string) (protocol.Machine, error) {
	c, err := s.a.api()
	if err != nil {
		return protocol.Machine{}, err
	}
	return c.ApproveMachine(ctx, code)
}

// Revoke makes the token of a machine invalid.
func (s Machines) Revoke(ctx context.Context, machineID string) error {
	c, err := s.a.api()
	if err != nil {
		return err
	}
	return c.RevokeMachine(ctx, machineID)
}
