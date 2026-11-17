package agent

import (
	"context"
	"testing"

	"github.com/austiecodes/solax/pkg/ir"
)

type fakeRuntime struct{ kind ir.Kind }

func (d fakeRuntime) Kind() ir.Kind                        { return d.kind }
func (d fakeRuntime) Probe(context.Context) (Probe, error) { return Probe{OK: true}, nil }
func (d fakeRuntime) Start(context.Context, SessionSpec) (Session, error) {
	return nil, nil
}

func TestFind(t *testing.T) {
	ds := []Runtime{fakeRuntime{ir.KindPi}, fakeRuntime{ir.KindCodex}, fakeRuntime{ir.KindClaude}}
	d, err := Find(ds, ir.KindCodex)
	if err != nil || d.Kind() != ir.KindCodex {
		t.Fatalf("Find(codex) = %v, %v", d, err)
	}
	if _, err := Find(ds, ir.Kind("nope")); err == nil {
		t.Fatal("Find of an unknown kind returned no error")
	}
}
