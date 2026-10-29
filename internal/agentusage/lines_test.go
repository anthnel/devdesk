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

// scanFromCase runs scanFrom over in and returns the lines seen and the result.
func scanFromCase(t *testing.T, in string, base, skipFrom int64, maxLen int) ([]seen, scanResult) {
	t.Helper()
	var got []seen
	res, err := scanFrom(strings.NewReader(in), base, skipFrom, maxLen, func(line []byte, off int64) {
		got = append(got, seen{string(line), off}) // copy: line is reused
	})
	if err != nil {
		t.Fatalf("scanFrom error: %v", err)
	}
	return got, res
}

func TestScanFrom(t *testing.T) {
	x10 := strings.Repeat("x", 10)
	tests := []struct {
		name        string
		in          string
		base        int64
		skipFrom    int64
		maxLen      int
		want        []seen
		consumed    int64
		end         int64
		resSkipFrom int64
		oversized   int
		oversizedAt []int64
		tail        string
	}{
		{"base shifts offsets", "a\nbb\n", 100, -1, 16, []seen{{"a", 100}, {"bb", 102}}, 105, 105, -1, 0, nil, ""},
		{"last line without newline is not consumed", "a\nbb", 100, -1, 16, []seen{{"a", 100}}, 102, 104, -1, 0, nil, "bb"},
		{"only a partial line leaves consumed at base", "abc", 100, -1, 16, nil, 100, 103, -1, 0, nil, "abc"},
		{"empty stream at base", "", 100, -1, 16, nil, 100, 100, -1, 0, nil, ""},
		{"blank lines keep absolute offsets", "a\n\n\nb\n", 7, -1, 16, []seen{{"a", 7}, {"b", 11}}, 13, 13, -1, 0, nil, ""},
		{"crlf with base", "a\r\nbc\r\n", 50, -1, 16, []seen{{"a", 50}, {"bc", 53}}, 57, 57, -1, 0, nil, ""},
		{"multibyte line with base", "é€\nok\n", 10, -1, 16, []seen{{"é€", 10}, {"ok", 16}}, 19, 19, -1, 0, nil, ""},
		{"line cut inside a rune stays in the tail", "ok\n\xc3", 10, -1, 16, []seen{{"ok", 10}}, 13, 14, -1, 0, nil, "\xc3"},
		{"complete oversized line counted once at its start", x10 + "\nok\n", 40, -1, 4, []seen{{"ok", 51}}, 54, 54, -1, 1, []int64{40}, ""},
		{"oversized cut by EOF sets skipFrom", "ok\n" + x10, 0, -1, 4, []seen{{"ok", 0}}, 3, 13, 3, 0, nil, ""},
		{"oversized cut by EOF, absolute base", "ok\n" + x10, 50, -1, 4, []seen{{"ok", 50}}, 53, 63, 53, 0, nil, ""},
		{"tail of maxLen+2 is still a tail", "ok\n" + strings.Repeat("x", 6), 0, -1, 4, []seen{{"ok", 0}}, 3, 9, -1, 0, nil, strings.Repeat("x", 6)},
		{"tail of maxLen+3 switches to skip mode", "ok\n" + strings.Repeat("x", 7), 0, -1, 4, []seen{{"ok", 0}}, 3, 10, 3, 0, nil, ""},
		{"resume in skip mode, newline arrives", "xxx\nok\n", 13, 3, 4, []seen{{"ok", 17}}, 20, 20, -1, 1, []int64{3}, ""},
		{"resume in skip mode, newline is the first byte", "\nok\n", 13, 3, 4, []seen{{"ok", 14}}, 17, 17, -1, 1, []int64{3}, ""},
		{"resume in skip mode, still no newline", "xxxx", 13, 3, 4, nil, 13, 17, 3, 0, nil, ""},
		{"resume in skip mode, nothing new", "", 13, 3, 4, nil, 13, 13, 3, 0, nil, ""},
		{"resume in skip mode, then a new oversized cut", "x\n" + x10, 13, 3, 4, nil, 15, 25, 15, 1, []int64{3}, ""},
		{"resume in skip mode, then a partial tail", "x\nab", 13, 3, 4, nil, 15, 17, -1, 1, []int64{3}, "ab"},
		{"resume in skip mode, crlf terminator", "xx\r\nok\n", 13, 3, 4, []seen{{"ok", 17}}, 20, 20, -1, 1, []int64{3}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, res := scanFromCase(t, tt.in, tt.base, tt.skipFrom, tt.maxLen)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("lines = %v, want %v", got, tt.want)
			}
			if res.Consumed != tt.consumed {
				t.Errorf("Consumed = %d, want %d", res.Consumed, tt.consumed)
			}
			if res.End != tt.end {
				t.Errorf("End = %d, want %d", res.End, tt.end)
			}
			if res.SkipFrom != tt.resSkipFrom {
				t.Errorf("SkipFrom = %d, want %d", res.SkipFrom, tt.resSkipFrom)
			}
			if res.Oversized != tt.oversized {
				t.Errorf("Oversized = %d, want %d", res.Oversized, tt.oversized)
			}
			if !reflect.DeepEqual(res.OversizedAt, tt.oversizedAt) {
				t.Errorf("OversizedAt = %v, want %v", res.OversizedAt, tt.oversizedAt)
			}
			if string(res.Tail) != tt.tail {
				t.Errorf("Tail = %q, want %q", res.Tail, tt.tail)
			}
		})
	}
}

// TestScanFromCRLFSplitAcrossReads: an oversized line whose "\r\n" is cut
// between two reads is counted exactly once, across the two calls.
func TestScanFromCRLFSplitAcrossReads(t *testing.T) {
	var lines []seen
	collectLine := func(line []byte, off int64) { lines = append(lines, seen{string(line), off}) }

	first, err := scanFrom(strings.NewReader("ok\nxxxxxxxx\r"), 0, -1, 4, collectLine)
	if err != nil {
		t.Fatal(err)
	}
	if first.SkipFrom != 3 || first.Oversized != 0 || first.End != 12 {
		t.Fatalf("first read = %+v, want SkipFrom 3, Oversized 0, End 12", first)
	}
	second, err := scanFrom(strings.NewReader("\nok2\n"), first.End, first.SkipFrom, 4, collectLine)
	if err != nil {
		t.Fatal(err)
	}
	if second.Oversized != 1 || !reflect.DeepEqual(second.OversizedAt, []int64{3}) {
		t.Errorf("second read = %+v, want one oversized line at 3", second)
	}
	if second.SkipFrom != -1 || second.Consumed != 17 {
		t.Errorf("second read = %+v, want SkipFrom -1, Consumed 17", second)
	}
	if want := []seen{{"ok", 0}, {"ok2", 13}}; !reflect.DeepEqual(lines, want) {
		t.Errorf("lines = %v, want %v", lines, want)
	}
}

// TestScanFromResumeAtEverySplit: reading a stream in two steps, resuming the
// way ReadDelta will (re-read the held-back tail, or continue in skip mode),
// yields the same lines and the same oversized lines as one read, whatever the
// split point. This covers UTF-8 runes, "\r\n" and oversized lines cut anywhere.
func TestScanFromResumeAtEverySplit(t *testing.T) {
	const maxLen = 6
	full := "ok\n" + strings.Repeat("x", 20) + "\r\nhé€\r\n\n" + strings.Repeat("y", 7) + "\nlast\n" + strings.Repeat("z", 12) + "\nend\n"

	wantLines, wantRes := scanFromCase(t, full, 0, -1, maxLen)
	if wantRes.Oversized != 3 || len(wantLines) < 4 {
		t.Fatalf("one-shot oracle looks wrong: %v %+v", wantLines, wantRes)
	}

	for k := 0; k <= len(full); k++ {
		var lines []seen
		var oversizedAt []int64
		collectLine := func(line []byte, off int64) { lines = append(lines, seen{string(line), off}) }

		first, err := scanFrom(strings.NewReader(full[:k]), 0, -1, maxLen, collectLine)
		if err != nil {
			t.Fatalf("k=%d: %v", k, err)
		}
		oversizedAt = append(oversizedAt, first.OversizedAt...)

		base, skipFrom := first.Consumed, int64(-1)
		if first.SkipFrom >= 0 {
			base, skipFrom = first.End, first.SkipFrom
		}
		second, err := scanFrom(strings.NewReader(full[base:]), base, skipFrom, maxLen, collectLine)
		if err != nil {
			t.Fatalf("k=%d second: %v", k, err)
		}
		oversizedAt = append(oversizedAt, second.OversizedAt...)

		if !reflect.DeepEqual(lines, wantLines) {
			t.Errorf("k=%d: lines = %v, want %v", k, lines, wantLines)
		}
		if !reflect.DeepEqual(oversizedAt, wantRes.OversizedAt) {
			t.Errorf("k=%d: oversized at %v, want %v", k, oversizedAt, wantRes.OversizedAt)
		}
		if second.Consumed != wantRes.Consumed || second.End != int64(len(full)) {
			t.Errorf("k=%d: Consumed/End = %d/%d, want %d/%d", k, second.Consumed, second.End, wantRes.Consumed, len(full))
		}
	}
}

// TestScanFromReaderErrorWhileSkipping: an error during a resumed skip returns
// what was consumed so far together with the error.
func TestScanFromReaderErrorWhileSkipping(t *testing.T) {
	var got []string
	res, err := scanFrom(&failingReader{data: "xxx\nok\nab"}, 13, 3, 4, func(line []byte, _ int64) {
		got = append(got, string(line))
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want errBoom", err)
	}
	if !reflect.DeepEqual(got, []string{"ok"}) {
		t.Errorf("lines = %v, want [ok]", got)
	}
	if res.Oversized != 1 || !reflect.DeepEqual(res.OversizedAt, []int64{3}) || res.Consumed != 20 {
		t.Errorf("result = %+v, want Oversized 1 at [3], Consumed 20", res)
	}
}

// TestScanFromReaderErrorBeforeNewlineWhileSkipping: a read error inside an
// oversized line, before its "\n", must leave SkipFrom at that line's start so
// the next cursor keeps skipping instead of emitting the line's end as a line.
func TestScanFromReaderErrorBeforeNewlineWhileSkipping(t *testing.T) {
	tests := []struct {
		name         string
		in           string
		base         int64
		skipFrom     int64
		wantSkipFrom int64
		wantConsumed int64
		wantEnd      int64
	}{
		{"resumed skip", "xxxxxxxx", 100, 90, 90, 100, 108},
		{"oversized line starts in this call", "ok\n" + "xxxxxxxxxx", 0, -1, 3, 3, 13},
		{"resumed skip from offset zero", "xxxxxxxx", 0, 0, 0, 0, 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := scanFrom(&failingReader{data: tt.in}, tt.base, tt.skipFrom, 4, func([]byte, int64) {})
			if !errors.Is(err, errBoom) {
				t.Fatalf("err = %v, want errBoom", err)
			}
			if res.SkipFrom != tt.wantSkipFrom || res.Consumed != tt.wantConsumed || res.End != tt.wantEnd {
				t.Errorf("SkipFrom/Consumed/End = %d/%d/%d, want %d/%d/%d",
					res.SkipFrom, res.Consumed, res.End, tt.wantSkipFrom, tt.wantConsumed, tt.wantEnd)
			}
			if res.Oversized != 0 {
				t.Errorf("Oversized = %d, want 0 (line not finished)", res.Oversized)
			}
		})
	}
}
