package model

import (
	"context"
	"time"
)

// Project is where agents work. Its source says where the files come
// from; several agents can work in one project. A task works in its
// project.
type Project struct {
	ID          ID
	OwnerID     ID
	Name        string
	Description string
	Source      ProjectSource
	Deleted     bool
	CreatedAt   time.Time
}

// ProjectSource is where the files of a project come from. Kind selects
// the fields that apply.
type ProjectSource struct {
	Kind SourceKind
	// MachineID is the machine that has the folder.
	MachineID ID
	// Path is the folder on that machine (local), which may start with
	// "~"; or the name of the folder in the projects folder of its daemon
	// (managed).
	Path string
}

// SourceKind is the kind of a project source.
type SourceKind string

const (
	// SourceLocal is a folder on one machine that a person set up. Solax
	// does not make or remove it.
	SourceLocal SourceKind = "local"
	// SourceManaged is a folder that Solax makes on one machine, in the
	// projects folder of its daemon, for a task that names no project.
	SourceManaged SourceKind = "managed"
)

const projectCols = `id, owner_id, name, description, kind, machine_id, path, deleted, created_at`

// GetProject returns the project with this ID, deleted or not.
func GetProject(ctx context.Context, db Querier, id ID) (Project, error) {
	return scanProject(db.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE id = ?`, id))
}

// GetProjectByName returns the live project of an owner with this name.
func GetProjectByName(ctx context.Context, db Querier, ownerID ID, name string) (Project, error) {
	return scanProject(db.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE owner_id = ? AND name = ? AND deleted = 0`, ownerID, name))
}

// SaveProject inserts or updates a project by ID. It fails with
// errs.ErrConflict when another live project of the owner has the name.
func SaveProject(ctx context.Context, db Executor, p Project) error {
	_, err := exec(ctx, db, `INSERT INTO projects(`+projectCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, description = excluded.description, kind = excluded.kind,
			machine_id = excluded.machine_id, path = excluded.path, deleted = excluded.deleted`,
		p.ID, p.OwnerID, p.Name, p.Description, p.Source.Kind, p.Source.MachineID, p.Source.Path, b2i(p.Deleted), ms(p.CreatedAt))
	return err
}

// ListProjects returns the projects of an owner, by name. all adds the
// deleted ones, which old tasks still name.
func ListProjects(ctx context.Context, db Querier, ownerID ID, all bool) ([]Project, error) {
	q := `SELECT ` + projectCols + ` FROM projects WHERE owner_id = ?`
	if !all {
		q += ` AND deleted = 0`
	}
	rows, err := db.QueryContext(ctx, q+` ORDER BY name, id`, ownerID)
	return collect(rows, err, scanProject)
}

func scanProject(s scanner) (Project, error) {
	var p Project
	var kind string
	var deleted int
	var created int64
	if err := s.Scan(&p.ID, &p.OwnerID, &p.Name, &p.Description, &kind, &p.Source.MachineID, &p.Source.Path, &deleted, &created); err != nil {
		return p, mapErr(err)
	}
	p.Source.Kind, p.Deleted, p.CreatedAt = SourceKind(kind), deleted != 0, tm(created)
	return p, nil
}
