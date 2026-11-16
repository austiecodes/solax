package agent

import (
	"context"
	"testing"

	"github.com/austiecodes/solax/pkg/ir"
)

type fakeDriver struct{ kind ir.Kind }

func (d fakeDriver) Kind() ir.Kind                        { return d.kind }
func (d fakeDriver) Probe(context.Context) (Probe, error) { return Probe{OK: true}, nil }
func (d fakeDriver) Start(context.Context, SessionSpec) (Session, error) {
	return nil, nil
}

func TestFind(t *testing.T) {
	ds := []Driver{fakeDriver{ir.KindPi}, fakeDriver{ir.KindCodex}, fakeDriver{ir.KindClaude}}
	d, err := Find(ds, ir.KindCodex)
	if err != nil || d.Kind() != ir.KindCodex {
		t.Fatalf("Find(codex) = %v, %v", d, err)
	}
	if _, err := Find(ds, ir.Kind("nope")); err == nil {
		t.Fatal("Find of an unknown kind returned no error")
	}
}
