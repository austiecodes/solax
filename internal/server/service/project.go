package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Projects. A project is where agents work; agents and projects are set
// up apart, and several agents can work in one project. Each task works in
// a project: the one it names, or a managed project that Solax launches
// for it on the machine of its agent.

// projectSource is where the files of a project come from. Each kind of
// source implements it: a folder on a machine (local or managed) now, a
// Git repository later.
type projectSource interface {
	// check reports why an agent on machine m cannot work in the project.
	check(m model.Machine) error
	// machine is the only machine that has the files, or "" when the
	// daemon of any machine can get them.
	machine() model.ID
	// workspace tells the daemon how to prepare the work folder.
	workspace() protocol.Workspace
}

// localFolder is a folder on one machine.
type localFolder struct {
	machineID model.ID
	path      string
}

func (l localFolder) check(m model.Machine) error { return onMachine(l.machineID, m) }

func (l localFolder) machine() model.ID { return l.machineID }

func (l localFolder) workspace() protocol.Workspace {
	return protocol.Workspace{Kind: protocol.WorkspaceLocal, Path: l.path}
}

// managedFolder is a folder that the daemon of one machine makes in its
// projects folder.
type managedFolder struct {
	machineID model.ID
	name      string // the folder in the projects folder
}

func (f managedFolder) check(m model.Machine) error { return onMachine(f.machineID, m) }

func (f managedFolder) machine() model.ID { return f.machineID }

func (f managedFolder) workspace() protocol.Workspace {
	return protocol.Workspace{Kind: protocol.WorkspaceManaged, Name: f.name}
}

// onMachine fails when the files of a project on machine id are not on m.
func onMachine(id model.ID, m model.Machine) error {
	if m.ID != id {
		return fmt.Errorf("%w: the project is a folder on another machine; give the task to an agent on that machine", errs.ErrInvalid)
	}
	return nil
}

// sourceOf returns the behavior of a stored source.
func sourceOf(s model.ProjectSource) (projectSource, error) {
	switch s.Kind {
	case model.SourceLocal:
		return localFolder{machineID: s.MachineID, path: s.Path}, nil
	case model.SourceManaged:
		return managedFolder{machineID: s.MachineID, name: s.Path}, nil
	}
	return nil, fmt.Errorf("%w: project source %q", errs.ErrInvalid, s.Kind)
}

// ProjectSpec is what a person sets up for a project.
type ProjectSpec struct {
	Name        string
	Description string
	Kind        model.SourceKind // "" is local
	Machine     string           // local: machine ID or name
	Path        string           // local: the folder on that machine
}

// ProjectPatch changes some fields of a project. Nil fields stay as they
// are.
type ProjectPatch struct {
	Name        *string
	Description *string
	Path        *string
}

// ProjectView is a project with the facts of its machine.
type ProjectView struct {
	model.Project
	MachineName string
	// Online is true when an agent can work in the project now: for a
	// local folder, its machine is connected.
	Online bool
}

// projectName is a name that a prompt and a shell can carry without
// quotes.
var projectName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// CreateProject sets up a project.
func CreateProject(ctx context.Context, a model.Actor, spec ProjectSpec) (model.Project, error) {
	if err := requireHuman(a); err != nil {
		return model.Project{}, err
	}
	name := strings.TrimSpace(spec.Name)
	if !projectName.MatchString(name) {
		return model.Project{}, fmt.Errorf("%w: project name must match %s", errs.ErrInvalid, projectName)
	}
	var p model.Project
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		src, err := newSource(ctx, tx, a, spec)
		if err != nil {
			return err
		}
		if err := freeProjectName(ctx, tx, a.OwnerID, name, ""); err != nil {
			return err
		}
		now := clock()
		p = model.Project{ID: model.NewIDAt(model.PrefixProject, now), OwnerID: a.OwnerID, Name: name,
			Description: strings.TrimSpace(spec.Description), Source: src, CreatedAt: now}
		return model.SaveProject(ctx, tx, p)
	})
	if err == nil {
		Publish(ctx, a.OwnerID, PushProjects, nil)
	}
	return p, err
}

// newSource checks the source of a new project.
func newSource(ctx context.Context, db model.Querier, a model.Actor, spec ProjectSpec) (model.ProjectSource, error) {
	switch spec.Kind {
	case "", model.SourceLocal:
		m, err := resolveMachine(ctx, db, a, spec.Machine)
		if err != nil {
			return model.ProjectSource{}, err
		}
		path, err := folderPath(spec.Path)
		if err != nil {
			return model.ProjectSource{}, err
		}
		return model.ProjectSource{Kind: model.SourceLocal, MachineID: m.ID, Path: path}, nil
	}
	return model.ProjectSource{}, fmt.Errorf("%w: project kind %q: want %q", errs.ErrInvalid, spec.Kind, model.SourceLocal)
}

// folderPath checks the folder of a local project: absolute, or from the
// home folder ("~/src"). The daemon expands "~" on its machine.
func folderPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p != "/" {
		p = strings.TrimRight(p, "/")
	}
	if p == "~" || strings.HasPrefix(p, "~/") || filepath.IsAbs(p) {
		return p, nil
	}
	return "", fmt.Errorf("%w: project folder %q: want an absolute path or one that starts with ~/", errs.ErrInvalid, p)
}

// freeProjectName fails when another live project of the owner (not self)
// has the name.
func freeProjectName(ctx context.Context, db model.Querier, owner model.ID, name string, self model.ID) error {
	p, err := model.GetProjectByName(ctx, db, owner, name)
	switch {
	case errors.Is(err, errs.ErrNotFound):
		return nil
	case err != nil:
		return err
	case p.ID != self:
		return fmt.Errorf("%w: project %q exists", errs.ErrConflict, name)
	}
	return nil
}

// UpdateProject changes a project. Tasks that run keep their folder until
// their session restarts.
func UpdateProject(ctx context.Context, a model.Actor, ref string, patch ProjectPatch) (model.Project, error) {
	if err := requireHuman(a); err != nil {
		return model.Project{}, err
	}
	var p model.Project
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		var err error
		if p, err = resolveProjectRef(ctx, tx, a, ref); err != nil {
			return err
		}
		if patch.Name != nil {
			name := strings.TrimSpace(*patch.Name)
			if !projectName.MatchString(name) {
				return fmt.Errorf("%w: project name must match %s", errs.ErrInvalid, projectName)
			}
			if err := freeProjectName(ctx, tx, a.OwnerID, name, p.ID); err != nil {
				return err
			}
			p.Name = name
		}
		if patch.Description != nil {
			p.Description = strings.TrimSpace(*patch.Description)
		}
		if patch.Path != nil {
			if p.Source.Kind != model.SourceLocal {
				return fmt.Errorf("%w: the folder of a %s project does not change", errs.ErrInvalid, p.Source.Kind)
			}
			if p.Source.Path, err = folderPath(*patch.Path); err != nil {
				return err
			}
		}
		return model.SaveProject(ctx, tx, p)
	})
	if err == nil {
		Publish(ctx, a.OwnerID, PushProjects, nil)
	}
	return p, err
}

// DeleteProject removes a project. Its tasks stay, and still name it; its
// files stay on the machine.
func DeleteProject(ctx context.Context, a model.Actor, ref string) error {
	if err := requireHuman(a); err != nil {
		return err
	}
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		p, err := resolveProjectRef(ctx, tx, a, ref)
		if err != nil {
			return err
		}
		p.Deleted = true
		return model.SaveProject(ctx, tx, p)
	})
	if err == nil {
		Publish(ctx, a.OwnerID, PushProjects, nil)
	}
	return err
}

// Projects returns the live projects of the owner, with their machines.
// The Super Agent reads them to choose where a task works.
func Projects(ctx context.Context, a model.Actor) ([]ProjectView, error) {
	if err := requireManager(a); err != nil {
		return nil, err
	}
	var out []ProjectView
	err := model.Snapshot(ctx, func(tx *sql.Tx) error {
		machines, err := model.ListMachines(ctx, tx, a.OwnerID)
		if err != nil {
			return err
		}
		byID := map[model.ID]model.Machine{}
		for _, m := range machines {
			byID[m.ID] = m
		}
		all, err := model.ListProjects(ctx, tx, a.OwnerID, false)
		if err != nil {
			return err
		}
		for _, p := range all {
			v := ProjectView{Project: p}
			src, err := sourceOf(p.Source)
			if err != nil {
				return err
			}
			if id := src.machine(); id != "" {
				m := byID[id]
				v.MachineName, v.Online = m.Name, m.Online && m.State == model.MachineApproved
			} else {
				v.Online = true
			}
			out = append(out, v)
		}
		return nil
	})
	return out, err
}

// ProjectNames maps the project IDs of the owner to names, deleted
// projects included: old tasks still name them.
func ProjectNames(ctx context.Context, a model.Actor) (map[model.ID]string, error) {
	all, err := model.ListProjects(ctx, model.ReadDB, a.OwnerID, true)
	out := make(map[model.ID]string, len(all))
	for _, p := range all {
		out[p.ID] = p.Name
	}
	return out, err
}

// resolveProjectRef finds a live project of the actor by ID or name.
func resolveProjectRef(ctx context.Context, db model.Querier, a model.Actor, ref string) (model.Project, error) {
	ref = strings.TrimSpace(ref)
	var (
		p   model.Project
		err error
	)
	if model.ID(ref).Prefix() == model.PrefixProject {
		p, err = model.GetProject(ctx, db, model.ID(ref))
		if err == nil && (p.Deleted || p.OwnerID != a.OwnerID) {
			err = errs.ErrNotFound
		}
	} else {
		p, err = model.GetProjectByName(ctx, db, a.OwnerID, ref)
	}
	if err != nil {
		return model.Project{}, fmt.Errorf("project %q: %w", ref, err)
	}
	return p, nil
}

// taskWorkspace is where the session of a task works: the folder of its
// project.
func taskWorkspace(ctx context.Context, db model.Querier, j model.Task) (protocol.Workspace, error) {
	if j.ProjectID == "" {
		return protocol.Workspace{}, fmt.Errorf("%w: task %s has no project", errs.ErrInvalid, j.ID)
	}
	p, err := model.GetProject(ctx, db, j.ProjectID)
	if err != nil {
		return protocol.Workspace{}, fmt.Errorf("project of task %s: %w", j.ID, err)
	}
	src, err := sourceOf(p.Source)
	if err != nil {
		return protocol.Workspace{}, err
	}
	return src.workspace(), nil
}

// launchProject makes a managed project for a task that names no project,
// on the machine of agent ag. Its name comes from the task title, and is
// new: no project of the owner, live or removed, has it, so the folder is
// new too. The caller publishes PushProjects after the commit.
func launchProject(ctx context.Context, tx *sql.Tx, a model.Actor, ag model.Agent, title string) (model.Project, error) {
	all, err := model.ListProjects(ctx, tx, a.OwnerID, true)
	if err != nil {
		return model.Project{}, err
	}
	taken := make(map[string]bool, 2*len(all))
	for _, p := range all {
		taken[p.Name] = true
		if p.Source.Kind == model.SourceManaged {
			taken[p.Source.Path] = true
		}
	}
	base := slug(title, 40)
	if base == "" {
		base = "project"
	}
	name := base
	for n := 2; taken[name]; n++ {
		name = fmt.Sprintf("%s-%d", base, n)
	}
	now := clock()
	p := model.Project{ID: model.NewIDAt(model.PrefixProject, now), OwnerID: a.OwnerID, Name: name, Description: title,
		Source: model.ProjectSource{Kind: model.SourceManaged, MachineID: ag.MachineID, Path: name}, CreatedAt: now}
	return p, model.SaveProject(ctx, tx, p)
}

// slug makes a name from text: its ASCII letters and digits in lower case,
// with a dash for each run of other characters, cut at a dash to at most
// limit bytes. Text with no ASCII letter or digit gives "".
func slug(text string, limit int) string {
	var b strings.Builder
	gap := false
	for _, r := range strings.ToLower(text) {
		if ('a' <= r && r <= 'z') || ('0' <= r && r <= '9') {
			if gap && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			gap = false
		} else {
			gap = true
		}
	}
	s := b.String()
	if len(s) > limit {
		s = s[:limit]
		if i := strings.LastIndexByte(s, '-'); i > 0 {
			s = s[:i]
		}
	}
	return s
}
