// Package csvsafe writes bounded CSV files whose text cells cannot run as spreadsheet formulas.
package csvsafe

import (
	"bytes"
	"encoding/csv"
	"errors"
	"strings"
	"unicode"
)

// ErrTooLarge reports a file that would exceed its writer's byte limit.
var ErrTooLarge = errors.New("csv exceeds its byte limit")

// limited exposes only Write, so bufio cannot bypass the limit through WriteString.
type limited struct {
	buf   bytes.Buffer
	limit int
}

func (b *limited) Write(data []byte) (int, error) {
	if len(data) > b.limit-b.buf.Len() {
		return 0, ErrTooLarge
	}
	return b.buf.Write(data)
}

// Writer buffers a whole CRLF file so a caller can refuse it before sending any byte.
type Writer struct {
	out *limited
	csv *csv.Writer
}

// NewWriter returns a Writer that refuses to hold more than limit bytes.
func NewWriter(limit int) *Writer {
	out := &limited{limit: limit}
	w := csv.NewWriter(out)
	w.UseCRLF = true
	return &Writer{out: out, csv: w}
}

// Write appends one record; an overflow may surface here or from Bytes.
func (w *Writer) Write(record []string) error {
	return w.csv.Write(record)
}

// Bytes returns the complete file, or nil and the first error.
func (w *Writer) Bytes() ([]byte, error) {
	w.csv.Flush()
	if err := w.csv.Error(); err != nil {
		return nil, err
	}
	return w.out.buf.Bytes(), nil
}

// Text prefixes an apostrophe to untrusted text that a spreadsheet would read as a formula.
func Text(value string) string {
	start := strings.TrimLeftFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	})
	if strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") || strings.HasPrefix(value, "\n") || (start != "" && strings.ContainsRune("=+-@", rune(start[0]))) {
		return "'" + value
	}
	return value
}
