package corpus

import (
	"bufio"
	"io"
	"strings"
)

// maxLineBytes bounds one JSONL line. A single log line can carry a whole tool
// output, so the scanner buffer is generous but still finite: a line above the
// limit is reported as truncated instead of growing memory without bound.
const maxLineBytes = 4 << 20

// lineScanner is a bounded line reader over one decoded log stream.
type lineScanner struct {
	scanner   *bufio.Scanner
	line      string
	err       error
	truncated bool
}

// newLineScanner wraps a decoded JSONL stream.
func newLineScanner(r io.Reader) *lineScanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), maxLineBytes)
	return &lineScanner{scanner: scanner}
}

// scan advances to the next retained line. It reports false at end of input or
// on a read error; both are silent failures for a best-effort collector.
func (l *lineScanner) scan() bool {
	if l.scanner.Scan() {
		l.line = l.scanner.Text()
		return true
	}
	l.err = l.scanner.Err()
	if l.err != nil && strings.Contains(l.err.Error(), "token too long") {
		l.truncated = true
	}
	return false
}

// text returns the current line.
func (l *lineScanner) text() string { return l.line }
