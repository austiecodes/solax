package main

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/locales"
	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/protocol"
)

// projectForm adds a project, or edits the project editing. The machine of
// a project does not change, nor the folder of a managed project: an edit
// shows them, but does not send them.
type projectForm struct {
	Name, Machine, Path, Desc string
	editing                   string // the ID of the project in edit; "" adds one
	managed                   bool   // the project in edit is a folder that Solax made
	open                      bool   // it shows in place of the chosen project
	busy                      bool
}

// projectsPage lists the projects, as the Agents page lists the agents:
// the list, and under it the chosen project, or the form.
func (d *desktop) projectsPage(c *ui.Context) {
	pal := paletteFor(c)
	d.showPage(c, pageSpec{icon: folderIcon, title: locales.NavProjects(), sub: locales.ProjectsSubtitle()}, func() {
		section(c, "projects", sectionSpec{
			title: locales.NavProjects(), count: countText(len(d.projects)), rows: listRows,
			desc: locales.ProjectsHelp(),
			actions: func() {
				iconButton(c, "refresh", reloadIcon, locales.Refresh(), pal, d.reloadSetup)
				iconButton(c, "new", plusIcon, locales.NewProject(), pal, func() { d.pform = projectForm{open: true} })
			},
		}, func() {
			if len(d.projects) == 0 {
				note(c, locales.ProjectsEmpty())
			}
			for _, p := range d.projects {
				d.projectItem(c, pal, p)
			}
		})
		if p, ok := d.chosenProject(); ok && !d.pform.open {
			d.projectDetail(c, p)
		} else {
			d.projectFormView(c)
		}
	})
}

// chosenProject is the project the page shows: the one the user chose,
// else the first.
func (d *desktop) chosenProject() (protocol.Project, bool) {
	for _, p := range d.projects {
		if p.ID == d.projectSel {
			return p, true
		}
	}
	if len(d.projects) > 0 {
		return d.projects[0], true
	}
	return protocol.Project{}, false
}

// projectItem is a project in the list, on one line: whether its machine
// is online, its name, its description and its machine.
func (d *desktop) projectItem(c *ui.Context, pal *palette, p protocol.Project) {
	cur, _ := d.chosenProject()
	m, _ := onOff(p.Online)
	it := listItem(c, p.ID, !d.pform.open && p.ID == cur.ID, p.Name, func() {
		dot(c, m)
		ui.Text(c, p.Name).FontSize(13.5).FontWeight(600).TextColor(pal.text).SingleLine().Shrink(0)
		ui.Text(c, p.Description).FontSize(13).TextColor(pal.textMuted).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		chip(c, p.Machine, markQuiet)
	})
	if it.Clicked() {
		d.projectSel, d.pform.open = p.ID, false
	}
}

// projectDetail shows the chosen project, with its actions.
func (d *desktop) projectDetail(c *ui.Context, p protocol.Project) {
	m, word := onOff(p.Online)
	desc := p.Description
	if desc == "" {
		desc = locales.ProjectNoDesc()
	}
	section(c, "detail-"+p.ID, sectionSpec{
		title: p.Name, count: p.Machine, desc: desc,
		lead: func() { avatar(c, folderIcon, m) },
		actions: func() {
			if button(c, "edit", locales.ProjectEdit(), plainButton, false) {
				d.pform = projectForm{Name: p.Name, Machine: p.Machine, Path: p.Path, Desc: p.Description,
					editing: p.ID, managed: p.Kind == "managed", open: true}
			}
			if button(c, "remove", locales.ProjectRemove(), removeButton, false) {
				id, name := p.ID, p.Name
				d.ask(locales.ProjectRemoveQuestion(name), locales.ProjectRemoveDetail(name), locales.ProjectRemove(), true, func() {
					d.do(func(ctx context.Context, c *client.Client) (func(), error) {
						return d.reloadSetup, c.DeleteProject(ctx, id)
					})
				})
			}
		},
	}, func() {
		row(c, "status", locales.FieldStatus(), locales.ProjectStatusHelp(), func() {
			dot(c, m)
			valueText(c, word, false)
		})
		row(c, "machine", locales.FieldMachine(), "", func() { valueText(c, p.Machine, false) })
		help := ""
		if p.Kind == "managed" {
			help = locales.ProjectManagedHelp()
		}
		row(c, "path", locales.FieldPath(), help, func() { valueText(c, p.Path, true) })
	})
}

// projectFormView is the form for a new project, or for the project in
// edit.
func (d *desktop) projectFormView(c *ui.Context) {
	f := &d.pform
	editing := f.editing != ""
	machines := d.machineNames()
	if !editing && f.Machine == "" && len(machines) > 0 {
		f.Machine = machines[0]
	}
	ready := !f.busy && strings.TrimSpace(f.Name) != "" && f.Machine != "" && strings.TrimSpace(f.Path) != ""
	// With no project, the form is the page: there is nothing to go back to.
	closable := editing || len(d.projects) > 0
	icon, title, help, submit := plusIcon, locales.NewProject(), locales.NewProjectHelp(), locales.ProjectAdd()
	if editing {
		icon, title, help, submit = pencilIcon, locales.EditProject(), "", locales.Save()
	}
	pal := paletteFor(c)
	section(c, "project-form", sectionSpec{
		title: title, desc: help,
		lead: func() { avatar(c, icon, markAccent) },
		actions: func() {
			if closable {
				iconButton(c, "close", closeIcon, locales.Close(), pal, func() { d.pform = projectForm{} })
			}
		},
		footer: func() {
			if closable && button(c, "cancel", locales.Cancel(), plainButton, false) {
				d.pform = projectForm{}
			}
			if button(c, "save-project", submit, primaryButton, !ready) {
				d.saveProject()
			}
		},
	}, func() {
		row(c, "name", locales.FieldName(), locales.FieldProjectNameHelp(), func() {
			ui.Row(c).Width(fieldW).Children(func() { textField(c, "name", &f.Name, "my-app") })
		})
		row(c, "machine", locales.FieldMachine(), locales.FieldProjectMachineHelp(), func() {
			switch {
			case editing:
				valueText(c, f.Machine, false)
			case len(machines) == 0:
				valueText(c, locales.NoMachinesYet(), false)
			default:
				ui.Row(c).Width(fieldW).Children(func() { selectField(c, "machine", &f.Machine, machines) })
			}
		})
		if f.managed {
			row(c, "path", locales.FieldPath(), locales.ProjectManagedHelp(), func() { valueText(c, f.Path, true) })
		} else {
			row(c, "path", locales.FieldPath(), locales.FieldPathHelp(), func() {
				ui.Row(c).Width(fieldW).Children(func() { textField(c, "path", &f.Path, "~/src/my-app") })
			})
		}
		stackRow(c, "desc", locales.FieldDesc(), locales.FieldProjectDescHelp(), func() {
			textArea(c, "desc", &f.Desc, "", 72)
		})
	})
}

// saveProject sends the form to the server: a new project, or the changed
// fields of the project in edit. On success the page shows the project.
func (d *desktop) saveProject() {
	f := &d.pform
	name, path, desc := strings.TrimSpace(f.Name), strings.TrimSpace(f.Path), strings.TrimSpace(f.Desc)
	editing, machine := f.editing, f.Machine
	var old protocol.Project
	for _, p := range d.projects {
		if p.ID == editing {
			old = p
		}
	}
	f.busy = true
	d.do(func(ctx context.Context, c *client.Client) (func(), error) {
		var (
			p   protocol.Project
			err error
		)
		if editing == "" {
			p, err = c.CreateProject(ctx, protocol.CreateProjectRequest{Name: name, Description: desc, Machine: machine, Path: path})
		} else {
			p, err = c.UpdateProject(ctx, editing, projectPatch(old, name, path, desc))
		}
		if err != nil {
			d.update(func() { d.pform.busy = false })
			return nil, err
		}
		return func() {
			d.pform = projectForm{}
			d.projectSel = p.ID
			d.reloadSetup()
		}, nil
	})
	if _, err := d.api(); err != nil {
		f.busy = false
	}
}

// projectPatch is the change from project old to the form values: only
// the fields that changed, so an edit does not check the other fields
// again.
func projectPatch(old protocol.Project, name, path, desc string) protocol.UpdateProjectRequest {
	var r protocol.UpdateProjectRequest
	if name != old.Name {
		r.Name = &name
	}
	if path != old.Path {
		r.Path = &path
	}
	if desc != old.Description {
		r.Description = &desc
	}
	return r
}
