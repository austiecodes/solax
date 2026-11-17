package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/server/middleware"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/internal/server/service"
	"github.com/austiecodes/solax/pkg/protocol"
)

// ListMachines lists the machines of the owner.
func ListMachines(c *gin.Context) {
	a := middleware.Actor(c)
	ms, err := service.Machines(c.Request.Context(), a)
	reply(c, http.StatusOK, mapAll(ms, wireMachine), err)
}

// EnrollMachine enrolls a new machine that waits for approval. It needs no token.
func EnrollMachine(c *gin.Context) {
	var r protocol.MachineEnrollment
	if !bind(c, &r) {
		return
	}
	m, err := service.EnrollMachine(c.Request.Context(), r.Name, r.TokenHash)
	reply(c, http.StatusOK, wireEnrolled(m), err)
}

// RegisterMachine registers a machine of the owner, approved at once.
func RegisterMachine(c *gin.Context) {
	var r protocol.MachineEnrollment
	if !bind(c, &r) {
		return
	}
	m, err := service.RegisterMachine(c.Request.Context(), middleware.Actor(c), r.Name, r.TokenHash)
	reply(c, http.StatusOK, wireEnrolled(m), err)
}

// ApproveMachine approves a pending machine by its pairing code.
func ApproveMachine(c *gin.Context) {
	var r protocol.ApproveMachineRequest
	if !bind(c, &r) {
		return
	}
	m, err := service.ApproveMachine(c.Request.Context(), middleware.Actor(c), r.Code)
	reply(c, http.StatusOK, wireMachine(m), err)
}

// RevokeMachine revokes a machine.
func RevokeMachine(c *gin.Context) {
	reply(c, 0, nil, service.RevokeMachine(c.Request.Context(), middleware.Actor(c), model.ID(c.Param("machine"))))
}
