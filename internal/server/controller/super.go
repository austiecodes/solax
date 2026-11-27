package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/server/middleware"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/internal/server/service"
	"github.com/austiecodes/solax/pkg/protocol"
)

// ListSuperMessages returns the conversation with the Super Agent.
func ListSuperMessages(c *gin.Context) {
	var q protocol.SuperMessagesQuery
	if !bindQuery(c, &q) {
		return
	}
	ms, err := service.SuperMessages(c.Request.Context(), middleware.Actor(c), q.Since, q.Limit)
	reply(c, http.StatusOK, mapAll(ms, model.WireMessage), err)
}

// SendToSuper sends a message of the user to the Super Agent.
func SendToSuper(c *gin.Context) {
	var r protocol.TextRequest
	if !bind(c, &r) {
		return
	}
	m, err := service.SendToSuper(c.Request.Context(), middleware.Actor(c), r.Text)
	reply(c, http.StatusCreated, model.WireMessage(m), err)
}

// InterruptSuper stops the running turn of the Super Agent.
func InterruptSuper(c *gin.Context) {
	err := service.InterruptSuper(c.Request.Context(), middleware.Actor(c))
	reply(c, http.StatusNoContent, nil, err)
}

// SuperReply stores a reply of the Super Agent.
func SuperReply(c *gin.Context) {
	var r protocol.TextRequest
	if !bind(c, &r) {
		return
	}
	m, err := service.SuperReply(c.Request.Context(), middleware.Actor(c), r.Text)
	reply(c, http.StatusCreated, model.WireMessage(m), err)
}
