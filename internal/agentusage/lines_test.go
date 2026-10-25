package agentusage

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

type seen struct {
	Line string
	Off  int64
}

func collect(t *testing.T, in string, maxLen int) ([]seen, scanResult) {
	t.Helper()
	var got []seen
	res, err := scanLines(strings.NewReader(in), maxLen, func(line []byte, off int64) {
		got = append(got, seen{string(line), off}) // copy: line is reused
	})
	if err != nil {
		t.Fatalf("scanLines error: %v", err)
	}
	return got, res
}

func TestScanLines(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		maxLen    int
		want      []seen
		consumed  int64
		oversized int
		tail      string
	}{
		{"empty input", "", 16, nil, 0, 0, ""},
		{"two complete lines", "a\nb\n", 16, []seen{{"a", 0}, {"b", 2}}, 4, 0, ""},
		{"trailing partial line", "a\nb", 16, []seen{{"a", 0}}, 2, 0, "b"},
		{"only a partial line", "abc", 16, nil, 0, 0, "abc"},
		{"blank lines ignored", "a\n\n\nb\n", 16, []seen{{"a", 0}, {"b", 4}}, 6, 0, ""},
		{"crlf tolerated", "a\r\nbc\r\n", 16, []seen{{"a", 0}, {"bc", 3}}, 7, 0, ""},
		{"cr-only line is blank", "a\n\r\nb\n", 16, []seen{{"a", 0}, {"b", 4}}, 6, 0, ""},
		{"offsets with varied lengths", "abc\nde\nf\n", 16, []seen{{"abc", 0}, {"de", 4}, {"f", 7}}, 9, 0, ""},
		{"exactly max is read", strings.Repeat("x", 16) + "\nok\n", 16, []seen{{strings.Repeat("x", 16), 0}, {"ok", 17}}, 20, 0, ""},
		{"max+1 is skipped, next read", strings.Repeat("x", 17) + "\nok\n", 16, []seen{{"ok", 18}}, 21, 1, ""},
		{"several oversized lines", strings.Repeat("y", 40) + "\n" + strings.Repeat("z", 100) + "\nok\n", 16, []seen{{"ok", 142}}, 145, 2, ""},
		{"oversized then partial tail", strings.Repeat("x", 20) + "\nab", 16, nil, 21, 1, "ab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, res := collect(t, tt.in, tt.maxLen)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("lines = %v, want %v", got, tt.want)
			}
			if res.Consumed != tt.consumed {
				t.Errorf("Consumed = %d, want %d", res.Consumed, tt.consumed)
			}
			if res.Oversized != tt.oversized {
				t.Errorf("Oversized = %d, want %d", res.Oversized, tt.oversized)
			}
			if string(res.Tail) != tt.tail {
				t.Errorf("Tail = %q, want %q", res.Tail, tt.tail)
			}
		})
	}
}

func TestScanLinesLongerThanReaderBuffer(t *testing.T) {
	// A line larger than the reader buffer (readerBufferBytes) must be
	// reassembled when under max and skipped when over it.
	long := strings.Repeat("a", 100_000)
	got, res := collect(t, long+"\nnext\n", 200_000)
	want := []seen{{long, 0}, {"next", 100_001}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("under max: got %d lines, want %v", len(got), []string{"long", "next"})
	}
	if res.Oversized != 0 || res.Consumed != 100_006 {
		t.Errorf("under max: %+v", res)
	}

	got, res = collect(t, long+"\nnext\n", 99_999)
	want = []seen{{"next", 100_001}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("over max: got %v, want %v", got, want)
	}
	if res.Oversized != 1 || res.Consumed != 100_006 {
		t.Errorf("over max: %+v", res)
	}
}

func TestMaxLineBytes(t *testing.T) {
	if MaxLineBytes != 4<<20 {
		t.Errorf("MaxLineBytes = %d, want %d", MaxLineBytes, 4<<20)
	}
}

func TestScanLinesOversizedWithoutNewline(t *testing.T) {
	// An over-long final line with no newline is neither kept in Tail nor
	// counted: it may never be completed.
	in := "ok\n" + strings.Repeat("x", 100_000)
	got, res := collect(t, in, 16)
	if want := []seen{{"ok", 0}}; !reflect.DeepEqual(got, want) {
		t.Errorf("lines = %v, want %v", got, want)
	}
	if res.Consumed != 3 || res.Oversized != 0 || len(res.Tail) != 0 {
		t.Errorf("result = %+v", res)
	}
}

func TestScanLinesCRLFAtMax(t *testing.T) {
	x := strings.Repeat("x", 16)
	got, res := collect(t, x+"\r\n", 16)
	if want := []seen{{x, 0}}; !reflect.DeepEqual(got, want) {
		t.Errorf("lines = %v, want %v", got, want)
	}
	if res.Oversized != 0 || res.Consumed != 18 {
		t.Errorf("result = %+v", res)
	}
}

func TestScanLinesOneOverMaxWithCRLF(t *testing.T) {
	got, res := collect(t, strings.Repeat("x", 17)+"\r\nok\n", 16)
	if want := []seen{{"ok", 19}}; !reflect.DeepEqual(got, want) {
		t.Errorf("lines = %v, want %v", got, want)
	}
	if res.Oversized != 1 || res.Consumed != 22 {
		t.Errorf("result = %+v", res)
	}
}

type failingReader struct{ data string }

var errBoom = errors.New("boom")

func (f *failingReader) Read(p []byte) (int, error) {
	if f.data == "" {
		return 0, errBoom
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}

func TestScanLinesReaderError(t *testing.T) {
	var got []string
	res, err := scanLines(&failingReader{data: "a\nb"}, 16, func(line []byte, _ int64) {
		got = append(got, string(line))
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want errBoom", err)
	}
	if !reflect.DeepEqual(got, []string{"a"}) || res.Consumed != 2 {
		t.Errorf("got %v, %+v", got, res)
	}
}
