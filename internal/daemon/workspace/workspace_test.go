package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/protocol"
)

func TestPrepare(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	home := t.TempDir()
	proj := filepath.Join(home, "app")
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(home, "notes.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	expand := func(p string) string {
		if rest, ok := strings.CutPrefix(p, "~/"); ok {
			return filepath.Join(home, rest)
		}
		return p
	}
	ws := Sources{
		protocol.WorkspaceLocal:   Local{Expand: expand},
		protocol.WorkspaceManaged: Managed{Root: filepath.Join(root, "projects")},
		protocol.WorkspaceSuper:   Fixed{Dir: filepath.Join(root, "super")},
	}

	// A local project is its folder, with "~" resolved.
	if dir, err := ws.Prepare(ctx, protocol.Workspace{Kind: protocol.WorkspaceLocal, Path: "~/app"}); err != nil || dir != proj {
		t.Fatalf("local = %q, %v; want %q", dir, err, proj)
	}
	// Solax does not create the folder of a project.
	for _, p := range []string{"~/missing", "~/notes.txt", ""} {
		if _, err := ws.Prepare(ctx, protocol.Workspace{Kind: protocol.WorkspaceLocal, Path: p}); err == nil {
			t.Errorf("local %q: no error", p)
		}
	}
	// The folder of a managed project is made on first use, and kept.
	want := filepath.Join(root, "projects", "notes")
	for range 2 {
		if dir, err := ws.Prepare(ctx, protocol.Workspace{Kind: protocol.WorkspaceManaged, Name: "notes"}); err != nil || dir != want {
			t.Fatalf("managed = %q, %v; want %q", dir, err, want)
		}
	}
	// The name of a managed folder cannot leave the projects folder.
	for _, n := range []string{"", "..", "a/b", "../x"} {
		if _, err := ws.Prepare(ctx, protocol.Workspace{Kind: protocol.WorkspaceManaged, Name: n}); !errors.Is(err, errs.ErrInvalid) {
			t.Errorf("managed %q: err = %v, want ErrInvalid", n, err)
		}
	}
	// The Super Agent has one folder.
	if dir, err := ws.Prepare(ctx, protocol.Workspace{Kind: protocol.WorkspaceSuper}); err != nil || dir != filepath.Join(root, "super") {
		t.Fatalf("super = %q, %v", dir, err)
	}
	// A kind this machine does not know fails clearly.
	if _, err := ws.Prepare(ctx, protocol.Workspace{Kind: "github"}); !errors.Is(err, errs.ErrInvalid) {
		t.Errorf("unknown kind: err = %v", err)
	}
}
