package api

import (
	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/errs"
)

// Each handler binds its request, calls the Engine with actor(c), and maps
// the result to a protocol DTO.

func (h *Handlers) listMachines(c *gin.Context) { fail(c, errs.ErrNotImplemented) }
func (h *Handlers) enrollMachine(c *gin.Context) {
	fail(c, errs.ErrNotImplemented) // protocol.MachineEnrollment -> protocol.MachineEnrolled (pending)
}
func (h *Handlers) registerMachine(c *gin.Context) {
	fail(c, errs.ErrNotImplemented) // protocol.MachineEnrollment -> protocol.MachineEnrolled (approved)
}
func (h *Handlers) approveMachine(c *gin.Context) {
	fail(c, errs.ErrNotImplemented) // protocol.ApproveMachineRequest
}
func (h *Handlers) revokeMachine(c *gin.Context)  { fail(c, errs.ErrNotImplemented) }
func (h *Handlers) listAgents(c *gin.Context)     { fail(c, errs.ErrNotImplemented) }
func (h *Handlers) listWorkspaces(c *gin.Context) { fail(c, errs.ErrNotImplemented) }
func (h *Handlers) createWorkspace(c *gin.Context) {
	fail(c, errs.ErrNotImplemented) // protocol.CreateWorkspaceRequest
}
func (h *Handlers) getWorkspace(c *gin.Context) { fail(c, errs.ErrNotImplemented) }
func (h *Handlers) addMember(c *gin.Context) {
	fail(c, errs.ErrNotImplemented) // protocol.AddMemberRequest
}
func (h *Handlers) removeMember(c *gin.Context) { fail(c, errs.ErrNotImplemented) }
func (h *Handlers) createTask(c *gin.Context) {
	fail(c, errs.ErrNotImplemented) // protocol.CreateTaskRequest
}
func (h *Handlers) postMessage(c *gin.Context) {
	fail(c, errs.ErrNotImplemented) // protocol.PostMessageRequest; author from token
}
func (h *Handlers) readMessages(c *gin.Context) {
	fail(c, errs.ErrNotImplemented) // protocol.ReadMessagesQuery
}
func (h *Handlers) getTask(c *gin.Context)       { fail(c, errs.ErrNotImplemented) }
func (h *Handlers) cancelTask(c *gin.Context)    { fail(c, errs.ErrNotImplemented) }
func (h *Handlers) summarizeTask(c *gin.Context) { fail(c, errs.ErrNotImplemented) }
func (h *Handlers) recall(c *gin.Context)        { fail(c, errs.ErrNotImplemented) }
func (h *Handlers) superMessages(c *gin.Context) { fail(c, errs.ErrNotImplemented) }
func (h *Handlers) sendToSuper(c *gin.Context)   { fail(c, errs.ErrNotImplemented) }
func (h *Handlers) superReply(c *gin.Context)    { fail(c, errs.ErrNotImplemented) }
