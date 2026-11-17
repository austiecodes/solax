package middleware

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Abort ends the request with the HTTP status of err. A 500 hides the
// error text, which may hold storage details; the error is attached to the
// gin context for the access log. Middleware and controllers use it.
func Abort(c *gin.Context, err error) {
	code := status(err)
	msg := err.Error()
	if code == http.StatusInternalServerError {
		_ = c.Error(err)
		msg = "internal error"
	}
	c.AbortWithStatusJSON(code, protocol.ErrorResponse{Error: msg})
}

// status maps the error kinds of package errs to HTTP statuses.
func status(err error) int {
	switch {
	case errors.Is(err, errs.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, errs.ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, errs.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, errs.ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, errs.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, errs.ErrUnavailable):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
