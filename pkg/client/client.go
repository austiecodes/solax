// Package client is the Go client of the Solax REST API. The CLI, the
// daemon and the desktop app use it.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/austiecodes/solax/pkg/protocol"
)

// ErrNotImplemented marks a server endpoint that is not built yet (501).
var ErrNotImplemented = errors.New("client: not implemented")

// Error is a non-2xx answer of the server.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("server: %d %s", e.Status, e.Message) }

// Is lets callers test errors.Is(err, client.ErrNotImplemented).
func (e *Error) Is(target error) bool {
	return target == ErrNotImplemented && e.Status == http.StatusNotImplemented
}

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

// do sends one request. in (if not nil) is the JSON body; out (if not nil)
// receives the JSON answer.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, in, out any) error {
	u := strings.TrimRight(c.BaseURL, "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		var e protocol.ErrorResponse
		if json.Unmarshal(data, &e) != nil || e.Error == "" {
			e.Error = strings.TrimSpace(string(data))
		}
		return &Error{Status: res.StatusCode, Message: e.Error}
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

func esc(s string) string { return url.PathEscape(s) }

func (c *Client) Machines(ctx context.Context) ([]protocol.Machine, error) {
	var out []protocol.Machine
	return out, c.do(ctx, http.MethodGet, "/v1/machines", nil, nil, &out)
}

// EnrollMachine enrolls a machine without a token. The machine stays
// pending until a client approves the pairing code.
func (c *Client) EnrollMachine(ctx context.Context, r protocol.MachineEnrollment) (protocol.MachineEnrolled, error) {
	var out protocol.MachineEnrolled
	return out, c.do(ctx, http.MethodPost, "/v1/machines/enroll", nil, r, &out)
}

// RegisterMachine adds an approved machine. It needs a client token.
func (c *Client) RegisterMachine(ctx context.Context, r protocol.MachineEnrollment) (protocol.MachineEnrolled, error) {
	var out protocol.MachineEnrolled
	return out, c.do(ctx, http.MethodPost, "/v1/machines", nil, r, &out)
}

// ApproveMachine approves a pending machine by its pairing code.
func (c *Client) ApproveMachine(ctx context.Context, code string) (protocol.Machine, error) {
	var out protocol.Machine
	return out, c.do(ctx, http.MethodPost, "/v1/machines/approve", nil, protocol.ApproveMachineRequest{Code: code}, &out)
}

// RevokeMachine makes the token of a machine invalid.
func (c *Client) RevokeMachine(ctx context.Context, machineID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/machines/"+esc(machineID), nil, nil, nil)
}

func (c *Client) Agents(ctx context.Context) ([]protocol.Agent, error) {
	var out []protocol.Agent
	return out, c.do(ctx, http.MethodGet, "/v1/agents", nil, nil, &out)
}

func (c *Client) Workspaces(ctx context.Context) ([]protocol.Workspace, error) {
	var out []protocol.Workspace
	return out, c.do(ctx, http.MethodGet, "/v1/workspaces", nil, nil, &out)
}

// Workspace returns a workspace by ID or name.
func (c *Client) Workspace(ctx context.Context, ws string) (protocol.Workspace, error) {
	var out protocol.Workspace
	return out, c.do(ctx, http.MethodGet, "/v1/workspaces/"+esc(ws), nil, nil, &out)
}

func (c *Client) CreateWorkspace(ctx context.Context, r protocol.CreateWorkspaceRequest) (protocol.Workspace, error) {
	var out protocol.Workspace
	return out, c.do(ctx, http.MethodPost, "/v1/workspaces", nil, r, &out)
}

func (c *Client) AddMember(ctx context.Context, ws string, r protocol.AddMemberRequest) error {
	return c.do(ctx, http.MethodPost, "/v1/workspaces/"+esc(ws)+"/members", nil, r, nil)
}

func (c *Client) RemoveMember(ctx context.Context, ws, agentRef string) error {
	return c.do(ctx, http.MethodDelete, "/v1/workspaces/"+esc(ws)+"/members/"+esc(agentRef), nil, nil, nil)
}

func (c *Client) CreateTask(ctx context.Context, ws string, r protocol.CreateTaskRequest) (protocol.Task, error) {
	var out protocol.Task
	return out, c.do(ctx, http.MethodPost, "/v1/workspaces/"+esc(ws)+"/tasks", nil, r, &out)
}

// Tasks lists the tasks of a workspace, including the general task.
func (c *Client) Tasks(ctx context.Context, ws string) ([]protocol.Task, error) {
	var out []protocol.Task
	return out, c.do(ctx, http.MethodGet, "/v1/workspaces/"+esc(ws)+"/tasks", nil, nil, &out)
}

func (c *Client) Task(ctx context.Context, task string) (protocol.Task, error) {
	var out protocol.Task
	return out, c.do(ctx, http.MethodGet, "/v1/tasks/"+esc(task), nil, nil, &out)
}

func (c *Client) CancelTask(ctx context.Context, task string) error {
	return c.do(ctx, http.MethodPost, "/v1/tasks/"+esc(task)+"/cancel", nil, nil, nil)
}

func (c *Client) SummarizeTask(ctx context.Context, task string) error {
	return c.do(ctx, http.MethodPost, "/v1/tasks/"+esc(task)+"/summarize", nil, nil, nil)
}

func (c *Client) PostMessage(ctx context.Context, ws string, r protocol.PostMessageRequest) (protocol.Message, error) {
	var out protocol.Message
	return out, c.do(ctx, http.MethodPost, "/v1/workspaces/"+esc(ws)+"/messages", nil, r, &out)
}

func (c *Client) ReadMessages(ctx context.Context, ws string, q protocol.ReadMessagesQuery) ([]protocol.Message, error) {
	v := url.Values{}
	if q.TaskID != "" {
		v.Set("task", q.TaskID)
	}
	if q.Since > 0 {
		v.Set("since", strconv.FormatInt(q.Since, 10))
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	var out []protocol.Message
	return out, c.do(ctx, http.MethodGet, "/v1/workspaces/"+esc(ws)+"/messages", v, nil, &out)
}

func (c *Client) SuperMessages(ctx context.Context, since int64) ([]protocol.Message, error) {
	v := url.Values{}
	if since > 0 {
		v.Set("since", strconv.FormatInt(since, 10))
	}
	var out []protocol.Message
	return out, c.do(ctx, http.MethodGet, "/v1/super/messages", v, nil, &out)
}

func (c *Client) SendToSuper(ctx context.Context, text string) (protocol.Message, error) {
	var out protocol.Message
	return out, c.do(ctx, http.MethodPost, "/v1/super/messages", nil, protocol.TextRequest{Text: text}, &out)
}

// Ping checks the server and the token.
func (c *Client) Ping(ctx context.Context) error {
	if err := c.do(ctx, http.MethodGet, "/healthz", nil, nil, nil); err != nil {
		return err
	}
	if c.Token == "" {
		return nil
	}
	return c.do(ctx, http.MethodGet, "/v1/agents", nil, nil, nil)
}

func (c *Client) Recall(ctx context.Context, id string) (protocol.Object, error) {
	var out protocol.Object
	return out, c.do(ctx, http.MethodGet, "/v1/objects/"+esc(id), nil, nil, &out)
}
