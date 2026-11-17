// Package controller holds the gin handlers of the REST API under /v1,
// one file for each domain. A handler binds its request, calls the
// service with the actor of the request, and maps the result to its wire
// form. Handlers contain no domain rules.
package controller

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/server/middleware"
)

// bind decodes a JSON body; a bad body is ErrInvalid (400).
func bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		middleware.Abort(c, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return false
	}
	return true
}

func bindQuery(c *gin.Context, v any) bool {
	if err := c.ShouldBindQuery(v); err != nil {
		middleware.Abort(c, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return false
	}
	return true
}

// reply writes v, or the status of err.
func reply(c *gin.Context, code int, v any, err error) {
	if err != nil {
		middleware.Abort(c, err)
		return
	}
	if v == nil {
		c.Status(http.StatusNoContent)
		return
	}
	c.JSON(code, v)
}
