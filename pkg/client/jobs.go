package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Agents.

func (c *Client) CreateAgent(ctx context.Context, r protocol.CreateAgentRequest) (protocol.Agent, error) {
	var out protocol.Agent
	return out, c.do(ctx, http.MethodPost, "/v1/agents", nil, r, &out)
}

func (c *Client) UpdateAgent(ctx context.Context, agent string, r protocol.UpdateAgentRequest) (protocol.Agent, error) {
	var out protocol.Agent
	return out, c.do(ctx, http.MethodPatch, "/v1/agents/"+esc(agent), nil, r, &out)
}

func (c *Client) DeleteAgent(ctx context.Context, agent string) error {
	return c.do(ctx, http.MethodDelete, "/v1/agents/"+esc(agent), nil, nil, nil)
}

// Jobs.

// Dispatch gives a job to an agent. It returns at once with the queued job.
func (c *Client) Dispatch(ctx context.Context, agent, prompt string) (protocol.Job, error) {
	var out protocol.Job
	return out, c.do(ctx, http.MethodPost, "/v1/jobs", nil, protocol.DispatchRequest{Agent: agent, Prompt: prompt}, &out)
}

func (c *Client) Jobs(ctx context.Context, limit int) ([]protocol.Job, error) {
	v := url.Values{}
	if limit > 0 {
		v.Set("limit", strconv.Itoa(limit))
	}
	var out []protocol.Job
	return out, c.do(ctx, http.MethodGet, "/v1/jobs", v, nil, &out)
}

func (c *Client) Job(ctx context.Context, id string) (protocol.Job, error) {
	var out protocol.Job
	return out, c.do(ctx, http.MethodGet, "/v1/jobs/"+esc(id), nil, nil, &out)
}

// FollowUp sends more input to the session of a job.
func (c *Client) FollowUp(ctx context.Context, id, text string) (protocol.Job, error) {
	var out protocol.Job
	return out, c.do(ctx, http.MethodPost, "/v1/jobs/"+esc(id)+"/messages", nil, protocol.TextRequest{Text: text}, &out)
}

func (c *Client) CancelJob(ctx context.Context, id string) (protocol.Job, error) {
	var out protocol.Job
	return out, c.do(ctx, http.MethodPost, "/v1/jobs/"+esc(id)+"/cancel", nil, nil, &out)
}

// SessionEvents returns the stored IR events of a session with seq > after.
func (c *Client) SessionEvents(ctx context.Context, session string, after int64) ([]ir.Event, error) {
	v := url.Values{}
	if after > 0 {
		v.Set("after", strconv.FormatInt(after, 10))
	}
	var out []ir.Event
	return out, c.do(ctx, http.MethodGet, "/v1/sessions/"+esc(session)+"/events", v, nil, &out)
}

// Stream connects to the client stream and calls fn for each push until
// ctx ends or the connection fails. It does not reconnect.
func (c *Client) Stream(ctx context.Context, fn func(protocol.Push)) error {
	return c.StreamReady(ctx, nil, fn)
}

// StreamReady is Stream that calls ready once the connection is up, so a
// client can reload its state knowing that no later push is missed.
func (c *Client) StreamReady(ctx context.Context, ready func(), fn func(protocol.Push)) error {
	u := strings.TrimRight(c.BaseURL, "/") + "/v1/stream"
	u = "ws" + strings.TrimPrefix(u, "http")
	ws, _, err := websocket.DefaultDialer.DialContext(ctx, u, http.Header{"Authorization": {"Bearer " + c.Token}})
	if err != nil {
		return err
	}
	defer ws.Close()
	ws.SetReadLimit(16 << 20)
	// Closing the connection is the only way to stop a blocked read.
	stop := context.AfterFunc(ctx, func() { ws.Close() })
	defer stop()
	if ready != nil {
		ready()
	}
	for {
		// The server pings every 30 seconds; gorilla answers during reads.
		_ = ws.SetReadDeadline(time.Now().Add(90 * time.Second))
		_, b, err := ws.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		var p protocol.Push
		if json.Unmarshal(b, &p) == nil {
			fn(p)
		}
	}
}
