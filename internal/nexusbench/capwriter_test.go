package nexusbench

import (
	"strings"
	"testing"
)

func TestCapWriter(t *testing.T) {
	tests := []struct {
		name     string
		limit    int
		writes   []string
		wantBuf  string
		wantOver bool
	}{
		{"under the limit", 8, []string{"abc"}, "abc", false},
		{"exactly the limit", 4, []string{"abcd"}, "abcd", false},
		{"exactly the limit in two writes", 4, []string{"ab", "cd"}, "abcd", false},
		{"one byte too many", 4, []string{"abcde"}, "abcd", true},
		{"the write that crosses the limit is cut", 4, []string{"ab", "cdef"}, "abcd", true},
		{"writes after the limit are dropped", 4, []string{"abcd", "efgh", "ij"}, "abcd", true},
		{"a zero limit keeps nothing", 0, []string{"abc"}, "", true},
		{"a zero limit with nothing written", 0, nil, "", false},
		{"an empty write is not an overflow", 0, []string{""}, "", false},
		{"an empty write after a full buffer", 3, []string{"abc", ""}, "abc", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newCapWriter(tt.limit)
			for _, s := range tt.writes {
				n, err := w.Write([]byte(s))
				if err != nil {
					t.Errorf("Write(%q) failed: %v (a failing writer would make os/exec stop the process)", s, err)
				}
				if n != len(s) {
					t.Errorf("Write(%q) = %d, want %d: the whole write is reported as consumed", s, n, len(s))
				}
			}
			if got := w.buf.String(); got != tt.wantBuf {
				t.Errorf("kept %q, want %q", got, tt.wantBuf)
			}
			if w.over != tt.wantOver {
				t.Errorf("over = %v, want %v", w.over, tt.wantOver)
			}
		})
	}
}

// A chatty tool cannot grow the buffer past its limit, however it writes.
func TestCapWriterStaysBounded(t *testing.T) {
	w := newCapWriter(1000)
	chunk := []byte(strings.Repeat("y", 333))
	for range 100 {
		if n, err := w.Write(chunk); n != len(chunk) || err != nil {
			t.Fatalf("Write = %d, %v; want %d, nil", n, err, len(chunk))
		}
	}
	if w.buf.Len() != 1000 {
		t.Errorf("kept %d bytes, want exactly the limit, 1000", w.buf.Len())
	}
	if !w.over {
		t.Error("over = false after 33300 bytes into a 1000 byte writer")
	}
}
