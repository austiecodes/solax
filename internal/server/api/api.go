// Package api holds the gin handlers of the REST API under /v1. Handlers
// translate HTTP into Engine calls. They contain no domain rules.
package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/auth"
	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Deps are the dependencies of the handlers.
type Deps struct {
	Commands   core.Commands
	Enrollment core.Enrollment
	Queries    core.Queries
	Auth       auth.Verifier
	Daemon     http.Handler // the hub, mounted at protocol.DaemonPath
	Stream     http.Handler // the client stream, mounted at /v1/stream
}

// Handlers implements the REST endpoints.
type Handlers struct {
	cmd    core.Commands
	enroll core.Enrollment
	q      core.Queries
}

// NewRouter returns the gin engine with all routes.
func NewRouter(d Deps) *gin.Engine {
	h := &Handlers{cmd: d.Commands, enroll: d.Enrollment, q: d.Queries}

	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/healthz", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	// Daemons authenticate with a machine token inside the hub.
	r.GET(protocol.DaemonPath, gin.WrapH(d.Daemon))

	// The only unauthenticated endpoint: a machine enrolls the hash of the
	// token it generated, and waits for approval. Rate-limited per address.
	r.POST("/v1/machines/enroll", RateLimit(), h.enrollMachine)

	v1 := r.Group("/v1", Authenticate(d.Auth))
	v1.GET("/stream", RequireKind(auth.KindClient), gin.WrapH(d.Stream))

	machines := v1.Group("/machines", RequireKind(auth.KindClient))
	machines.GET("", h.listMachines)
	machines.POST("", h.registerMachine)
	machines.POST("/approve", h.approveMachine)
	machines.DELETE("/:machine", h.revokeMachine)
	v1.GET("/agents", h.listAgents)

	ws := v1.Group("/workspaces")
	ws.GET("", h.listWorkspaces)
	ws.POST("", RequireKind(auth.KindClient, auth.KindAgent), h.createWorkspace)
	ws.GET("/:ws", h.getWorkspace)
	ws.POST("/:ws/members", h.addMember)
	ws.DELETE("/:ws/members/:agent", h.removeMember)
	ws.POST("/:ws/tasks", h.createTask)
	ws.POST("/:ws/messages", h.postMessage)
	ws.GET("/:ws/messages", h.readMessages)

	tasks := v1.Group("/tasks")
	tasks.GET("/:task", h.getTask)
	tasks.POST("/:task/cancel", h.cancelTask)
	tasks.POST("/:task/summarize", h.summarizeTask)

	// The conversation of the user with the Super Agent.
	super := v1.Group("/super")
	super.GET("/messages", RequireKind(auth.KindClient), h.superMessages)
	super.POST("/messages", RequireKind(auth.KindClient), h.sendToSuper)
	super.POST("/replies", RequireSuper(), h.superReply)

	// Chain of Recall: any object by ID.
	v1.GET("/objects/:id", h.recall)

	return r
}

// fail writes the HTTP status of err.
func fail(c *gin.Context, err error) {
	c.AbortWithStatusJSON(status(err), protocol.ErrorResponse{Error: err.Error()})
}

func status(err error) int {
	switch {
	case errors.Is(err, errs.ErrNotImplemented):
		return http.StatusNotImplemented
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
