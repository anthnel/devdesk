package agentusage

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// --- helpers -------------------------------------------------------------

// isolateReadFileHome keeps a test away from the real home directory.
func isolateReadFileHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

// probe counts how a test's transcripts are opened and read.
type probe struct {
	opens   int // successful opens
	calls   int // every call of openTranscript, failed or not
	bytes   int64
	perPath map[string]int // calls by path
}

// call records one call of openTranscript.
func (p *probe) call(path string) {
	p.calls++
	if p.perPath == nil {
		p.perPath = map[string]int{}
	}
	p.perPath[path]++
}

// countingFile counts the bytes read through Read and ReadAt.
type countingFile struct {
	transcriptFile
	p *probe
}

func (c countingFile) Read(b []byte) (int, error) {
	n, err := c.transcriptFile.Read(b)
	c.p.bytes += int64(n)
	return n, err
}

func (c countingFile) ReadAt(b []byte, off int64) (int, error) {
	n, err := c.transcriptFile.ReadAt(b, off)
	c.p.bytes += int64(n)
	return n, err
}

// swapOpen replaces openTranscript for the length of the test.
func swapOpen(t *testing.T, fn func(path string) (transcriptFile, error)) {
	t.Helper()
	orig := openTranscript
	openTranscript = fn
	t.Cleanup(func() { openTranscript = orig })
}

// countOpens counts opens and bytes read by readFile.
func countOpens(t *testing.T) *probe {
	t.Helper()
	p := &probe{}
	orig := openTranscript
	swapOpen(t, func(path string) (transcriptFile, error) {
		p.call(path)
		f, err := orig(path)
		if err != nil {
			return nil, err
		}
		p.opens++
		return countingFile{f, p}, nil
	})
	return p
}

var errFault = errors.New("injected read failure")

// faultyFile fails with errFault once limit bytes were delivered through Read.
type faultyFile struct {
	transcriptFile
	limit, n int64
}

func (f *faultyFile) Read(b []byte) (int, error) {
	if f.n >= f.limit {
		return 0, errFault
	}
	if rem := f.limit - f.n; int64(len(b)) > rem {
		b = b[:rem]
	}
	n, err := f.transcriptFile.Read(b)
	f.n += int64(n)
	return n, err
}

// faultFirstOpen makes the first open fail after limit bytes; later opens
// are normal. It returns a counter of opens.
func faultFirstOpen(t *testing.T, limit int64) *probe {
	t.Helper()
	p := &probe{}
	orig := openTranscript
	swapOpen(t, func(path string) (transcriptFile, error) {
		p.call(path)
		f, err := orig(path)
		if err != nil {
			return nil, err
		}
		p.opens++
		if p.opens == 1 {
			return &faultyFile{transcriptFile: f, limit: limit}, nil
		}
		return f, nil
	})
	return p
}

func putFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendFile(t *testing.T, path string, data []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// ln is a valid usage line with its newline.
func ln(id string) []byte {
	return append(msgLine(id, "claude-x", "end_turn", 1, 2, 3, 4), '\n')
}

func cat(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

// cur is the cursor of a parent transcript not read yet.
func cur(path string) fileCursor {
	return newFileCursor(path, sanitizeLabel(filepath.Base(path)), "", parentRank)
}

func pass(fc fileCursor) fileRead { return readFile(fc, time.Now()) }

// step reads once and fails if the read asks for a reset.
func step(t *testing.T, fc fileCursor) fileRead {
	t.Helper()
	fr := pass(fc)
	if fr.reset {
		t.Fatal("unexpected reset")
	}
	return fr
}

func fold(dst, part *table) {
	if part != nil {
		dst.merge(part)
	}
}

// ids returns the message ids held by part, sorted by position.
func partIDs(part *table) []string {
	if part == nil {
		return nil
	}
	var out []string
	for _, k := range part.orderedKeys() {
		out = append(out, k.id)
	}
	return out
}

func offOf(t *testing.T, part *table, id string) int64 {
	t.Helper()
	if part == nil || part.entries[msgKey{id: id}] == nil {
		t.Fatalf("message %q not in the part (ids %v)", id, partIDs(part))
	}
	return part.entries[msgKey{id: id}].last.off
}

// assertMatchesParseSession checks the table folded from every part read
// equals ParseSession on the final file (D3, the oracle of the story).
func assertMatchesParseSession(t *testing.T, path string, tb *table) {
	t.Helper()
	want, err := ParseSession(SessionFiles{ID: "s", Parent: path})
	if err != nil {
		t.Fatal(err)
	}
	agents, diag, versions := tb.finish([]Agent{{Main: true, State: StateUnknown}})
	want.Diagnostics.TruncatedFiles = nil
	if !reflect.DeepEqual(agents, want.Agents) {
		t.Errorf("agents:\n got %+v\nwant %+v", agents, want.Agents)
	}
	if !reflect.DeepEqual(diag, want.Diagnostics) {
		t.Errorf("diagnostics:\n got %+v\nwant %+v", diag, want.Diagnostics)
	}
	if !reflect.DeepEqual(versions, want.Versions) {
		t.Errorf("versions: got %v, want %v", versions, want.Versions)
	}
}

// --- appends -------------------------------------------------------------

// Criterion 2: a 6 MiB transcript grown by one line costs about that line.
func TestReadFileVolumeIsTheAppendedBytes(t *testing.T) {
	isolateReadFileHome(t)
	path := filepath.Join(t.TempDir(), "p.jsonl")
	var buf bytes.Buffer
	for i := 0; buf.Len() < 6<<20; i++ {
		buf.Write(ln(fmt.Sprintf("old-%d", i)))
	}
	putFile(t, path, buf.Bytes())
	p := countOpens(t)

	fr1 := step(t, cur(path))
	if fr1.part == nil || fr1.next.offset != int64(buf.Len()) {
		t.Fatalf("first read: part nil=%v offset=%d, want %d", fr1.part == nil, fr1.next.offset, buf.Len())
	}
	extra := ln("new")
	appendFile(t, path, extra)
	*p = probe{}

	fr2 := step(t, fr1.next)
	if limit := int64(len(extra)) + 1; p.bytes > limit {
		t.Errorf("second read consumed %d bytes, want <= %d", p.bytes, limit)
	}
	if got := partIDs(fr2.part); !reflect.DeepEqual(got, []string{"new"}) {
		t.Errorf("second part ids = %v, want [new]", got)
	}
}

func TestReadFileUnchangedSizeIsNeverOpened(t *testing.T) {
	isolateReadFileHome(t)
	path := filepath.Join(t.TempDir(), "p.jsonl")
	putFile(t, path, cat(ln("a"), ln("b")))
	p := countOpens(t)

	fr1 := step(t, cur(path))
	if p.opens != 1 || p.calls != 1 || fr1.part == nil {
		t.Fatalf("first read: opens=%d calls=%d part nil=%v", p.opens, p.calls, fr1.part == nil)
	}
	later := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	fr2 := step(t, fr1.next)
	if p.opens != 1 || p.calls != 1 {
		t.Errorf("after a quiet pass: opens=%d calls=%d, want 1 and 1 (a call that fails is still a read attempt)", p.opens, p.calls)
	}
	if fr2.part != nil {
		t.Errorf("part of a quiet pass = %v, want nil", partIDs(fr2.part))
	}
	if !fr2.next.modTime.Equal(later) {
		t.Errorf("modTime = %v, want the refreshed %v", fr2.next.modTime, later)
	}
	if fr2.next.offset != fr1.next.offset || fr2.next.size != fr1.next.size {
		t.Errorf("position moved on a quiet pass: %+v -> %+v", fr1.next, fr2.next)
	}
}

func TestReadFileHeldBackLineIsReadAgainOnceComplete(t *testing.T) {
	isolateReadFileHome(t)
	path := filepath.Join(t.TempDir(), "p.jsonl")
	first := ln("a")
	whole := ln("h")
	cut := len(whole) / 2
	putFile(t, path, cat(first, whole[:cut]))

	fr1 := step(t, cur(path))
	if got := partIDs(fr1.part); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("first part ids = %v, want [a]", got)
	}
	if !fr1.next.tail || fr1.next.offset != int64(len(first)) {
		t.Fatalf("tail=%v offset=%d, want true and %d", fr1.next.tail, fr1.next.offset, len(first))
	}
	appendFile(t, path, whole[cut:])
	fr2 := step(t, fr1.next)
	if got := partIDs(fr2.part); !reflect.DeepEqual(got, []string{"h"}) {
		t.Fatalf("second part ids = %v, want [h]", got)
	}
	if off := offOf(t, fr2.part, "h"); off != int64(len(first)) {
		t.Errorf("held-back line counted at %d, want its original offset %d", off, len(first))
	}
	if fr2.next.tail || fr2.next.offset != int64(len(first)+len(whole)) {
		t.Errorf("tail=%v offset=%d, want false and %d", fr2.next.tail, fr2.next.offset, len(first)+len(whole))
	}
}

// Every byte of a line is a possible split point between two passes; CRLF
// terminators and multi-byte runes must not be cut into two readings.
func TestReadFileEverySplitPointCountsTheLineOnce(t *testing.T) {
	isolateReadFileHome(t)
	multibyte := msgLine("m1", "claude-é😀", "end_turn", 5, 6, 7, 8)
	tests := []struct {
		name string
		line []byte
	}{
		{"LF", ln("m1")},
		{"CRLF", cat(msgLine("m1", "claude-x", "end_turn", 5, 6, 7, 8), []byte("\r\n"))},
		{"4-byte rune in the model", cat(multibyte, []byte("\n"))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prefix := ln("m0")
			for k := 1; k < len(tc.line); k++ {
				path := filepath.Join(t.TempDir(), "p.jsonl")
				putFile(t, path, cat(prefix, tc.line[:k]))
				fr1 := step(t, cur(path))
				appendFile(t, path, tc.line[k:])
				fr2 := step(t, fr1.next)

				all := newTable()
				fold(all, fr1.part)
				fold(all, fr2.part)
				if t.Failed() {
					return
				}
				want := 0
				for _, id := range append(partIDs(fr1.part), partIDs(fr2.part)...) {
					if id == "m1" {
						want++
					}
				}
				if want != 1 {
					t.Fatalf("split at %d: m1 seen in %d parts, want 1", k, want)
				}
				assertMatchesParseSession(t, path, all)
				if t.Failed() {
					t.Fatalf("split at %d diverges from a one-shot read", k)
				}
			}
		})
	}
}

// Several passes over a file grown piece by piece equal one full read.
func TestReadFileSuccessivePassesEqualAFullRead(t *testing.T) {
	isolateReadFileHome(t)
	dup := msgLine("a", "claude-y", "end_turn", 9, 9, 9, 9)
	tests := []struct {
		name   string
		chunks [][]byte
	}{
		{"whole lines", [][]byte{cat(ln("a"), ln("b")), ln("c")}},
		{"line split mid-way", [][]byte{cat(ln("a"), ln("b")[:40]), ln("b")[40:], ln("c")}},
		{"message rewritten later", [][]byte{ln("a"), cat(dup, []byte("\n")), ln("b")}},
		{"unreadable line and blank lines", [][]byte{[]byte("not json\n\n"), ln("a"), []byte("{\"x\":\n"), ln("b")}},
		{"no-id lines", [][]byte{cat(msgLine("", "claude-x", "end_turn", 1, 1, 1, 1), []byte("\n")), cat(msgLine("", "claude-x", "", 2, 2, 2, 2), []byte("\n"))}},
		{"ends on a held-back line", [][]byte{ln("a"), ln("b")[:25]}},
		{"empty pass in the middle", [][]byte{ln("a"), nil, ln("b")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "p.jsonl")
			putFile(t, path, nil)
			all := newTable()
			fc := cur(path)
			for i, c := range tc.chunks {
				if i > 0 {
					appendFile(t, path, c)
				} else {
					putFile(t, path, c)
				}
				fr := step(t, fc)
				fold(all, fr.part)
				fc = fr.next
			}
			assertMatchesParseSession(t, path, all)
			want, _ := ParseSession(SessionFiles{ID: "s", Parent: path})
			wantTail := len(want.Diagnostics.TruncatedFiles) > 0
			if fc.tail != wantTail {
				t.Errorf("tail = %v, ParseSession reports truncated = %v", fc.tail, wantTail)
			}
		})
	}
}

// --- oversized lines -----------------------------------------------------

func TestReadFileOversizedLineIsCountedOncePerPass(t *testing.T) {
	isolateReadFileHome(t)
	path := filepath.Join(t.TempDir(), "p.jsonl")
	a := ln("a")
	big := bytes.Repeat([]byte("x"), MaxLineBytes+10)
	putFile(t, path, cat(a, big, []byte("\n"), ln("b")))

	all := newTable()
	fr1 := step(t, cur(path))
	fold(all, fr1.part)
	if fr1.part == nil || fr1.part.oversized != 1 {
		t.Fatalf("first pass oversized = %v, want 1", fr1.part)
	}
	appendFile(t, path, ln("c"))
	fr2 := step(t, fr1.next)
	fold(all, fr2.part)
	if fr2.part == nil || fr2.part.oversized != 0 {
		t.Errorf("second pass recounted the oversized line: %+v", fr2.part)
	}
	fr3 := step(t, fr2.next)
	if fr3.part != nil {
		t.Errorf("a quiet third pass returned a part: %v", partIDs(fr3.part))
	}
	_, diag, _ := all.finish([]Agent{{Main: true}})
	if diag.OversizedLines != 1 {
		t.Errorf("OversizedLines = %d, want 1", diag.OversizedLines)
	}
	if len(diag.Files) != 1 || diag.Files[0].Kind != IssueOversized || diag.Files[0].Offset != int64(len(a)) {
		t.Errorf("Files = %+v, want one oversized at offset %d", diag.Files, len(a))
	}
	assertMatchesParseSession(t, path, all)
}

// An oversized line still being written is resumed in skip mode (D2): never
// counted early, never re-read from its start, counted once when it ends.
func TestReadFileOversizedLineStillBeingWritten(t *testing.T) {
	isolateReadFileHome(t)
	path := filepath.Join(t.TempDir(), "p.jsonl")
	a := ln("a")
	putFile(t, path, cat(a, bytes.Repeat([]byte("x"), MaxLineBytes+100)))
	p := countOpens(t)

	all := newTable()
	fr1 := step(t, cur(path))
	fold(all, fr1.part)
	if fr1.next.skipFrom != int64(len(a)) {
		t.Fatalf("skipFrom = %d, want %d", fr1.next.skipFrom, len(a))
	}
	if fr1.part == nil || fr1.part.oversized != 0 || fr1.next.tail {
		t.Fatalf("in-progress line must be neither counted nor a tail: part=%+v tail=%v", fr1.part, fr1.next.tail)
	}
	if fr1.next.offset != int64(len(a)) {
		t.Fatalf("offset = %d, want %d (just past the last complete line)", fr1.next.offset, len(a))
	}

	more := bytes.Repeat([]byte("x"), 100)
	appendFile(t, path, more)
	*p = probe{}
	fr2 := step(t, fr1.next)
	fold(all, fr2.part)
	if limit := int64(len(more)) + 1; p.bytes > limit {
		t.Errorf("resume consumed %d bytes, want <= %d (only the new ones)", p.bytes, limit)
	}
	if fr2.next.skipFrom != int64(len(a)) || (fr2.part != nil && fr2.part.oversized != 0) {
		t.Errorf("still unfinished: skipFrom=%d part=%+v", fr2.next.skipFrom, fr2.part)
	}

	appendFile(t, path, cat([]byte("\n"), ln("b")))
	fr3 := step(t, fr2.next)
	fold(all, fr3.part)
	if fr3.part == nil || fr3.part.oversized != 1 {
		t.Fatalf("ended line: part = %+v, want one oversized", fr3.part)
	}
	if fr3.next.skipFrom != -1 || fr3.next.tail {
		t.Errorf("skipFrom=%d tail=%v after the line ended, want -1 and false", fr3.next.skipFrom, fr3.next.tail)
	}
	_, diag, _ := all.finish([]Agent{{Main: true}})
	if len(diag.Files) != 1 || diag.Files[0].Offset != int64(len(a)) {
		t.Errorf("Files = %+v, want one oversized at its start %d", diag.Files, len(a))
	}
	assertMatchesParseSession(t, path, all)
}

// --- not an append: reset (D4) ------------------------------------------

func TestReadFileNotAnAppendResets(t *testing.T) {
	isolateReadFileHome(t)
	initial := cat(ln("a"), ln("b"), ln("c"))
	tests := []struct {
		name   string
		mutate func(t *testing.T, path string)
	}{
		{"truncated shorter", func(t *testing.T, path string) { putFile(t, path, ln("a")) }},
		{"truncated then regrown past the offset", func(t *testing.T, path string) {
			putFile(t, path, bytes.Repeat([]byte("x"), 2*len(initial)))
		}},
		{"rotated, same size", func(t *testing.T, path string) {
			other := strings.NewReplacer(`"id":"a"`, `"id":"x"`, `"id":"b"`, `"id":"y"`, `"id":"c"`, `"id":"z"`).Replace(string(initial))
			if len(other) != len(initial) {
				t.Fatal("setup: replacement changed the size")
			}
			tmp := path + ".new"
			putFile(t, tmp, []byte(other))
			if err := os.Rename(tmp, path); err != nil {
				t.Fatal(err)
			}
		}},
		{"rotated, larger", func(t *testing.T, path string) {
			tmp := path + ".new"
			putFile(t, tmp, cat(initial, ln("d")))
			if err := os.Rename(tmp, path); err != nil {
				t.Fatal(err)
			}
		}},
		{"deleted", func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}},
		{"replaced by a link, same content", func(t *testing.T, path string) {
			target := path + ".target"
			putFile(t, target, initial)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
		}},
		{"appended but no longer openable", func(t *testing.T, path string) {
			appendFile(t, path, ln("d"))
			swapOpen(t, func(string) (transcriptFile, error) { return nil, os.ErrPermission })
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "p.jsonl")
			putFile(t, path, initial)
			fr1 := step(t, cur(path))
			if fr1.next.id == nil || fr1.next.offset != int64(len(initial)) {
				t.Fatalf("setup: id nil=%v offset=%d", fr1.next.id == nil, fr1.next.offset)
			}
			tc.mutate(t, path)
			if fr2 := pass(fr1.next); !fr2.reset {
				t.Errorf("reset = false, want true (part %v)", partIDs(fr2.part))
			}
		})
	}
}

// Nothing was read yet: a missing or unusable path is a state, not a reset.
func TestReadFileFreshCursorOnAnUnusablePath(t *testing.T) {
	isolateReadFileHome(t)
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string) string
	}{
		{"missing", func(t *testing.T, dir string) string { return filepath.Join(dir, "p.jsonl") }},
		{"directory", func(t *testing.T, dir string) string {
			p := filepath.Join(dir, "p.jsonl")
			if err := os.Mkdir(p, 0o700); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{"symbolic link to a real transcript", func(t *testing.T, dir string) string {
			target := filepath.Join(dir, "real.jsonl")
			putFile(t, target, ln("a"))
			p := filepath.Join(dir, "p.jsonl")
			if err := os.Symlink(target, p); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return p
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.setup(t, t.TempDir())
			fr := pass(cur(path))
			if fr.reset {
				t.Fatal("reset = true, want a state issue")
			}
			if !fr.next.hasIssue || fr.next.issue != IssueOpenFailed {
				t.Errorf("issue = %v (has %v), want IssueOpenFailed", fr.next.issue, fr.next.hasIssue)
			}
			if fr.part != nil && len(fr.part.entries) > 0 {
				t.Errorf("read something from an unusable path: %v", partIDs(fr.part))
			}
			if fr.next.offset != 0 || fr.next.id != nil {
				t.Errorf("cursor moved: offset=%d id nil=%v", fr.next.offset, fr.next.id == nil)
			}
		})
	}
}

// --- D6: errors are states, cleared by a later success --------------------

func TestReadFileOpenFailureClearsOnALaterSuccess(t *testing.T) {
	isolateReadFileHome(t)
	path := filepath.Join(t.TempDir(), "p.jsonl")
	fr1 := step(t, cur(path))
	if !fr1.next.hasIssue || fr1.next.issue != IssueOpenFailed {
		t.Fatalf("missing file: issue = %v (has %v)", fr1.next.issue, fr1.next.hasIssue)
	}
	putFile(t, path, cat(ln("a"), ln("b")))
	fr2 := step(t, fr1.next)
	if fr2.next.hasIssue {
		t.Errorf("issue %v still set after a successful read", fr2.next.issue)
	}
	if got := partIDs(fr2.part); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("ids = %v, want [a b]", got)
	}
}

func TestReadFileOpenErrorOnAFreshCursorIsAStateThenClears(t *testing.T) {
	isolateReadFileHome(t)
	path := filepath.Join(t.TempDir(), "p.jsonl")
	putFile(t, path, ln("a"))
	orig := openTranscript
	swapOpen(t, func(string) (transcriptFile, error) { return nil, os.ErrPermission })

	fr1 := pass(cur(path))
	if fr1.reset || !fr1.next.hasIssue || fr1.next.issue != IssueOpenFailed {
		t.Fatalf("reset=%v issue=%v (has %v), want a state IssueOpenFailed", fr1.reset, fr1.next.issue, fr1.next.hasIssue)
	}
	openTranscript = orig
	fr2 := step(t, fr1.next)
	if fr2.next.hasIssue || !reflect.DeepEqual(partIDs(fr2.part), []string{"a"}) {
		t.Errorf("after recovery: issue=%v ids=%v", fr2.next.hasIssue, partIDs(fr2.part))
	}
}

// A read error keeps the lines before it and is a state; the bytes between
// Consumed and End must be read again even when End is the file's size.
func TestReadFileReadErrorIsAStateAndBytesAreReadAgain(t *testing.T) {
	isolateReadFileHome(t)
	l := int64(len(ln("a")))
	tests := []struct {
		name     string
		content  []byte
		limit    int64
		consumed int64
		first    []string
		second   []string
		tail     bool
	}{
		{
			name:     "error after the whole file, on a held-back tail",
			content:  cat(ln("a"), ln("b"), bytes.Repeat([]byte("a"), 50)),
			limit:    2*l + 50,
			consumed: 2 * l,
			first:    []string{"a", "b"},
			second:   nil,
			tail:     true,
		},
		{
			name:     "error in the middle of the third line",
			content:  cat(ln("a"), ln("b"), ln("c")),
			limit:    2*l + l/2,
			consumed: 2 * l,
			first:    []string{"a", "b"},
			second:   []string{"c"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "p.jsonl")
			putFile(t, path, tc.content)
			p := faultFirstOpen(t, tc.limit)

			all := newTable()
			fr1 := step(t, cur(path))
			fold(all, fr1.part)
			if !reflect.DeepEqual(partIDs(fr1.part), tc.first) {
				t.Fatalf("first ids = %v, want %v", partIDs(fr1.part), tc.first)
			}
			if !fr1.next.hasIssue || fr1.next.issue != IssueReadError || fr1.next.issueOff != tc.consumed {
				t.Fatalf("issue = %v at %d (has %v), want IssueReadError at %d",
					fr1.next.issue, fr1.next.issueOff, fr1.next.hasIssue, tc.consumed)
			}
			if fr1.next.offset != tc.consumed {
				t.Fatalf("offset = %d, want Consumed %d", fr1.next.offset, tc.consumed)
			}

			fr2 := step(t, fr1.next)
			fold(all, fr2.part)
			if p.opens != 2 {
				t.Errorf("opens = %d, want 2: the file must be reopened after a read error", p.opens)
			}
			if fr2.next.hasIssue {
				t.Errorf("issue %v still set after a clean read", fr2.next.issue)
			}
			if got := partIDs(fr2.part); !reflect.DeepEqual(got, tc.second) {
				t.Errorf("second ids = %v, want %v (nothing counted twice)", got, tc.second)
			}
			if fr2.next.tail != tc.tail {
				t.Errorf("tail = %v, want %v", fr2.next.tail, tc.tail)
			}
			assertMatchesParseSession(t, path, all)
		})
	}
}

// TruncatedFiles is a state too: it follows whether a tail exists right now.
func TestReadFileTailFlagFollowsTheFileNow(t *testing.T) {
	isolateReadFileHome(t)
	path := filepath.Join(t.TempDir(), "p.jsonl")
	whole := ln("b")
	putFile(t, path, cat(ln("a"), whole[:30]))
	fr1 := step(t, cur(path))
	if !fr1.next.tail {
		t.Fatal("tail = false with an incomplete line")
	}
	appendFile(t, path, whole[30:])
	if fr2 := step(t, fr1.next); fr2.next.tail {
		t.Error("tail still true once the line is complete")
	}
}

// --- clock ----------------------------------------------------------------

func TestReadFileClampsModTimeToNow(t *testing.T) {
	isolateReadFileHome(t)
	path := filepath.Join(t.TempDir(), "p.jsonl")
	putFile(t, path, ln("a"))
	now := time.Now().Truncate(time.Second)
	future := now.Add(time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	fr := readFile(cur(path), now)
	if !fr.next.modTime.Equal(now) {
		t.Errorf("modTime = %v, want clamped to now %v", fr.next.modTime, now)
	}

	past := now.Add(-time.Hour)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	if fr := readFile(cur(path), now); !fr.next.modTime.Equal(past) {
		t.Errorf("modTime = %v, want the unclamped %v", fr.next.modTime, past)
	}
}

// --- failures after the open: Stat, Seek ----------------------------------

var errInjected = errors.New("injected handle failure")

// statFailFile fails Stat, the first call the handle gets after the open.
type statFailFile struct{ transcriptFile }

func (statFailFile) Stat() (os.FileInfo, error) { return nil, errInjected }

// seekFailFile fails Seek, the call that positions the read.
type seekFailFile struct{ transcriptFile }

func (seekFailFile) Seek(int64, int) (int64, error) { return 0, errInjected }

// A handle whose Stat or Seek fails is a failed read of this file: state
// IssueOpenFailed on a cursor that never read, a reset on one that did (D4),
// and in neither case a part or a moved position.
func TestReadFileHandleFailures(t *testing.T) {
	isolateReadFileHome(t)
	tests := []struct {
		name string
		wrap func(transcriptFile) transcriptFile
	}{
		{"Stat fails", func(f transcriptFile) transcriptFile { return statFailFile{f} }},
		{"Seek fails", func(f transcriptFile) transcriptFile { return seekFailFile{f} }},
	}
	for _, tc := range tests {
		t.Run(tc.name+" on a fresh cursor", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "p.jsonl")
			putFile(t, path, cat(ln("a"), ln("b")))
			orig := openTranscript
			swapOpen(t, func(path string) (transcriptFile, error) {
				f, err := orig(path)
				if err != nil {
					return nil, err
				}
				return tc.wrap(f), nil
			})
			fr := pass(cur(path))
			if fr.reset || !fr.next.hasIssue || fr.next.issue != IssueOpenFailed {
				t.Fatalf("reset=%v issue=%v (has %v), want a state IssueOpenFailed", fr.reset, fr.next.issue, fr.next.hasIssue)
			}
			if fr.part != nil && len(fr.part.entries) > 0 {
				t.Errorf("read something through a failing handle: %v", partIDs(fr.part))
			}
			if fr.next.offset != 0 || fr.next.id != nil {
				t.Errorf("cursor moved: offset=%d id nil=%v", fr.next.offset, fr.next.id == nil)
			}
		})
		t.Run(tc.name+" on a cursor that read", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "p.jsonl")
			putFile(t, path, ln("a"))
			fr1 := step(t, cur(path))
			appendFile(t, path, ln("b"))
			orig := openTranscript
			swapOpen(t, func(path string) (transcriptFile, error) {
				f, err := orig(path)
				if err != nil {
					return nil, err
				}
				return tc.wrap(f), nil
			})
			if fr2 := pass(fr1.next); !fr2.reset {
				t.Errorf("reset = false, want true (part %v)", partIDs(fr2.part))
			}
		})
	}
}

// --- TOCTOU: the handle is not the file Lstat saw --------------------------

// A file swapped in between the Lstat and the open is another file: its
// identity differs from the one the cursor read, so the read is not an append
// even when the guard byte before the offset happens to be a newline.
func TestReadFileHandleOfAnotherFileResets(t *testing.T) {
	isolateReadFileHome(t)
	initial := cat(ln("a"), ln("b"))
	tests := []struct {
		name  string
		other func() []byte
	}{
		{"larger, same prefix", func() []byte { return cat(initial, ln("c")) }},
		{"larger, different content, newline at the guard byte", func() []byte {
			return cat(ln("x"), ln("y"), ln("z"))
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "p.jsonl")
			elsewhere := filepath.Join(dir, "other.jsonl")
			putFile(t, path, initial)
			fr1 := step(t, cur(path))
			if fr1.next.id == nil || fr1.next.offset != int64(len(initial)) {
				t.Fatalf("setup: id nil=%v offset=%d", fr1.next.id == nil, fr1.next.offset)
			}
			appendFile(t, path, ln("c")) // Lstat sees a grown file: not quiet, so it is opened
			putFile(t, elsewhere, tc.other())
			orig := openTranscript
			p := &probe{}
			swapOpen(t, func(string) (transcriptFile, error) {
				p.call(path)
				return orig(elsewhere)
			})
			fr2 := pass(fr1.next)
			if p.calls != 1 {
				t.Fatalf("calls = %d, want 1: the setup must reach the open", p.calls)
			}
			if !fr2.reset {
				t.Errorf("reset = false, want true (part %v): a handle of another file was read as an append", partIDs(fr2.part))
			}
		})
	}
}
