// Package agenttest records native transcripts and replays them through a
// Decoder for golden tests. Transcripts live in testdata/transcripts/<kind>/.
package agenttest

import (
	"io"
	"testing"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/errs"
)

// Recorder copies the raw stdout of an agent process to w.
type Recorder struct {
	w io.Writer
}

// NewRecorder returns a Recorder that writes to w.
func NewRecorder(w io.Writer) *Recorder { return &Recorder{w: w} }

// Wrap returns a reader that records everything read from r.
func (r *Recorder) Wrap(rd io.Reader) io.Reader { return io.TeeReader(rd, r.w) }

// Replay decodes the transcript at path and compares the IR with the
// golden file next to it (<path>.ir.json). With -update, it writes the
// golden file.
func Replay(t *testing.T, d agent.Decoder, path string) {
	t.Helper()
	t.Skip(errs.ErrNotImplemented.Error())
}
