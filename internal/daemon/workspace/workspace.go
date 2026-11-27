// Package workspace prepares the work folder of a session on this
// machine, from the workspace that the server names. Each kind of
// workspace has a Source: the folder of a local project, the folder of a
// managed project, and the folder of the Super Agent. A Git repository
// becomes one more Source, which clones the repository in the projects
// folder.
package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Source prepares the work folders of one kind of workspace.
type Source interface {
	// Prepare returns the folder for w, ready for an agent to start in.
	Prepare(ctx context.Context, w protocol.Workspace) (dir string, err error)
}

// Sources picks the Source of each kind of workspace.
type Sources map[protocol.WorkspaceKind]Source

// Prepare prepares w with the Source of its kind.
func (s Sources) Prepare(ctx context.Context, w protocol.Workspace) (string, error) {
	src, ok := s[w.Kind]
	if !ok {
		return "", fmt.Errorf("%w: workspace kind %q is not supported on this machine", errs.ErrInvalid, w.Kind)
	}
	return src.Prepare(ctx, w)
}

// Local is a folder of a local project. The folder must exist: Solax
// does not create the folder of a project.
type Local struct {
	// Expand resolves "~" to the home folder. Nil leaves the path as it is.
	Expand func(string) string
}

// Prepare returns w.Path, with "~" resolved, when it is a folder.
func (l Local) Prepare(_ context.Context, w protocol.Workspace) (string, error) {
	dir := w.Path
	if l.Expand != nil {
		dir = l.Expand(dir)
	}
	if dir == "" {
		return "", fmt.Errorf("%w: local workspace without a path", errs.ErrInvalid)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("project folder: %w", err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("%w: project folder %s is not a folder", errs.ErrInvalid, dir)
	}
	return dir, nil
}

// Managed is the projects folder of the daemon, Root. Each managed
// project has its folder in it, named w.Name. Solax makes the folder on
// first use and keeps it, also when the project is removed.
type Managed struct {
	Root string
}

// Prepare makes Root/<w.Name> when it does not exist and returns it.
func (m Managed) Prepare(_ context.Context, w protocol.Workspace) (string, error) {
	if w.Name == "" || w.Name != filepath.Base(w.Name) || w.Name == "." || w.Name == ".." {
		return "", fmt.Errorf("%w: managed project folder %q", errs.ErrInvalid, w.Name)
	}
	return makeDir(filepath.Join(m.Root, w.Name))
}

// Fixed is one folder for each session of its kind: the folder of the
// Super Agent. Solax makes it on first use.
type Fixed struct {
	Dir string
}

// Prepare makes Dir when it does not exist and returns it.
func (f Fixed) Prepare(context.Context, protocol.Workspace) (string, error) { return makeDir(f.Dir) }

func makeDir(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("work folder: %w", err)
	}
	return dir, nil
}
