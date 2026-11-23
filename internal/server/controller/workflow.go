package controller

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/server/middleware"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/internal/server/service"
	"github.com/austiecodes/solax/pkg/protocol"
)

// StartWorkflow starts a workflow script, or a saved workflow. It returns at
// once.
func StartWorkflow(c *gin.Context) {
	var r protocol.StartWorkflowRequest
	if !bind(c, &r) {
		return
	}
	w, err := service.StartWorkflow(c.Request.Context(), middleware.Actor(c), r)
	reply(c, http.StatusAccepted, model.WireWorkflow(w, nil, false), err)
}

// SaveWorkflow keeps a workflow script under its meta.name.
func SaveWorkflow(c *gin.Context) {
	var r protocol.SaveWorkflowRequest
	if !bind(c, &r) {
		return
	}
	sw, err := service.SaveWorkflowScript(c.Request.Context(), middleware.Actor(c), r.Source)
	reply(c, http.StatusOK, model.WireSaved(sw, false), err)
}

// ListWorkflows returns the saved workflows and the recent runs.
func ListWorkflows(c *gin.Context) {
	var q struct {
		Limit int `form:"limit"`
	}
	if !bindQuery(c, &q) {
		return
	}
	runs, saved, err := service.Workflows(c.Request.Context(), middleware.Actor(c), q.Limit)
	reply(c, http.StatusOK, protocol.WorkflowList{
		Saved: mapAll(saved, func(s model.SavedWorkflow) protocol.SavedWorkflow { return model.WireSaved(s, false) }),
		Runs:  mapAll(runs, func(w model.Workflow) protocol.Workflow { return model.WireWorkflow(w, nil, false) }),
	}, err)
}

// GetWorkflow returns one run (a wfl_ ID, with its steps) or one saved
// workflow (a name, with its recent runs).
func GetWorkflow(c *gin.Context) {
	ref := c.Param("workflow")
	ctx, a := c.Request.Context(), middleware.Actor(c)
	if strings.HasPrefix(ref, string(model.PrefixWorkflow)+"_") {
		w, steps, err := service.WorkflowRun(ctx, a, model.ID(ref))
		run := model.WireWorkflow(w, steps, true)
		reply(c, http.StatusOK, protocol.WorkflowItem{Run: &run}, err)
		return
	}
	sw, runs, err := service.SavedWorkflow(ctx, a, ref)
	saved := model.WireSaved(sw, true)
	reply(c, http.StatusOK, protocol.WorkflowItem{
		Saved: &saved,
		Runs:  mapAll(runs, func(w model.Workflow) protocol.Workflow { return model.WireWorkflow(w, nil, false) }),
	}, err)
}

// CancelWorkflow stops a workflow and its active tasks.
func CancelWorkflow(c *gin.Context) {
	w, err := service.CancelWorkflow(c.Request.Context(), middleware.Actor(c), model.ID(c.Param("workflow")))
	reply(c, http.StatusOK, model.WireWorkflow(w, nil, false), err)
}

// AnswerWorkflow answers the open question of a workflow.
func AnswerWorkflow(c *gin.Context) {
	var r protocol.AnswerRequest
	if !bind(c, &r) {
		return
	}
	w, err := service.AnswerWorkflow(c.Request.Context(), middleware.Actor(c), model.ID(c.Param("workflow")), r.Choice, r.Text)
	reply(c, http.StatusOK, model.WireWorkflow(w, nil, false), err)
}
