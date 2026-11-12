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

// The hello Drivers list must keep a stable order across connects; a map
// walk gave a random order before.
func TestRegistryOrder(t *testing.T) {
	ds := []Driver{fakeDriver{ir.KindPi}, fakeDriver{ir.KindCodex}, fakeDriver{ir.KindClaude}}
	r := NewRegistry(ds...)
	for range 5 {
		got := r.All()
		if len(got) != len(ds) {
			t.Fatalf("All() returned %d drivers, want %d", len(got), len(ds))
		}
		for i := range ds {
			if got[i].Kind() != ds[i].Kind() {
				t.Fatalf("position %d = %q, want %q", i, got[i].Kind(), ds[i].Kind())
			}
		}
	}
	if _, err := r.Get(ir.Kind("nope")); err == nil {
		t.Fatal("Get of an unknown kind returned no error")
	}
}
