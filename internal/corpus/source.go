package corpus

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
)

// MaxBackfillFileBytes bounds one session log. For plain JSONL only the tail is
// kept, because the end of a session is what a digest needs. For a zstd stream
// the limit applies to the decoded bytes: a compressed stream cannot be read
// from its tail, so decoding stops at the limit. Both cases set
// truncated: true.
const MaxBackfillFileBytes = 8 << 20

// zstdMagic is the frame magic of a zstd stream. It is checked on content
// rather than on the file name so a renamed file still decodes and a plain file
// named .zst is not misread.
var zstdMagic = []byte{0x28, 0xB5, 0x2F, 0xFD}

// sessionStream is one bounded view of a session log.
type sessionStream struct {
	reader io.Reader
	close  func() error
	// truncated reports that the stream was cut at the size limit, so the
	// digest must say it did not see the whole session.
	truncated bool
	// capped is set for a compressed stream, whose truncation is only known
	// after the decoder has consumed the bounded reader.
	capped *cappedReader
}

// Close releases the underlying file.
func (s *sessionStream) Close() error {
	if s.close == nil {
		return nil
	}
	return s.close()
}

// Truncated reports whether the collector saw only a prefix or a tail of the
// log. A compressed stream is truncated exactly when decoding consumed the
// whole byte budget.
func (s *sessionStream) Truncated() bool {
	if s.capped != nil {
		return s.capped.hit
	}
	return s.truncated
}

// openSessionStream opens one log file without following a symbolic link and
// bounds how much of it the collector may read.
func openSessionStream(path string) (*sessionStream, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	var magic [4]byte
	n, _ := io.ReadFull(file, magic[:])
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, err
	}
	compressed := n == len(magic) && bytes.Equal(magic[:], zstdMagic)

	if compressed {
		// The bound applies to the decoded bytes, so a capped reader sits in
		// front of the decoder and records whether decoding actually reached
		// the limit. The flag is read after the collector has consumed the
		// stream.
		capped := &cappedReader{reader: file, limit: MaxBackfillFileBytes}
		return &sessionStream{reader: capped, close: file.Close, capped: capped}, nil
	}

	if info.Size() > MaxBackfillFileBytes {
		// Plain JSONL: keep the tail. The first line of the window is very
		// likely partial, so the scanner discards it. The file's own first
		// line is read before the seek and put in front of the tail: Codex
		// writes its session_meta record (session id and cwd) there, and a
		// large session would otherwise lose its project key. See
		// docs/18-agent-memory-review-and-optimization.md section 4.3.2.
		first := readFirstLine(file, info.Size()-MaxBackfillFileBytes)
		if _, err := file.Seek(info.Size()-MaxBackfillFileBytes, io.SeekStart); err != nil {
			file.Close()
			return nil, err
		}
		var reader io.Reader = skipPartialLine(file)
		if first != nil {
			reader = io.MultiReader(bytes.NewReader(first), reader)
		}
		return &sessionStream{reader: reader, close: file.Close, truncated: true}, nil
	}
	return &sessionStream{reader: file, close: file.Close}, nil
}

// MaxFirstLineBytes bounds the leading line kept in front of a truncated
// plain log. A longer first line is not a metadata record worth keeping and
// is dropped like any other line outside the tail window.
const MaxFirstLineBytes = 64 << 10

// readFirstLine returns the file's first line with its newline when it is
// complete within MaxFirstLineBytes and ends before tailStart, the offset where
// the kept tail window begins. A first line that reaches into the window is
// already covered by the tail's partial-line skip, so returning it would
// duplicate it. The caller seeks afterwards, so the read offset is irrelevant.
func readFirstLine(file *os.File, tailStart int64) []byte {
	limit := int64(MaxFirstLineBytes)
	if tailStart < limit {
		limit = tailStart
	}
	if limit <= 0 {
		return nil
	}
	buf := make([]byte, limit)
	n, err := file.ReadAt(buf, 0)
	if n <= 0 && err != nil {
		return nil
	}
	index := bytes.IndexByte(buf[:n], '\n')
	if index < 0 {
		return nil
	}
	return append([]byte(nil), buf[:index+1]...)
}

// skipPartialLine drops the first line of a tail window. When the window
// happens to start exactly at a line boundary the first complete line is
// dropped too, which loses at most one line rather than corrupting a record.
func skipPartialLine(r io.Reader) io.Reader {
	return &partialLineReader{reader: r}
}

// partialLineReader discards everything before the first newline.
type partialLineReader struct {
	reader io.Reader
	ready  bool
}

// Read implements io.Reader.
func (p *partialLineReader) Read(buf []byte) (int, error) {
	if !p.ready {
		p.ready = true
		if err := discardUntilNewline(p.reader); err != nil {
			return 0, err
		}
	}
	return p.reader.Read(buf)
}

// discardUntilNewline consumes bytes up to and including the first newline.
func discardUntilNewline(r io.Reader) error {
	var one [1]byte
	for {
		n, err := r.Read(one[:])
		if n > 0 && one[0] == '\n' {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// cappedReader reports when a reader was cut short at the limit, so a zstd
// digest is marked truncated only when decoding actually stopped at the bound.
type cappedReader struct {
	reader io.Reader
	read   int64
	limit  int64
	hit    bool
}

// Read implements io.Reader.
func (c *cappedReader) Read(buf []byte) (int, error) {
	if c.read >= c.limit {
		c.hit = true
		return 0, io.EOF
	}
	// A read that fills the remaining budget marks the stream as cut only when
	// the decoder asks for more afterwards; a file that ends exactly at the
	// limit is not truncated.
	if remaining := c.limit - c.read; int64(len(buf)) > remaining {
		buf = buf[:remaining]
	}
	n, err := c.reader.Read(buf)
	c.read += int64(n)
	return n, err
}
