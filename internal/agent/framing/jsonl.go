// Package framing reads and writes the native wire formats of the agents:
// strict JSONL (pi, Claude Code) and JSON-RPC 2.0 over JSONL (Codex).
//
// Rules:
//   - Split records only on '\n'. Accept an optional '\r' before it.
//   - Do not split on U+2028 or U+2029. They are valid inside JSON strings.
//   - Read stdout without pause. A full pipe stalls pi and Claude Code.
package framing

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// MaxRecord is the largest record that the reader accepts.
const MaxRecord = 64 << 20

// ErrTooLarge is returned for a record longer than MaxRecord.
var ErrTooLarge = errors.New("framing: record too large")

// Reader reads JSONL records.
type Reader struct {
	r *bufio.Reader
}

// NewReader returns a Reader on r.
func NewReader(r io.Reader) *Reader { return &Reader{r: bufio.NewReaderSize(r, 64<<10)} }

// Next returns the next non-empty record without the line terminator. It
// returns io.EOF after the last record. A last record without '\n' is
// still returned.
func (r *Reader) Next() ([]byte, error) {
	for {
		var buf []byte
		for {
			chunk, err := r.r.ReadSlice('\n')
			if len(buf)+len(chunk) > MaxRecord {
				return nil, ErrTooLarge
			}
			buf = append(buf, chunk...)
			if err == nil {
				break
			}
			if errors.Is(err, bufio.ErrBufferFull) {
				continue
			}
			if errors.Is(err, io.EOF) && len(bytes.TrimSpace(buf)) > 0 {
				return trimEOL(buf), nil
			}
			return nil, err
		}
		if rec := trimEOL(buf); len(bytes.TrimSpace(rec)) > 0 {
			return rec, nil
		}
	}
}

func trimEOL(b []byte) []byte {
	b = bytes.TrimSuffix(b, []byte{'\n'})
	return bytes.TrimSuffix(b, []byte{'\r'})
}

// Writer writes JSONL records. The delivery path and the event pump both
// send, so it is safe for concurrent use.
type Writer struct {
	mu sync.Mutex
	w  io.Writer
}

// NewWriter returns a Writer on w.
func NewWriter(w io.Writer) *Writer { return &Writer{w: w} }

// Write encodes v as one JSON record and a '\n'. It is safe for concurrent use.
func (w *Writer) Write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("framing: encode: %w", err)
	}
	b = append(b, '\n')
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err = w.w.Write(b)
	return err
}
