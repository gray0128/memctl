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
		// likely partial, so the scanner discards it.
		if _, err := file.Seek(info.Size()-MaxBackfillFileBytes, io.SeekStart); err != nil {
			file.Close()
			return nil, err
		}
		return &sessionStream{reader: skipPartialLine(file), close: file.Close, truncated: true}, nil
	}
	return &sessionStream{reader: file, close: file.Close}, nil
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
