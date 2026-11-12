package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/errs"
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
	if err != nil {
		fail(c, err)
		return
	}
	agents, err := h.q.Agents(c.Request.Context(), a)
	if err != nil {
		fail(c, err)
		return
	}
	reply(c, http.StatusOK, mapAll(ms, func(m core.Machine) protocol.Machine { return machineDTO(m, agents) }), nil)
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
	reply(c, http.StatusOK, machineDTO(m, nil), err)
}

func (h *Handlers) revokeMachine(c *gin.Context) {
	reply(c, 0, nil, h.cmd.RevokeMachine(c.Request.Context(), actor(c), core.ID(c.Param("machine"))))
}

func (h *Handlers) listAgents(c *gin.Context) {
	as, err := h.q.Agents(c.Request.Context(), actor(c))
	reply(c, http.StatusOK, mapAll(as, agentDTO), err)
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
