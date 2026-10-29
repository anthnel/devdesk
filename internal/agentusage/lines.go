package agentusage

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

// readerBufferBytes is the bufio buffer size; longer lines are reassembled.
const readerBufferBytes = 64 << 10

// MaxLineBytes is the longest line (newline excluded) kept in memory.
const MaxLineBytes = 4 << 20

// scanResult describes what scanLines and scanFrom consumed.
type scanResult struct {
	// Consumed is the offset just past the last complete line.
	Consumed int64
	// Oversized counts complete lines longer than maxLen that were skipped.
	Oversized int
	// OversizedAt holds the offset of each skipped line, in order.
	OversizedAt []int64
	// Tail holds the bytes after the last newline (an incomplete line).
	Tail []byte
	// End is base + the number of bytes read (base is 0 for scanLines).
	End int64
	// SkipFrom is the start of an oversized line cut by EOF or a read error, or -1.
	SkipFrom int64
}

// scanFrom is scanLines for a stream positioned at byte base of a file:
// offsets given to fn and returned are absolute. If skipFrom >= 0 the stream
// starts inside an oversized line that began at skipFrom: bytes up to the
// first "\n" are skipped and that line is counted once, at skipFrom.
// Consumed starts at base, End is base plus the bytes read, and SkipFrom is
// the start of an oversized line still unfinished at EOF (-1 otherwise).
func scanFrom(r io.Reader, base, skipFrom int64, maxLen int, fn func(line []byte, off int64)) (scanResult, error) {
	br := bufio.NewReaderSize(r, readerBufferBytes)
	res := scanResult{Consumed: base, End: base, SkipFrom: -1}
	var (
		pending  []byte // line prefix reassembled across buffer fills
		skipping = skipFrom >= 0
		start    = base // offset of the current line's first byte
	)
	if skipping {
		start = skipFrom
	}
	for {
		chunk, err := br.ReadSlice('\n')
		res.End += int64(len(chunk))
		if !skipping {
			// maxLen+1 bytes are kept so a trailing "\r" does not count against maxLen.
			if len(pending)+len(chunk) > maxLen+2 { // +1 more: chunk may end with "\n"
				skipping, pending = true, nil
			} else {
				pending = append(pending, chunk...)
			}
		}
		switch {
		case err == nil:
			if skipping {
				res.skip(start)
			} else {
				res.emit(pending, start, maxLen, fn)
			}
			res.Consumed = res.End
			start, pending, skipping = res.End, pending[:0], false
		case errors.Is(err, bufio.ErrBufferFull):
		case errors.Is(err, io.EOF):
			if skipping {
				res.SkipFrom = start
			} else {
				res.Tail = bytes.Clone(pending)
			}
			return res, nil
		default:
			if skipping {
				res.SkipFrom = start // keep the oversized line resumable
			}
			return res, err
		}
	}
}

// scanLines calls fn for every complete, non-empty line of r no longer
// than maxLen bytes. off is the offset of the line's first byte; line has no
// trailing "\n" or "\r" and is only valid during the call: fn must copy it
// to keep it. Complete lines longer than maxLen are skipped without being held
// in memory. An incomplete final line is returned in Tail whatever its
// length, unless it already exceeds maxLen: it is then dropped, and counted in
// neither Consumed nor Oversized (it may never be completed). A Tail of
// maxLen+1 or maxLen+2 bytes is still kept (the cutoff is maxLen+2, since a
// "\r" may follow); one longer than that is dropped.
func scanLines(r io.Reader, maxLen int, fn func(line []byte, off int64)) (scanResult, error) {
	return scanFrom(r, 0, -1, maxLen, fn)
}

// emit trims the line terminator from raw (which ends with "\n") and hands
// the line to fn, or counts it as oversized.
func (res *scanResult) emit(raw []byte, off int64, maxLen int, fn func([]byte, int64)) {
	line := bytes.TrimSuffix(bytes.TrimSuffix(raw, []byte("\n")), []byte("\r"))
	switch {
	case len(line) > maxLen:
		res.skip(off)
	case len(line) > 0:
		fn(line, off)
	}
}

// skip counts one oversized line starting at off.
func (res *scanResult) skip(off int64) {
	res.Oversized++
	res.OversizedAt = append(res.OversizedAt, off)
}
