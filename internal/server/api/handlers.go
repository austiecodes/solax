package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Each handler binds its request, calls the Engine with actor(c), and maps
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

func (h *Handlers) listMachines(c *gin.Context) {
	a := actor(c)
	ms, err := h.eng.Machines(c.Request.Context(), a)
	reply(c, http.StatusOK, mapAll(ms, wireMachine), err)
}

func (h *Handlers) enrollMachine(c *gin.Context) {
	var r protocol.MachineEnrollment
	if !bind(c, &r) {
		return
	}
	m, err := h.eng.EnrollMachine(c.Request.Context(), r.Name, r.TokenHash)
	reply(c, http.StatusOK, wireEnrolled(m), err)
}

func (h *Handlers) registerMachine(c *gin.Context) {
	var r protocol.MachineEnrollment
	if !bind(c, &r) {
		return
	}
	m, err := h.eng.RegisterMachine(c.Request.Context(), actor(c), r.Name, r.TokenHash)
	reply(c, http.StatusOK, wireEnrolled(m), err)
}

func (h *Handlers) approveMachine(c *gin.Context) {
	var r protocol.ApproveMachineRequest
	if !bind(c, &r) {
		return
	}
	m, err := h.eng.ApproveMachine(c.Request.Context(), actor(c), r.Code)
	reply(c, http.StatusOK, wireMachine(m), err)
}

func (h *Handlers) revokeMachine(c *gin.Context) {
	reply(c, 0, nil, h.eng.RevokeMachine(c.Request.Context(), actor(c), core.ID(c.Param("machine"))))
}

func (h *Handlers) listAgents(c *gin.Context) {
	as, err := h.eng.Agents(c.Request.Context(), actor(c))
	reply(c, http.StatusOK, mapAll(as, wireAgent), err)
}

func (h *Handlers) createAgent(c *gin.Context) {
	var r protocol.CreateAgentRequest
	if !bind(c, &r) {
		return
	}
	ag, err := h.eng.CreateAgent(c.Request.Context(), actor(c), core.AgentSpec{
		Machine: r.Machine, Name: r.Name, Description: r.Description, Driver: r.Driver,
		Dir: r.Dir, Model: r.Model, Options: r.Options, Super: r.Super,
	})
	h.replyAgent(c, http.StatusCreated, ag, err)
}

func (h *Handlers) updateAgent(c *gin.Context) {
	var r protocol.UpdateAgentRequest
	if !bind(c, &r) {
		return
	}
	ag, err := h.eng.UpdateAgent(c.Request.Context(), actor(c), c.Param("agent"), core.AgentPatch{
		Description: r.Description, Dir: r.Dir, Model: r.Model, Options: r.Options, Super: r.Super,
	})
	h.replyAgent(c, http.StatusOK, ag, err)
}

// replyAgent answers with the agent as the list shows it, with its machine.
func (h *Handlers) replyAgent(c *gin.Context, code int, ag core.Agent, err error) {
	if err != nil {
		fail(c, err)
		return
	}
	views, err := h.eng.Agents(c.Request.Context(), actor(c))
	for _, v := range views {
		if v.ID == ag.ID {
			reply(c, code, wireAgent(v), err)
			return
		}
	}
	reply(c, code, wireAgent(core.AgentView{Agent: ag}), err)
}

func (h *Handlers) deleteAgent(c *gin.Context) {
	reply(c, http.StatusNoContent, nil, h.eng.DeleteAgent(c.Request.Context(), actor(c), c.Param("agent")))
}

// Jobs.

func (h *Handlers) dispatch(c *gin.Context) {
	var r protocol.DispatchRequest
	if !bind(c, &r) {
		return
	}
	j, err := h.eng.Dispatch(c.Request.Context(), actor(c), r.Agent, r.Prompt)
	h.replyJob(c, http.StatusAccepted, j, err)
}

func (h *Handlers) listJobs(c *gin.Context) {
	var q struct {
		Limit int `form:"limit"`
	}
	if !bindQuery(c, &q) {
		return
	}
	a := actor(c)
	js, err := h.eng.Jobs(c.Request.Context(), a, q.Limit)
	if err != nil {
		fail(c, err)
		return
	}
	names := h.agentNames(c)
	reply(c, http.StatusOK, mapAll(js, func(j core.Job) protocol.Job { return core.WireJob(j, names[j.AgentID]) }), nil)
}

func (h *Handlers) getJob(c *gin.Context) {
	j, err := h.eng.Job(c.Request.Context(), actor(c), core.ID(c.Param("job")))
	h.replyJob(c, http.StatusOK, j, err)
}

func (h *Handlers) followUpJob(c *gin.Context) {
	var r protocol.TextRequest
	if !bind(c, &r) {
		return
	}
	j, err := h.eng.FollowUp(c.Request.Context(), actor(c), core.ID(c.Param("job")), r.Text)
	h.replyJob(c, http.StatusAccepted, j, err)
}

func (h *Handlers) cancelJob(c *gin.Context) {
	j, err := h.eng.CancelJob(c.Request.Context(), actor(c), core.ID(c.Param("job")))
	h.replyJob(c, http.StatusOK, j, err)
}

func (h *Handlers) sessionEvents(c *gin.Context) {
	var q struct {
		After int64 `form:"after"`
		Limit int   `form:"limit"`
	}
	if !bindQuery(c, &q) {
		return
	}
	evs, err := h.eng.SessionEvents(c.Request.Context(), actor(c), core.ID(c.Param("session")), q.After, q.Limit)
	if evs == nil {
		evs = []ir.Event{}
	}
	reply(c, http.StatusOK, evs, err)
}

func (h *Handlers) replyJob(c *gin.Context, code int, j core.Job, err error) {
	if err != nil {
		fail(c, err)
		return
	}
	reply(c, code, core.WireJob(j, h.agentNames(c)[j.AgentID]), nil)
}

// agentNames maps agent IDs to names, deleted agents included.
func (h *Handlers) agentNames(c *gin.Context) map[core.ID]string {
	out := map[core.ID]string{}
	views, _ := h.eng.Agents(c.Request.Context(), actor(c))
	for _, v := range views {
		out[v.ID] = v.Name
	}
	return out
}

func (h *Handlers) recall(c *gin.Context) {
	obj, err := h.eng.Recall(c.Request.Context(), actor(c), core.ID(c.Param("id")))
	reply(c, http.StatusOK, obj, err)
}

// The Super Agent conversation.

func (h *Handlers) superMessages(c *gin.Context) {
	var q protocol.SuperMessagesQuery
	if !bindQuery(c, &q) {
		return
	}
	ms, err := h.eng.SuperMessages(c.Request.Context(), actor(c), q.Since, q.Limit)
	reply(c, http.StatusOK, mapAll(ms, core.WireMessage), err)
}

func (h *Handlers) sendToSuper(c *gin.Context) {
	var r protocol.TextRequest
	if !bind(c, &r) {
		return
	}
	m, err := h.eng.SendToSuper(c.Request.Context(), actor(c), r.Text)
	reply(c, http.StatusCreated, core.WireMessage(m), err)
}

func (h *Handlers) superReply(c *gin.Context) {
	var r protocol.TextRequest
	if !bind(c, &r) {
		return
	}
	m, err := h.eng.SuperReply(c.Request.Context(), actor(c), r.Text)
	reply(c, http.StatusCreated, core.WireMessage(m), err)
}
