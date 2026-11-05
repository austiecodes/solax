// Package framing reads and writes the native wire formats of the agents:
// strict JSONL (pi, Claude Code) and JSON-RPC 2.0 over JSONL (Codex).
//
// Rules:
//   - Split records only on '\n'. Accept an optional '\r' before it.
//   - Do not split on U+2028 or U+2029. They are valid inside JSON strings.
//   - Read stdout without pause. A full pipe stalls pi and Claude Code.
package framing

import (
	"io"

	"github.com/austiecodes/solax/internal/errs"
)

// MaxRecord is the largest record that the reader accepts.
const MaxRecord = 64 << 20

// Reader reads JSONL records.
type Reader struct {
	r io.Reader
}

// NewReader returns a Reader on r.
func NewReader(r io.Reader) *Reader { return &Reader{r: r} }

// Next returns the next record without the line terminator. It returns
// io.EOF after the last record.
func (r *Reader) Next() ([]byte, error) {
	return nil, errs.ErrNotImplemented
}

// Writer writes JSONL records.
type Writer struct {
	w io.Writer
}

// NewWriter returns a Writer on w.
func NewWriter(w io.Writer) *Writer { return &Writer{w: w} }

// Write encodes v as one JSON record and a '\n'. It is safe for concurrent use.
func (w *Writer) Write(v any) error {
	return errs.ErrNotImplemented
}
