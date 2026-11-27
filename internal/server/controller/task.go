package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/server/middleware"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/internal/server/service"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// DispatchTask gives a task to an agent. It returns at once.
func DispatchTask(c *gin.Context) {
	var r protocol.DispatchRequest
	if !bind(c, &r) {
		return
	}
	j, err := service.Dispatch(c.Request.Context(), middleware.Actor(c), r.Agent, r.Project, r.Prompt)
	replyTask(c, http.StatusAccepted, j, err)
}

// ListTasks lists the tasks of the owner, newest first.
func ListTasks(c *gin.Context) {
	var q struct {
		Limit int `form:"limit"`
	}
	if !bindQuery(c, &q) {
		return
	}
	a := middleware.Actor(c)
	js, err := service.Tasks(c.Request.Context(), a, q.Limit)
	if err != nil {
		middleware.Abort(c, err)
		return
	}
	agents, projects := agentNames(c), projectNames(c)
	reply(c, http.StatusOK, mapAll(js, func(j model.Task) protocol.Task {
		return model.WireTask(j, agents[j.AgentID], projects[j.ProjectID])
	}), nil)
}

// GetTask returns one task.
func GetTask(c *gin.Context) {
	j, err := service.Task(c.Request.Context(), middleware.Actor(c), model.ID(c.Param("task")))
	replyTask(c, http.StatusOK, j, err)
}

// FollowUpTask sends more text to the agent of a task: a follow-up, or a
// steer of the running turn.
func FollowUpTask(c *gin.Context) {
	var r protocol.FollowUpRequest
	if !bind(c, &r) {
		return
	}
	j, err := service.FollowUp(c.Request.Context(), middleware.Actor(c), model.ID(c.Param("task")), r.Text, ir.InputMode(r.Mode))
	replyTask(c, http.StatusAccepted, j, err)
}

// CancelTask cancels a task.
func CancelTask(c *gin.Context) {
	j, err := service.CancelTask(c.Request.Context(), middleware.Actor(c), model.ID(c.Param("task")))
	replyTask(c, http.StatusOK, j, err)
}

// InterruptTask stops the running turn of a task.
func InterruptTask(c *gin.Context) {
	j, err := service.InterruptTask(c.Request.Context(), middleware.Actor(c), model.ID(c.Param("task")))
	replyTask(c, http.StatusOK, j, err)
}

func replyTask(c *gin.Context, code int, j model.Task, err error) {
	if err != nil {
		middleware.Abort(c, err)
		return
	}
	reply(c, code, model.WireTask(j, agentNames(c)[j.AgentID], projectNames(c)[j.ProjectID]), nil)
}

// agentNames maps agent IDs to names, deleted agents included.
func agentNames(c *gin.Context) map[model.ID]string {
	out := map[model.ID]string{}
	views, _ := service.Agents(c.Request.Context(), middleware.Actor(c))
	for _, v := range views {
		out[v.ID] = v.Name
	}
	return out
}

// projectNames maps project IDs to names, deleted projects included.
func projectNames(c *gin.Context) map[model.ID]string {
	out, _ := service.ProjectNames(c.Request.Context(), middleware.Actor(c))
	return out
}
