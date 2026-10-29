package agentusage

import (
	"bytes"
	"testing"
)

// Tests added by the review of step 4 (DEVDESK-44): the no-id bound of a
// full read, and what Delta.Empty means.

// A full read (first pass, D4 reset, ParseSession) merges one part per file.
// The merge must not let the pass hold N x maxNoIDEntries entries before
// Apply bounds it: the table the pass hands over is already bounded, and the
// totals are exact and equal to ParseSession's.
func TestFullReadBoundsNoIDEntriesAcrossFiles(t *testing.T) {
	s := newSess(t)
	const perFile = maxNoIDEntries/2 + 1
	const files = 3
	total := perFile * files
	line := append(msgLine("", "m", "end_turn", 1, 0, 0, 0), '\n')
	batch := bytes.Repeat(line, perFile)
	s.put(s.parent(), nil)
	for _, id := range []string{"a1", "a2", "a3"} {
		s.put(s.sub(id), batch)
	}

	a := NewAggregate(s.parent())
	d, err := ReadDelta(a.Cursor())
	if err != nil {
		t.Fatalf("ReadDelta: %v", err)
	}
	if !d.Full() || d.part == nil {
		t.Fatalf("Full=%v part=%v, want a full delta with a table", d.Full(), d.part)
	}
	if d.part.noID > maxNoIDEntries {
		t.Errorf("table handed to Apply holds %d no-id entries, want <= %d (the pass must bound its own merge)",
			d.part.noID, maxNoIDEntries)
	}
	if err := a.Apply(d); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	u := a.Usage()
	var msgs int
	var input int64
	for _, ag := range u.Agents {
		msgs += ag.Messages
		input += ag.Tokens.Input
	}
	if msgs != total || input != int64(total) || u.Diagnostics.NoMessageID != total {
		t.Errorf("Messages=%d Input=%d NoMessageID=%d, want %d each", msgs, input, u.Diagnostics.NoMessageID, total)
	}
	want, err := ParseSession(s.located())
	if err != nil {
		t.Fatalf("ParseSession: %v", err)
	}
	assertSameUsage(t, u, want)
}

// Empty means: no new complete line, and neither the end-of-file state (tail)
// nor the issue state changed. A held-back line that only grows is not news.
func TestDeltaEmptyIgnoresAGrowingTail(t *testing.T) {
	s := newSess(t)
	b := ln("b")
	half := len(b) / 2
	s.put(s.parent(), cat(ln("a"), b[:half]))
	a := NewAggregate(s.parent())
	readApply(t, a)

	s.add(s.parent(), b[half:len(b)-1-5]) // longer, still no newline
	d := readApply(t, a)
	if !d.Empty() || d.Full() {
		t.Errorf("tail grew without a newline: Empty=%v Full=%v, want true and false", d.Empty(), d.Full())
	}
	assertMatchesParse(t, a, s)

	s.add(s.parent(), b[len(b)-1-5:]) // completes the line
	d = readApply(t, a)
	if d.Empty() || d.Full() {
		t.Errorf("tail completed: Empty=%v Full=%v, want false and false", d.Empty(), d.Full())
	}
	assertMatchesParse(t, a, s)
	if got := mainMessages(t, a.Usage()); got != 2 {
		t.Errorf("Messages = %d, want 2", got)
	}
}

// A tail that appears, or disappears without a new complete line, is a state
// change: TruncatedFiles differs.
func TestDeltaNotEmptyWhenTheTailAppears(t *testing.T) {
	s := newSess(t)
	b := ln("b")
	s.put(s.parent(), ln("a"))
	a := NewAggregate(s.parent())
	readApply(t, a)

	s.add(s.parent(), b[:len(b)/2])
	d := readApply(t, a)
	if d.Empty() {
		t.Error("a tail appeared (TruncatedFiles changes): Empty = true, want false")
	}
	assertMatchesParse(t, a, s)
}

// A persistent IssueReadError is news once, not on every tick.
func TestDeltaEmptyWhenAReadErrorPersists(t *testing.T) {
	isolateReadFileHome(t)
	s := newSess(t)
	l := int64(len(ln("a")))
	s.put(s.parent(), cat(ln("a"), bytes.Repeat([]byte("a"), 50)))

	orig := openTranscript
	opens := 0
	swapOpen(t, func(path string) (transcriptFile, error) {
		f, err := orig(path)
		if err != nil {
			return nil, err
		}
		opens++
		limit := int64(10) // later opens start at the offset, 10 bytes into the tail
		if opens == 1 {
			limit = l + 10
		}
		return &faultyFile{transcriptFile: f, limit: limit}, nil // every open fails mid-tail
	})

	a := NewAggregate(s.parent())
	first := readApply(t, a)
	if first.Empty() {
		t.Fatal("first delta: Empty = true, want false")
	}
	if !hasIssue(a.Usage(), incSessUUID+".jsonl", IssueReadError) {
		t.Fatalf("setup: no IssueReadError after the first read: %+v", a.Usage().Diagnostics)
	}
	for i := 2; i <= 3; i++ {
		d := readApply(t, a)
		if !d.Empty() || d.Full() {
			t.Errorf("tick %d with the same read error: Empty=%v Full=%v, want true and false", i, d.Empty(), d.Full())
		}
	}

	openTranscript = orig // the fault goes away: the issue clears, which is news
	d := readApply(t, a)
	if d.Empty() {
		t.Error("the read error cleared: Empty = true, want false")
	}
	assertMatchesParse(t, a, s)
}
