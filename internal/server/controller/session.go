package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/server/middleware"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/internal/server/service"
	"github.com/austiecodes/solax/pkg/ir"
)

// ListSessionEvents returns the stored events of a session.
func ListSessionEvents(c *gin.Context) {
	var q struct {
		After int64 `form:"after"`
		Limit int   `form:"limit"`
	}
	if !bindQuery(c, &q) {
		return
	}
	evs, err := service.SessionEvents(c.Request.Context(), middleware.Actor(c), model.ID(c.Param("session")), q.After, q.Limit)
	if evs == nil {
		evs = []ir.Event{}
	}
	reply(c, http.StatusOK, evs, err)
}
