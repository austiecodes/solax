// Package agenttest records native transcripts and replays them through a
// Decoder for golden tests. Transcripts live in testdata/transcripts/<kind>/.
package agenttest

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/agent/framing"
	"github.com/austiecodes/solax/pkg/ir"
)

var update = flag.Bool("update", false, "rewrite golden IR files")

// Recorder copies the raw stdout of an agent process to w.
type Recorder struct {
	w io.Writer
}

// NewRecorder returns a Recorder that writes to w.
func NewRecorder(w io.Writer) *Recorder { return &Recorder{w: w} }

// Wrap returns a reader that records everything read from r.
func (r *Recorder) Wrap(rd io.Reader) io.Reader { return io.TeeReader(rd, r.w) }

// Transcript returns the path of a transcript under testdata/transcripts
// at the repository root.
func Transcript(kind ir.Kind, name string) string {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..", "..")
	return filepath.Join(root, "testdata", "transcripts", string(kind), name)
}

// Decode replays a transcript through d and returns the IR events. A
// decoder with NoteInput is told about one input first, as a session does
// before it writes the prompt.
func Decode(t *testing.T, d agent.Decoder, path string) []ir.Event {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if n, ok := d.(interface{ NoteInput() }); ok {
		n.NoteInput()
	}
	var out []ir.Event
	r := framing.NewReader(f)
	for {
		rec, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		evs, err := d.Decode(rec)
		if err != nil {
			t.Fatalf("decode %s: %v", rec, err)
		}
		out = append(out, evs...)
	}
	return out
}

// Replay decodes the transcript at path and compares the IR with the
// golden file next to it (<path>.ir.json). With -update, it writes the
// golden file. It returns the events for further checks.
func Replay(t *testing.T, d agent.Decoder, path string) []ir.Event {
	t.Helper()
	evs := Decode(t, d, path)
	got, err := json.MarshalIndent(evs, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	golden := path + ".ir.json"
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return evs
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run the test with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("IR differs from %s (run with -update after a review)\n--- got ---\n%s", golden, got)
	}
	return evs
}

// CheckTurns checks the rules that hold for every driver: turns do not
// nest, every turn.started has one turn.completed, and every item.started
// has an item.completed with the same ID.
func CheckTurns(t *testing.T, evs []ir.Event) {
	t.Helper()
	open := ""
	items := map[string]bool{}
	for i, e := range evs {
		switch e.Type {
		case ir.TurnStarted:
			if open != "" {
				t.Fatalf("event %d: turn %q started inside turn %q", i, e.TurnID, open)
			}
			open = e.TurnID
		case ir.TurnCompleted:
			if open == "" || e.TurnID != open {
				t.Fatalf("event %d: turn %q completed, open turn %q", i, e.TurnID, open)
			}
			open = ""
		case ir.ItemStarted:
			items[e.Item.ID] = true
		case ir.ItemCompleted:
			delete(items, e.Item.ID)
		}
	}
	if open != "" {
		t.Errorf("turn %q never completed", open)
	}
	for id := range items {
		t.Errorf("item %q started and never completed", id)
	}
}
