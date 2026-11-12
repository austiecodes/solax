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
	Stream     Streamer     // the client stream, mounted at /v1/stream
}

// Streamer serves the client stream of one owner.
type Streamer interface {
	Serve(w http.ResponseWriter, r *http.Request, owner core.ID)
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
	v1.GET("/stream", RequireKind(auth.KindClient), func(c *gin.Context) {
		d.Stream.Serve(c.Writer, c.Request, actor(c).OwnerID)
	})

	// The Super Agent lists machines to know what is available. Only a
	// person changes them.
	machines := v1.Group("/machines", RequireClientOrSuper())
	machines.GET("", h.listMachines)
	machines.POST("", RequireKind(auth.KindClient), h.registerMachine)
	machines.POST("/approve", RequireKind(auth.KindClient), h.approveMachine)
	machines.DELETE("/:machine", RequireKind(auth.KindClient), h.revokeMachine)
	// Agents are set up by a person; the Super Agent reads them to route.
	v1.GET("/agents", h.listAgents)
	agents := v1.Group("/agents", RequireKind(auth.KindClient))
	agents.POST("", h.createAgent)
	agents.PATCH("/:agent", h.updateAgent)
	agents.DELETE("/:agent", h.deleteAgent)

	// Jobs: a person or the Super Agent dispatches work to an agent. The
	// call returns at once; the result arrives on the stream (and in the
	// Super Agent inbox).
	jobs := v1.Group("/jobs", RequireClientOrSuper())
	jobs.POST("", h.dispatch)
	jobs.GET("", h.listJobs)
	jobs.GET("/:job", h.getJob)
	jobs.POST("/:job/messages", h.followUpJob)
	jobs.POST("/:job/cancel", h.cancelJob)
	v1.GET("/sessions/:session/events", RequireClientOrSuper(), h.sessionEvents)

	// Reading is open to every non-machine token; a worker agent gets only
	// its own workspace and task (the Engine enforces the scope).
	ws := v1.Group("/workspaces")
	ws.GET("", h.listWorkspaces)
	ws.POST("", RequireClientOrSuper(), h.createWorkspace)
	ws.GET("/:ws", h.getWorkspace)
	ws.GET("/:ws/tasks", h.listTasks)
	ws.POST("/:ws/members", RequireClientOrSuper(), h.addMember)
	ws.DELETE("/:ws/members/:agent", RequireClientOrSuper(), h.removeMember)
	ws.POST("/:ws/tasks", RequireClientOrSuper(), h.createTask)
	ws.POST("/:ws/messages", h.postMessage)
	ws.GET("/:ws/messages", h.readMessages)

	tasks := v1.Group("/tasks")
	tasks.GET("/:task", h.getTask)
	tasks.POST("/:task/cancel", RequireClientOrSuper(), h.cancelTask)
	tasks.POST("/:task/summarize", RequireClientOrSuper(), h.summarizeTask)

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
// A 500 hides the error text, which may hold storage details; the error is
// attached to the gin context for the access log.
func fail(c *gin.Context, err error) {
	code := status(err)
	msg := err.Error()
	if code == http.StatusInternalServerError {
		_ = c.Error(err)
		msg = "internal error"
	}
	c.AbortWithStatusJSON(code, protocol.ErrorResponse{Error: msg})
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
