package nexusbench

import "bytes"

// capWriter keeps the first max bytes written to it and drops the rest, so a
// chatty or hostile tool cannot fill the memory of the bench. Write never fails:
// a failing writer would make os/exec stop the process on a full buffer.
type capWriter struct {
	buf  bytes.Buffer
	max  int
	over bool // more than max bytes were written
}

// newCapWriter returns a writer that keeps at most max bytes.
func newCapWriter(limit int) *capWriter { return &capWriter{max: limit} }

// Write implements io.Writer.
func (w *capWriter) Write(p []byte) (int, error) {
	n := len(p)
	if room := w.max - w.buf.Len(); n > room {
		w.over = true
		p = p[:max(room, 0)]
	}
	w.buf.Write(p)
	return n, nil
}
