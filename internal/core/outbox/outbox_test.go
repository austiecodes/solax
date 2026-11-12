package outbox

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/store/sqlite"
)

func TestBackoffGrowsAndCaps(t *testing.T) {
	o := Options{}.withDefaults()
	prev := time.Duration(0)
	var last time.Duration
	for attempts := 0; attempts < 10; attempts++ {
		d := backoff(o, attempts)
		if d < prev {
			t.Fatalf("attempts %d: %v shrinks below %v", attempts, d, prev)
		}
		if d > o.MaxBackoff {
			t.Fatalf("attempts %d: %v exceeds MaxBackoff %v", attempts, d, o.MaxBackoff)
		}
		prev, last = d, d
	}
	if last != o.MaxBackoff {
		t.Fatalf("backoff did not reach MaxBackoff: %v", last)
	}
	if got := backoff(o, 0); got != o.MinBackoff {
		t.Fatalf("first attempt = %v, want MinBackoff %v", got, o.MinBackoff)
	}
}

func TestOptionsDefaults(t *testing.T) {
	o := Options{}.withDefaults()
	if o.Concurrency <= 0 || o.MinBackoff <= 0 || o.MaxBackoff < o.MinBackoff {
		t.Fatalf("bad defaults: %+v", o)
	}
	// A caller that inverts the bounds gets sorted ones.
	o = Options{MinBackoff: time.Hour, MaxBackoff: time.Second}.withDefaults()
	if o.MinBackoff > o.MaxBackoff {
		t.Fatalf("bounds not sorted: %+v", o)
	}
}

func TestSignalDuringRunRunsAgain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ob := New(store, nil)
	ob.poll = time.Hour // only Kick may wake it
	store.OnEffects(ob.Kick)

	enqueue := func() {
		if err := store.Atomic(ctx, func(tx core.Tx) error {
			return tx.Effects().Enqueue(ctx, "test", "k", nil)
		}); err != nil {
			t.Error(err)
		}
	}
	var runs atomic.Int32
	done := make(chan struct{})
	ob.Handle("test", func(ctx context.Context, ef core.Effect) error {
		switch runs.Add(1) {
		case 1:
			enqueue() // a new signal while running
		case 2:
			close(done)
		}
		return nil
	}, Options{})
	ob.Handle("panics", func(ctx context.Context, ef core.Effect) error { panic("bad handler") }, Options{})
	if err := store.Atomic(ctx, func(tx core.Tx) error {
		return tx.Effects().Enqueue(ctx, "panics", "p", nil)
	}); err != nil {
		t.Fatal(err)
	}

	go ob.Run(ctx)
	enqueue()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("second run never happened (runs = %d)", runs.Load())
	}
}
