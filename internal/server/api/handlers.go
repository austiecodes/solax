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
// the result to a protocol DTO. Handlers contain no domain rules.

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
	ms, err := h.q.Machines(c.Request.Context(), a)
	reply(c, http.StatusOK, mapAll(ms, machineDTO), err)
}

func (h *Handlers) enrollMachine(c *gin.Context) {
	var r protocol.MachineEnrollment
	if !bind(c, &r) {
		return
	}
	m, err := h.enroll.EnrollMachine(c.Request.Context(), r.Name, r.TokenHash)
	reply(c, http.StatusOK, enrolledDTO(m), err)
}

func (h *Handlers) registerMachine(c *gin.Context) {
	var r protocol.MachineEnrollment
	if !bind(c, &r) {
		return
	}
	m, err := h.cmd.RegisterMachine(c.Request.Context(), actor(c), r.Name, r.TokenHash)
	reply(c, http.StatusOK, enrolledDTO(m), err)
}

func (h *Handlers) approveMachine(c *gin.Context) {
	var r protocol.ApproveMachineRequest
	if !bind(c, &r) {
		return
	}
	m, err := h.cmd.ApproveMachine(c.Request.Context(), actor(c), r.Code)
	reply(c, http.StatusOK, machineDTO(m), err)
}

func (h *Handlers) revokeMachine(c *gin.Context) {
	reply(c, 0, nil, h.cmd.RevokeMachine(c.Request.Context(), actor(c), core.ID(c.Param("machine"))))
}

func (h *Handlers) listAgents(c *gin.Context) {
	as, err := h.q.Agents(c.Request.Context(), actor(c))
	reply(c, http.StatusOK, mapAll(as, agentDTO), err)
}

func (h *Handlers) createAgent(c *gin.Context) {
	var r protocol.CreateAgentRequest
	if !bind(c, &r) {
		return
	}
	ag, err := h.cmd.CreateAgent(c.Request.Context(), actor(c), core.AgentSpec{
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
	ag, err := h.cmd.UpdateAgent(c.Request.Context(), actor(c), c.Param("agent"), core.AgentPatch{
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
	views, err := h.q.Agents(c.Request.Context(), actor(c))
	for _, v := range views {
		if v.ID == ag.ID {
			reply(c, code, agentDTO(v), err)
			return
		}
	}
	reply(c, code, agentDTO(core.AgentView{Agent: ag}), err)
}

func (h *Handlers) deleteAgent(c *gin.Context) {
	reply(c, http.StatusNoContent, nil, h.cmd.DeleteAgent(c.Request.Context(), actor(c), c.Param("agent")))
}

// Jobs.

func (h *Handlers) dispatch(c *gin.Context) {
	var r protocol.DispatchRequest
	if !bind(c, &r) {
		return
	}
	j, err := h.cmd.Dispatch(c.Request.Context(), actor(c), r.Agent, r.Prompt)
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
	js, err := h.q.Jobs(c.Request.Context(), a, q.Limit)
	if err != nil {
		fail(c, err)
		return
	}
	names := h.agentNames(c)
	reply(c, http.StatusOK, mapAll(js, func(j core.Job) protocol.Job { return core.JobDTO(j, names[j.AgentID]) }), nil)
}

func (h *Handlers) getJob(c *gin.Context) {
	j, err := h.q.Job(c.Request.Context(), actor(c), core.ID(c.Param("job")))
	h.replyJob(c, http.StatusOK, j, err)
}

func (h *Handlers) followUpJob(c *gin.Context) {
	var r protocol.TextRequest
	if !bind(c, &r) {
		return
	}
	j, err := h.cmd.FollowUp(c.Request.Context(), actor(c), core.ID(c.Param("job")), r.Text)
	h.replyJob(c, http.StatusAccepted, j, err)
}

func (h *Handlers) cancelJob(c *gin.Context) {
	j, err := h.cmd.CancelJob(c.Request.Context(), actor(c), core.ID(c.Param("job")))
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
	evs, err := h.q.SessionEvents(c.Request.Context(), actor(c), core.ID(c.Param("session")), q.After, q.Limit)
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
	reply(c, code, core.JobDTO(j, h.agentNames(c)[j.AgentID]), nil)
}

// agentNames maps agent IDs to names, deleted agents included.
func (h *Handlers) agentNames(c *gin.Context) map[core.ID]string {
	out := map[core.ID]string{}
	views, _ := h.q.Agents(c.Request.Context(), actor(c))
	for _, v := range views {
		out[v.ID] = v.Name
	}
	return out
}

// Workspaces and members.

func (h *Handlers) listWorkspaces(c *gin.Context) {
	ctx, a := c.Request.Context(), actor(c)
	ws, err := h.q.Workspaces(ctx, a)
	if err != nil {
		fail(c, err)
		return
	}
	out := make([]protocol.Workspace, 0, len(ws))
	for _, w := range ws {
		_, members, err := h.q.Workspace(ctx, a, w.ID)
		if err != nil {
			fail(c, err)
			return
		}
		out = append(out, workspaceDTO(w, members))
	}
	reply(c, http.StatusOK, out, nil)
}

func (h *Handlers) createWorkspace(c *gin.Context) {
	var r protocol.CreateWorkspaceRequest
	if !bind(c, &r) {
		return
	}
	ctx, a := c.Request.Context(), actor(c)
	w, err := h.cmd.CreateWorkspace(ctx, a, r.Name, r.Leader)
	if err != nil {
		fail(c, err)
		return
	}
	w, members, err := h.q.Workspace(ctx, a, w.ID)
	reply(c, http.StatusCreated, workspaceDTO(w, members), err)
}

func (h *Handlers) getWorkspace(c *gin.Context) {
	w, members, err := h.q.Workspace(c.Request.Context(), actor(c), core.ID(c.Param("ws")))
	reply(c, http.StatusOK, workspaceDTO(w, members), err)
}

func (h *Handlers) listTasks(c *gin.Context) {
	ts, err := h.q.Tasks(c.Request.Context(), actor(c), core.ID(c.Param("ws")))
	reply(c, http.StatusOK, mapAll(ts, taskDTO), err)
}

func (h *Handlers) addMember(c *gin.Context) {
	var r protocol.AddMemberRequest
	if !bind(c, &r) {
		return
	}
	reply(c, 0, nil, h.cmd.AddMember(c.Request.Context(), actor(c), core.ID(c.Param("ws")), r.Agent, r.Leader))
}

func (h *Handlers) removeMember(c *gin.Context) {
	reply(c, 0, nil, h.cmd.RemoveMember(c.Request.Context(), actor(c), core.ID(c.Param("ws")), c.Param("agent")))
}

// Tasks and messages.

func (h *Handlers) createTask(c *gin.Context) {
	var r protocol.CreateTaskRequest
	if !bind(c, &r) {
		return
	}
	t, err := h.cmd.CreateTask(c.Request.Context(), actor(c), core.ID(c.Param("ws")), r.Brief)
	reply(c, http.StatusCreated, taskDTO(t), err)
}

func (h *Handlers) postMessage(c *gin.Context) {
	var r protocol.PostMessageRequest
	if !bind(c, &r) {
		return
	}
	in := core.PostInput{
		Task: core.ID(r.TaskID), Mentions: r.Mentions, Kind: core.MessageKind(r.Kind),
		Status: core.ReportStatus(r.Status), Checkpoint: r.Checkpoint, Text: r.Text,
	}
	m, err := h.cmd.PostMessage(c.Request.Context(), actor(c), core.ID(c.Param("ws")), in)
	reply(c, http.StatusCreated, messageDTO(m), err)
}

func (h *Handlers) readMessages(c *gin.Context) {
	var q protocol.ReadMessagesQuery
	if !bindQuery(c, &q) {
		return
	}
	ctx, a := c.Request.Context(), actor(c)
	task := core.ID(q.TaskID)
	if task == "" {
		// The general task of the workspace.
		ts, err := h.q.Tasks(ctx, a, core.ID(c.Param("ws")))
		if err != nil {
			fail(c, err)
			return
		}
		for _, t := range ts {
			if t.General {
				task = t.ID
			}
		}
		if task == "" {
			fail(c, errs.ErrNotFound)
			return
		}
	}
	ms, err := h.q.Messages(ctx, a, task, q.Since, q.Limit)
	reply(c, http.StatusOK, mapAll(ms, messageDTO), err)
}

func (h *Handlers) getTask(c *gin.Context) {
	t, err := h.q.Task(c.Request.Context(), actor(c), core.ID(c.Param("task")))
	reply(c, http.StatusOK, taskDTO(t), err)
}

func (h *Handlers) cancelTask(c *gin.Context) {
	reply(c, 0, nil, h.cmd.CancelTask(c.Request.Context(), actor(c), core.ID(c.Param("task"))))
}

func (h *Handlers) summarizeTask(c *gin.Context) {
	reply(c, 0, nil, h.cmd.RequestSummary(c.Request.Context(), actor(c), core.ID(c.Param("task"))))
}

func (h *Handlers) recall(c *gin.Context) {
	obj, err := h.q.Recall(c.Request.Context(), actor(c), core.ID(c.Param("id")))
	reply(c, http.StatusOK, obj, err)
}

// The Super Agent conversation.

func (h *Handlers) superMessages(c *gin.Context) {
	var q protocol.SuperMessagesQuery
	if !bindQuery(c, &q) {
		return
	}
	ms, err := h.q.SuperMessages(c.Request.Context(), actor(c), q.Since, q.Limit)
	reply(c, http.StatusOK, mapAll(ms, messageDTO), err)
}

func (h *Handlers) sendToSuper(c *gin.Context) {
	var r protocol.TextRequest
	if !bind(c, &r) {
		return
	}
	m, err := h.cmd.SendToSuper(c.Request.Context(), actor(c), r.Text)
	reply(c, http.StatusCreated, messageDTO(m), err)
}

func (h *Handlers) superReply(c *gin.Context) {
	var r protocol.TextRequest
	if !bind(c, &r) {
		return
	}
	m, err := h.cmd.SuperReply(c.Request.Context(), actor(c), r.Text)
	reply(c, http.StatusCreated, messageDTO(m), err)
}
