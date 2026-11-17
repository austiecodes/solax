package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/server/middleware"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/internal/server/service"
)

// Recall returns any object by ID with the IDs of related objects.
func Recall(c *gin.Context) {
	obj, err := service.Recall(c.Request.Context(), middleware.Actor(c), model.ID(c.Param("id")))
	reply(c, http.StatusOK, obj, err)
}
