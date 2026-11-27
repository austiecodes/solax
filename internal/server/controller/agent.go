package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/server/middleware"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/internal/server/service"
	"github.com/austiecodes/solax/pkg/protocol"
)

// ListAgents lists the live agents of the owner.
func ListAgents(c *gin.Context) {
	as, err := service.Agents(c.Request.Context(), middleware.Actor(c))
	reply(c, http.StatusOK, mapAll(as, wireAgent), err)
}

// CreateAgent creates an agent on a machine.
func CreateAgent(c *gin.Context) {
	var r protocol.CreateAgentRequest
	if !bind(c, &r) {
		return
	}
	ag, err := service.CreateAgent(c.Request.Context(), middleware.Actor(c), service.AgentSpec{
		Machine: r.Machine, Name: r.Name, Description: r.Description, Instructions: r.Instructions, Runtime: r.Runtime,
		Model: r.Model, Options: r.Options, Super: r.Super,
	})
	replyAgent(c, http.StatusCreated, ag, err)
}

// UpdateAgent changes an agent.
func UpdateAgent(c *gin.Context) {
	var r protocol.UpdateAgentRequest
	if !bind(c, &r) {
		return
	}
	ag, err := service.UpdateAgent(c.Request.Context(), middleware.Actor(c), c.Param("agent"), service.AgentPatch{
		Description: r.Description, Instructions: r.Instructions, Model: r.Model, Options: r.Options, Super: r.Super,
	})
	replyAgent(c, http.StatusOK, ag, err)
}

// replyAgent answers with the agent as the list shows it, with its machine.
func replyAgent(c *gin.Context, code int, ag model.Agent, err error) {
	if err != nil {
		middleware.Abort(c, err)
		return
	}
	views, err := service.Agents(c.Request.Context(), middleware.Actor(c))
	for _, v := range views {
		if v.ID == ag.ID {
			reply(c, code, wireAgent(v), err)
			return
		}
	}
	reply(c, code, wireAgent(service.AgentView{Agent: ag}), err)
}

// DeleteAgent deletes an agent.
func DeleteAgent(c *gin.Context) {
	reply(c, http.StatusNoContent, nil, service.DeleteAgent(c.Request.Context(), middleware.Actor(c), c.Param("agent")))
}
