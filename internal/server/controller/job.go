package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/server/middleware"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/internal/server/service"
	"github.com/austiecodes/solax/pkg/protocol"
)

// DispatchJob gives a job to an agent. It returns at once.
func DispatchJob(c *gin.Context) {
	var r protocol.DispatchRequest
	if !bind(c, &r) {
		return
	}
	j, err := service.Dispatch(c.Request.Context(), middleware.Actor(c), r.Agent, r.Prompt)
	replyJob(c, http.StatusAccepted, j, err)
}

// ListJobs lists the jobs of the owner, newest first.
func ListJobs(c *gin.Context) {
	var q struct {
		Limit int `form:"limit"`
	}
	if !bindQuery(c, &q) {
		return
	}
	a := middleware.Actor(c)
	js, err := service.Jobs(c.Request.Context(), a, q.Limit)
	if err != nil {
		middleware.Abort(c, err)
		return
	}
	names := agentNames(c)
	reply(c, http.StatusOK, mapAll(js, func(j model.Job) protocol.Job { return model.WireJob(j, names[j.AgentID]) }), nil)
}

// GetJob returns one job.
func GetJob(c *gin.Context) {
	j, err := service.Job(c.Request.Context(), middleware.Actor(c), model.ID(c.Param("job")))
	replyJob(c, http.StatusOK, j, err)
}

// FollowUpJob sends more text to the agent of a job.
func FollowUpJob(c *gin.Context) {
	var r protocol.TextRequest
	if !bind(c, &r) {
		return
	}
	j, err := service.FollowUp(c.Request.Context(), middleware.Actor(c), model.ID(c.Param("job")), r.Text)
	replyJob(c, http.StatusAccepted, j, err)
}

// CancelJob cancels a job.
func CancelJob(c *gin.Context) {
	j, err := service.CancelJob(c.Request.Context(), middleware.Actor(c), model.ID(c.Param("job")))
	replyJob(c, http.StatusOK, j, err)
}

func replyJob(c *gin.Context, code int, j model.Job, err error) {
	if err != nil {
		middleware.Abort(c, err)
		return
	}
	reply(c, code, model.WireJob(j, agentNames(c)[j.AgentID]), nil)
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
