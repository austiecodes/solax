package core

import (
	"testing"
	"time"
)

// IDs of one prefix sort by creation time across distinct milliseconds,
// including at the Unix epoch, where the base-36 timestamp is short. (IDs
// minted in the same millisecond carry a random suffix and need not sort.)
func TestNewIDAtSorts(t *testing.T) {
	times := []time.Time{
		time.Unix(0, 0),
		time.Unix(0, 1_000_000),
		time.Unix(0, 999_999_000),
		time.Unix(1_700_000_000, 0),
	}
	var prev ID
	for _, ts := range times {
		for range 3 {
			id := NewIDAt(PrefixTask, ts)
			if prev != "" && id <= prev {
				t.Fatalf("ID %q does not sort after %q", id, prev)
			}
			if got := id.Prefix(); got != PrefixTask {
				t.Fatalf("Prefix() = %q, want %q", got, PrefixTask)
			}
		}
		prev = NewIDAt(PrefixTask, ts)
	}
}

func TestNewIDAtFixedWidth(t *testing.T) {
	// prefix + "_" + 9 base-36 digits + 16 hex digits, whatever the clock.
	want := len("ses_") + idTimeWidth + 16
	for _, ts := range []time.Time{time.Unix(0, 0), time.Now()} {
		if got := len(NewIDAt(PrefixSession, ts)); got != want {
			t.Fatalf("%s: len = %d, want %d", ts.UTC().Format(time.RFC3339), got, want)
		}
	}
}

func TestPrefixesAreDistinct(t *testing.T) {
	seen := map[Prefix]bool{}
	for _, p := range []Prefix{PrefixOwner, PrefixClient, PrefixMachine, PrefixAgent, PrefixWorkspace, PrefixTask, PrefixMessage, PrefixSession, PrefixBatch} {
		if p == "" || seen[p] {
			t.Fatalf("prefix %q is empty or repeated", p)
		}
		seen[p] = true
	}
}
