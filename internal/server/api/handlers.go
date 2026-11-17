package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/internal/server/service"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Each handler binds its request, calls the service with actor(c), and maps
// the result to its wire form. Handlers contain no domain rules.

// bind decodes a JSON body; a bad body is ErrInvalid (400).
func bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		fail(c, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return false
	}
	return true
}

func bindQuery(c *gin.Context, v any) bool {
	if err := c.ShouldBindQuery(v); err != nil {
		fail(c, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return false
	}
	return true
}

// reply writes v, or the status of err.
func reply(c *gin.Context, code int, v any, err error) {
	if err != nil {
		fail(c, err)
		return
	}
	if v == nil {
		c.Status(http.StatusNoContent)
		return
	}
	c.JSON(code, v)
}

// Machines and agents.

func listMachines(c *gin.Context) {
	a := actor(c)
	ms, err := service.Machines(c.Request.Context(), a)
	reply(c, http.StatusOK, mapAll(ms, wireMachine), err)
}

func enrollMachine(c *gin.Context) {
	var r protocol.MachineEnrollment
	if !bind(c, &r) {
		return
	}
	m, err := service.EnrollMachine(c.Request.Context(), r.Name, r.TokenHash)
	reply(c, http.StatusOK, wireEnrolled(m), err)
}

func registerMachine(c *gin.Context) {
	var r protocol.MachineEnrollment
	if !bind(c, &r) {
		return
	}
	m, err := service.RegisterMachine(c.Request.Context(), actor(c), r.Name, r.TokenHash)
	reply(c, http.StatusOK, wireEnrolled(m), err)
}

func approveMachine(c *gin.Context) {
	var r protocol.ApproveMachineRequest
	if !bind(c, &r) {
		return
	}
	m, err := service.ApproveMachine(c.Request.Context(), actor(c), r.Code)
	reply(c, http.StatusOK, wireMachine(m), err)
}

func revokeMachine(c *gin.Context) {
	reply(c, 0, nil, service.RevokeMachine(c.Request.Context(), actor(c), model.ID(c.Param("machine"))))
}

func listAgents(c *gin.Context) {
	as, err := service.Agents(c.Request.Context(), actor(c))
	reply(c, http.StatusOK, mapAll(as, wireAgent), err)
}

func createAgent(c *gin.Context) {
	var r protocol.CreateAgentRequest
	if !bind(c, &r) {
		return
	}
	ag, err := service.CreateAgent(c.Request.Context(), actor(c), service.AgentSpec{
		Machine: r.Machine, Name: r.Name, Description: r.Description, Instructions: r.Instructions, Runtime: r.Runtime,
		Dir: r.Dir, Model: r.Model, Options: r.Options, Super: r.Super,
	})
	replyAgent(c, http.StatusCreated, ag, err)
}

func updateAgent(c *gin.Context) {
	var r protocol.UpdateAgentRequest
	if !bind(c, &r) {
		return
	}
	ag, err := service.UpdateAgent(c.Request.Context(), actor(c), c.Param("agent"), service.AgentPatch{
		Description: r.Description, Instructions: r.Instructions, Dir: r.Dir, Model: r.Model, Options: r.Options, Super: r.Super,
	})
	replyAgent(c, http.StatusOK, ag, err)
}

// replyAgent answers with the agent as the list shows it, with its machine.
func replyAgent(c *gin.Context, code int, ag model.Agent, err error) {
	if err != nil {
		fail(c, err)
		return
	}
	views, err := service.Agents(c.Request.Context(), actor(c))
	for _, v := range views {
		if v.ID == ag.ID {
			reply(c, code, wireAgent(v), err)
			return
		}
	}
	reply(c, code, wireAgent(service.AgentView{Agent: ag}), err)
}

func deleteAgent(c *gin.Context) {
	reply(c, http.StatusNoContent, nil, service.DeleteAgent(c.Request.Context(), actor(c), c.Param("agent")))
}

// Jobs.

func dispatch(c *gin.Context) {
	var r protocol.DispatchRequest
	if !bind(c, &r) {
		return
	}
	j, err := service.Dispatch(c.Request.Context(), actor(c), r.Agent, r.Prompt)
	replyJob(c, http.StatusAccepted, j, err)
}

func listJobs(c *gin.Context) {
	var q struct {
		Limit int `form:"limit"`
	}
	if !bindQuery(c, &q) {
		return
	}
	a := actor(c)
	js, err := service.Jobs(c.Request.Context(), a, q.Limit)
	if err != nil {
		fail(c, err)
		return
	}
	names := agentNames(c)
	reply(c, http.StatusOK, mapAll(js, func(j model.Job) protocol.Job { return model.WireJob(j, names[j.AgentID]) }), nil)
}

func getJob(c *gin.Context) {
	j, err := service.Job(c.Request.Context(), actor(c), model.ID(c.Param("job")))
	replyJob(c, http.StatusOK, j, err)
}

func followUpJob(c *gin.Context) {
	var r protocol.TextRequest
	if !bind(c, &r) {
		return
	}
	j, err := service.FollowUp(c.Request.Context(), actor(c), model.ID(c.Param("job")), r.Text)
	replyJob(c, http.StatusAccepted, j, err)
}

func cancelJob(c *gin.Context) {
	j, err := service.CancelJob(c.Request.Context(), actor(c), model.ID(c.Param("job")))
	replyJob(c, http.StatusOK, j, err)
}

func sessionEvents(c *gin.Context) {
	var q struct {
		After int64 `form:"after"`
		Limit int   `form:"limit"`
	}
	if !bindQuery(c, &q) {
		return
	}
	evs, err := service.SessionEvents(c.Request.Context(), actor(c), model.ID(c.Param("session")), q.After, q.Limit)
	if evs == nil {
		evs = []ir.Event{}
	}
	reply(c, http.StatusOK, evs, err)
}

func replyJob(c *gin.Context, code int, j model.Job, err error) {
	if err != nil {
		fail(c, err)
		return
	}
	reply(c, code, model.WireJob(j, agentNames(c)[j.AgentID]), nil)
}

// agentNames maps agent IDs to names, deleted agents included.
func agentNames(c *gin.Context) map[model.ID]string {
	out := map[model.ID]string{}
	views, _ := service.Agents(c.Request.Context(), actor(c))
	for _, v := range views {
		out[v.ID] = v.Name
	}
	return out
}

func recall(c *gin.Context) {
	obj, err := service.Recall(c.Request.Context(), actor(c), model.ID(c.Param("id")))
	reply(c, http.StatusOK, obj, err)
}

// The Super Agent conversation.

func superMessages(c *gin.Context) {
	var q protocol.SuperMessagesQuery
	if !bindQuery(c, &q) {
		return
	}
	ms, err := service.SuperMessages(c.Request.Context(), actor(c), q.Since, q.Limit)
	reply(c, http.StatusOK, mapAll(ms, model.WireMessage), err)
}

func sendToSuper(c *gin.Context) {
	var r protocol.TextRequest
	if !bind(c, &r) {
		return
	}
	m, err := service.SendToSuper(c.Request.Context(), actor(c), r.Text)
	reply(c, http.StatusCreated, model.WireMessage(m), err)
}

func superReply(c *gin.Context) {
	var r protocol.TextRequest
	if !bind(c, &r) {
		return
	}
	m, err := service.SuperReply(c.Request.Context(), actor(c), r.Text)
	reply(c, http.StatusCreated, model.WireMessage(m), err)
}
