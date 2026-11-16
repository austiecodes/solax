package cli

import (
	"bytes"
	"io"
	"strings"

	"github.com/austiecodes/solax/internal/locale"
)

// tableWriter lines up tab-separated cells in columns, as text/tabwriter
// does with a padding of 2, but counts the width of a cell in terminal
// columns: a Chinese header or name takes two per character. Write the
// rows, then Flush.
type tableWriter struct {
	w   io.Writer
	buf bytes.Buffer
}

func table(w io.Writer) *tableWriter { return &tableWriter{w: w} }

func (t *tableWriter) Write(p []byte) (int, error) { return t.buf.Write(p) }

// Flush writes the rows. The last cell of a row is not padded.
func (t *tableWriter) Flush() error {
	text := strings.TrimSuffix(t.buf.String(), "\n")
	t.buf.Reset()
	if text == "" {
		return nil
	}
	var rows [][]string
	var widths []int
	for _, line := range strings.Split(text, "\n") {
		cells := strings.Split(line, "\t")
		for i, c := range cells[:len(cells)-1] {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], locale.Width(c))
		}
		rows = append(rows, cells)
	}
	var b strings.Builder
	for _, cells := range rows {
		for i, c := range cells {
			if i < len(cells)-1 {
				b.WriteString(locale.Pad(c, widths[i]+2))
			} else {
				b.WriteString(c)
			}
		}
		b.WriteByte('\n')
	}
	_, err := io.WriteString(t.w, b.String())
	return err
}
