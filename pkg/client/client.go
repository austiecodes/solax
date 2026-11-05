// Package client is the Go client of the Solax REST API. The CLI, the
// daemon and the desktop app use it.
package client

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/austiecodes/solax/pkg/protocol"
)

// ErrNotImplemented marks a skeleton method.
var ErrNotImplemented = errors.New("client: not implemented")

// Client calls one Solax server.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New returns a client for baseURL with a bearer token.
func New(baseURL, token string) *Client {
	return &Client{BaseURL: baseURL, Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) Machines(ctx context.Context) ([]protocol.Machine, error) {
	return nil, ErrNotImplemented
}

// EnrollMachine enrolls a machine without a token. The machine stays
// pending until a client approves the pairing code.
func (c *Client) EnrollMachine(ctx context.Context, r protocol.MachineEnrollment) (protocol.MachineEnrolled, error) {
	return protocol.MachineEnrolled{}, ErrNotImplemented
}

// RegisterMachine adds an approved machine. It needs a client token.
func (c *Client) RegisterMachine(ctx context.Context, r protocol.MachineEnrollment) (protocol.MachineEnrolled, error) {
	return protocol.MachineEnrolled{}, ErrNotImplemented
}

// ApproveMachine approves a pending machine by its pairing code.
func (c *Client) ApproveMachine(ctx context.Context, code string) (protocol.Machine, error) {
	return protocol.Machine{}, ErrNotImplemented
}

// RevokeMachine makes the token of a machine invalid.
func (c *Client) RevokeMachine(ctx context.Context, machineID string) error {
	return ErrNotImplemented
}

func (c *Client) Agents(ctx context.Context) ([]protocol.Agent, error) {
	return nil, ErrNotImplemented
}

func (c *Client) Workspaces(ctx context.Context) ([]protocol.Workspace, error) {
	return nil, ErrNotImplemented
}

func (c *Client) Workspace(ctx context.Context, ws string) (protocol.Workspace, error) {
	return protocol.Workspace{}, ErrNotImplemented
}

func (c *Client) CreateWorkspace(ctx context.Context, r protocol.CreateWorkspaceRequest) (protocol.Workspace, error) {
	return protocol.Workspace{}, ErrNotImplemented
}

func (c *Client) AddMember(ctx context.Context, ws string, r protocol.AddMemberRequest) error {
	return ErrNotImplemented
}

func (c *Client) RemoveMember(ctx context.Context, ws, agentRef string) error {
	return ErrNotImplemented
}

func (c *Client) CreateTask(ctx context.Context, ws string, r protocol.CreateTaskRequest) (protocol.Task, error) {
	return protocol.Task{}, ErrNotImplemented
}

func (c *Client) Task(ctx context.Context, task string) (protocol.Task, error) {
	return protocol.Task{}, ErrNotImplemented
}

func (c *Client) CancelTask(ctx context.Context, task string) error {
	return ErrNotImplemented
}

func (c *Client) SummarizeTask(ctx context.Context, task string) error {
	return ErrNotImplemented
}

func (c *Client) PostMessage(ctx context.Context, ws string, r protocol.PostMessageRequest) (protocol.Message, error) {
	return protocol.Message{}, ErrNotImplemented
}

func (c *Client) ReadMessages(ctx context.Context, ws string, q protocol.ReadMessagesQuery) ([]protocol.Message, error) {
	return nil, ErrNotImplemented
}

func (c *Client) SuperMessages(ctx context.Context, since int64) ([]protocol.Message, error) {
	return nil, ErrNotImplemented
}

func (c *Client) SendToSuper(ctx context.Context, text string) (protocol.Message, error) {
	return protocol.Message{}, ErrNotImplemented
}

// Ping checks the server and the token.
func (c *Client) Ping(ctx context.Context) error {
	return ErrNotImplemented
}

func (c *Client) Recall(ctx context.Context, id string) (protocol.Object, error) {
	return protocol.Object{}, ErrNotImplemented
}
