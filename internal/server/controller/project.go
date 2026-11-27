package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/server/middleware"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/internal/server/service"
	"github.com/austiecodes/solax/pkg/protocol"
)

// ListProjects lists the live projects of the owner.
func ListProjects(c *gin.Context) {
	ps, err := service.Projects(c.Request.Context(), middleware.Actor(c))
	reply(c, http.StatusOK, mapAll(ps, wireProject), err)
}

// CreateProject sets up a project.
func CreateProject(c *gin.Context) {
	var r protocol.CreateProjectRequest
	if !bind(c, &r) {
		return
	}
	p, err := service.CreateProject(c.Request.Context(), middleware.Actor(c), service.ProjectSpec{
		Name: r.Name, Description: r.Description, Kind: model.SourceKind(r.Kind), Machine: r.Machine, Path: r.Path,
	})
	replyProject(c, http.StatusCreated, p, err)
}

// UpdateProject changes a project.
func UpdateProject(c *gin.Context) {
	var r protocol.UpdateProjectRequest
	if !bind(c, &r) {
		return
	}
	p, err := service.UpdateProject(c.Request.Context(), middleware.Actor(c), c.Param("project"), service.ProjectPatch{
		Name: r.Name, Description: r.Description, Path: r.Path,
	})
	replyProject(c, http.StatusOK, p, err)
}

// DeleteProject removes a project.
func DeleteProject(c *gin.Context) {
	reply(c, http.StatusNoContent, nil, service.DeleteProject(c.Request.Context(), middleware.Actor(c), c.Param("project")))
}

// replyProject answers with the project as the list shows it.
func replyProject(c *gin.Context, code int, p model.Project, err error) {
	if err != nil {
		middleware.Abort(c, err)
		return
	}
	views, err := service.Projects(c.Request.Context(), middleware.Actor(c))
	for _, v := range views {
		if v.ID == p.ID {
			reply(c, code, wireProject(v), err)
			return
		}
	}
	reply(c, code, wireProject(service.ProjectView{Project: p}), err)
}
