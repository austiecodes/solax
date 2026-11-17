// Package router builds the route table of the server: the REST API
// under /v1, the daemon endpoint and the client stream.
package router

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/auth"
	"github.com/austiecodes/solax/internal/server/controller"
	"github.com/austiecodes/solax/internal/server/middleware"
	"github.com/austiecodes/solax/internal/server/stream"
	"github.com/austiecodes/solax/pkg/protocol"
)

// New returns the gin engine with all routes. daemon is the hub, mounted
// at protocol.DaemonPath; st is the client stream, mounted at /v1/stream.
func New(tokens *auth.Tokens, daemon http.Handler, st *stream.Stream) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/healthz", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	// Daemons authenticate with a machine token inside the hub.
	r.GET(protocol.DaemonPath, gin.WrapH(daemon))

	// The only unauthenticated endpoint: a machine enrolls the hash of the
	// token it generated, and waits for approval. Rate-limited per address.
	r.POST("/v1/machines/enroll", middleware.RateLimit(), controller.EnrollMachine)

	v1 := r.Group("/v1", middleware.Authenticate(tokens))
	v1.GET("/stream", middleware.RequireKind(auth.KindClient), func(c *gin.Context) {
		st.Serve(c.Writer, c.Request, middleware.Actor(c).OwnerID)
	})

	// The Super Agent lists machines to know what is available. Only a
	// person changes them.
	machines := v1.Group("/machines", middleware.RequireClientOrSuper())
	machines.GET("", controller.ListMachines)
	machines.POST("", middleware.RequireKind(auth.KindClient), controller.RegisterMachine)
	machines.POST("/approve", middleware.RequireKind(auth.KindClient), controller.ApproveMachine)
	machines.DELETE("/:machine", middleware.RequireKind(auth.KindClient), controller.RevokeMachine)
	// Agents are set up by a person; the Super Agent reads them to route.
	v1.GET("/agents", controller.ListAgents)
	agents := v1.Group("/agents", middleware.RequireKind(auth.KindClient))
	agents.POST("", controller.CreateAgent)
	agents.PATCH("/:agent", controller.UpdateAgent)
	agents.DELETE("/:agent", controller.DeleteAgent)

	// Jobs: a person or the Super Agent dispatches work to an agent. The
	// call returns at once; the result arrives on the stream (and in the
	// Super Agent inbox).
	jobs := v1.Group("/jobs", middleware.RequireClientOrSuper())
	jobs.POST("", controller.DispatchJob)
	jobs.GET("", controller.ListJobs)
	jobs.GET("/:job", controller.GetJob)
	jobs.POST("/:job/messages", controller.FollowUpJob)
	jobs.POST("/:job/cancel", controller.CancelJob)
	v1.GET("/sessions/:session/events", middleware.RequireClientOrSuper(), controller.ListSessionEvents)

	// The conversation of the user with the Super Agent.
	super := v1.Group("/super")
	super.GET("/messages", middleware.RequireKind(auth.KindClient), controller.ListSuperMessages)
	super.POST("/messages", middleware.RequireKind(auth.KindClient), controller.SendToSuper)
	super.POST("/replies", middleware.RequireSuper(), controller.SuperReply)

	// Any object by ID, with the IDs of related objects.
	v1.GET("/objects/:id", controller.Recall)

	return r
}
