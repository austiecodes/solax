package client

import (
	"context"
	"net/http"

	"github.com/austiecodes/solax/pkg/protocol"
)

// Workflows.

// StartWorkflow starts a workflow script or a saved workflow and returns at
// once.
func (c *Client) StartWorkflow(ctx context.Context, r protocol.StartWorkflowRequest) (protocol.Workflow, error) {
	var out protocol.Workflow
	return out, c.do(ctx, http.MethodPost, "/v1/workflows", nil, r, &out)
}

// SaveWorkflow keeps a script under its meta.name.
func (c *Client) SaveWorkflow(ctx context.Context, source string) (protocol.SavedWorkflow, error) {
	var out protocol.SavedWorkflow
	return out, c.do(ctx, http.MethodPost, "/v1/workflows/saved", nil, protocol.SaveWorkflowRequest{Source: source}, &out)
}

// Workflows returns the saved workflows and the recent runs.
func (c *Client) Workflows(ctx context.Context) (protocol.WorkflowList, error) {
	var out protocol.WorkflowList
	return out, c.do(ctx, http.MethodGet, "/v1/workflows", nil, nil, &out)
}

// Workflow returns one run (ref is a wfl_ ID) or one saved workflow (ref is
// a name).
func (c *Client) Workflow(ctx context.Context, ref string) (protocol.WorkflowItem, error) {
	var out protocol.WorkflowItem
	return out, c.do(ctx, http.MethodGet, "/v1/workflows/"+esc(ref), nil, nil, &out)
}

// CancelWorkflow stops a workflow and its active tasks.
func (c *Client) CancelWorkflow(ctx context.Context, id string) (protocol.Workflow, error) {
	var out protocol.Workflow
	return out, c.do(ctx, http.MethodPost, "/v1/workflows/"+esc(id)+"/cancel", nil, nil, &out)
}

// AnswerWorkflow answers the open question of a workflow.
func (c *Client) AnswerWorkflow(ctx context.Context, id, choice, text string) (protocol.Workflow, error) {
	var out protocol.Workflow
	return out, c.do(ctx, http.MethodPost, "/v1/workflows/"+esc(id)+"/answer", nil, protocol.AnswerRequest{Choice: choice, Text: text}, &out)
}
