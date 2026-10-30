package agentusage

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

// fileCursor is the read position of one transcript. Values only: no pointer
// except the immutable os.FileInfo.
type fileCursor struct {
	path     string      // full path, never logged
	name     string      // sanitized base name (FileIssue.File, TruncatedFiles)
	agent    string      // key of the agent in a table: "" for the parent, else rank
	label    string      // sanitized agent ID shown to the user; "" for the parent
	rank     string      // position rank of the file (parentRank or transcriptRank)
	id       os.FileInfo // from the open handle's Stat at the last read; nil before
	offset   int64       // just past the last complete line folded in
	size     int64       // file size at the last read
	modTime  time.Time   // Lstat mtime at the last listing
	skipFrom int64       // start of an oversized line still being written, or -1
	tail     bool        // an incomplete line follows offset (TruncatedFiles)
	issue    IssueKind   // state issue of the last read (D6), valid if hasIssue
	issueOff int64
	hasIssue bool
}

// newFileCursor returns the cursor of a transcript not read yet.
func newFileCursor(path, name, agent, rank string) fileCursor {
	return fileCursor{path: path, name: name, agent: agent, rank: rank, skipFrom: -1}
}

// transcriptFile is what readFile needs from an open transcript; *os.File
// satisfies it.
type transcriptFile interface {
	io.Reader
	io.ReaderAt
	io.Seeker
	io.Closer
	Stat() (os.FileInfo, error)
}

// openTranscript opens a transcript through openPlain. A var so tests can
// count opens and bytes read.
var openTranscript = func(path string) (transcriptFile, error) {
	f, err := openPlain(path, false)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, errNotRegular
	}
	return f, nil
}

// fileRead is the outcome of reading one file from its cursor.
type fileRead struct {
	next  fileCursor
	part  *table // lines folded from the new bytes; nil when nothing was read
	reset bool   // not an append (D4): the caller restarts the whole session
	err   error  // why the file could not be opened or read at all, if so
}

// read reports whether the cursor has already read part of its file.
func (fc fileCursor) read() bool { return fc.offset > 0 || fc.skipFrom >= 0 || fc.id != nil }

// quiet reports whether the file is, as far as Lstat can tell, exactly what the
// last read saw. A cursor holding a read error never is: the bytes between
// its offset and the failure must be read again.
func (fc fileCursor) quiet(li os.FileInfo) bool {
	if fc.id == nil || (fc.hasIssue && fc.issue == IssueReadError) {
		return false
	}
	return li.Size() == fc.size && os.SameFile(li, fc.id)
}

// failRead is the outcome of a path that cannot be read: a reset when the file
// was read before (D4), else a state issue cleared by the next success (D6).
func failRead(fc fileCursor, err error) fileRead {
	if fc.read() {
		return fileRead{next: fc, reset: true, err: err}
	}
	fc.issue, fc.issueOff, fc.hasIssue = IssueOpenFailed, 0, true
	return fileRead{next: fc, err: err}
}

// isAppend reports whether the open file is the one fc read, only grown (D4).
func (fc fileCursor) isAppend(f transcriptFile, fi os.FileInfo) bool {
	if (fc.id != nil && !os.SameFile(fi, fc.id)) || fi.Size() < fc.size {
		return false
	}
	if fc.offset == 0 {
		return true
	}
	var b [1]byte
	n, _ := f.ReadAt(b[:], fc.offset-1)
	return n == 1 && b[0] == '\n'
}

// readFile reads what was appended to fc's file since fc. now clamps mtimes.
// It never follows a link and never opens a non-regular file.
func readFile(fc fileCursor, now time.Time) fileRead {
	li, err := os.Lstat(fc.path)
	if err != nil {
		return failRead(fc, err)
	}
	if !li.Mode().IsRegular() {
		return failRead(fc, errNotRegular)
	}
	fc.modTime = li.ModTime()
	if fc.modTime.After(now) {
		fc.modTime = now
	}
	if fc.quiet(li) {
		return fileRead{next: fc}
	}
	f, err := openTranscript(fc.path)
	if err != nil {
		return failRead(fc, err)
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return failRead(fc, err)
	}
	if !fc.isAppend(f, fi) {
		return fileRead{next: fc, reset: true}
	}
	return fc.scan(f, fi)
}

// scan folds the lines past the cursor into a new table and returns the
// advanced cursor. A line held back is read again next time (D1); an
// oversized one still being written is resumed in skip mode (D2).
func (fc fileCursor) scan(f transcriptFile, fi os.FileInfo) fileRead {
	start := fc.offset
	if fc.skipFrom >= 0 {
		start = fc.size
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return failRead(fc, err)
	}
	part := newTable()
	res, err := scanFrom(f, start, fc.skipFrom, MaxLineBytes, func(line []byte, off int64) {
		part.addLine(fc.agent, fc.name, pos{rank: fc.rank, off: off}, line)
	})
	for _, off := range res.OversizedAt {
		part.addOversized(fc.name, pos{rank: fc.rank, off: off})
	}
	next := fc
	next.id, next.size, next.skipFrom = fi, res.End, res.SkipFrom
	next.offset, next.tail = res.Consumed, len(res.Tail) > 0
	if fc.skipFrom >= 0 && res.Consumed == start {
		next.offset = fc.offset // the oversized line has not ended yet
	}
	next.issue, next.issueOff, next.hasIssue = 0, 0, false
	if err != nil {
		next.issue, next.issueOff, next.hasIssue = IssueReadError, next.offset, true
	}
	return fileRead{next: next, part: part}
}

// ErrStaleDelta is returned by Apply for a delta that was not read from the
// aggregate's current cursor. The aggregate is unchanged; read again from
// Cursor().
var ErrStaleDelta = errors.New("agentusage: delta does not follow the aggregate's cursor")

// readMetaFile reads one meta file; a var so tests can count meta reads (D7).
// The session reader goes through it.
var readMetaFile = readMeta

// timeNow is time.Now; a var for tests.
var timeNow = time.Now

// aggregateIDs hands out the identity of each Aggregate (D9).
var aggregateIDs atomic.Uint64

// metaCursor is the last listing of one sub-agent's meta file and what was
// read from it. Values only, so a cursor stays a plain copyable value.
type metaCursor struct {
	rank       string // rank of the sub-agent's transcript
	path       string // "" when absent
	size       int64
	modTime    time.Time
	unreadable bool
	typ        string
	state      State
	depth      int
}

// sameResult reports whether o tells the same story as m: Lstat details such
// as the mtime are not part of it.
func (m metaCursor) sameResult(o metaCursor) bool {
	return m.rank == o.rank && (m.path == "") == (o.path == "") && m.unreadable == o.unreadable &&
		m.typ == o.typ && m.state == o.state && m.depth == o.depth
}

// unchangedSince reports whether the file Lstat described by fi is the one m
// was read from.
func (m metaCursor) unchangedSince(path string, fi os.FileInfo) bool {
	return fi != nil && m.path == path && m.size == fi.Size() && m.modTime.Equal(fi.ModTime())
}

// Cursor records how far each file of one session has been read. It is a
// plain value: copying it is safe and ReadDelta never mutates it. It holds
// full paths, so it must never be logged; String and GoString redact them.
type Cursor struct {
	parent string // parent transcript path
	id     uint64 // identity of the aggregate it came from
	seq    uint64 // number of deltas applied when it was taken
	fresh  bool   // nothing applied yet, or rewound: the next delta is Full
	files  []fileCursor
	metas  []metaCursor
}

// Rewound returns c with every position cleared, keeping its identity: the
// next ReadDelta reads every file from the start and returns a Full delta.
func (c Cursor) Rewound() Cursor {
	files := make([]fileCursor, len(c.files))
	for i, fc := range c.files {
		files[i] = newFileCursor(fc.path, fc.name, fc.agent, fc.rank)
		files[i].label = fc.label
	}
	c.files, c.metas, c.fresh = files, nil, true
	return c
}

// String redacts the paths the cursor holds.
func (c Cursor) String() string { return fmt.Sprintf("agentusage.Cursor{files: %d}", len(c.files)) }

// GoString redacts the paths the cursor holds.
func (c Cursor) GoString() string { return c.String() }

// Delta is what ReadDelta found since a Cursor: the new complete lines of each
// file, already deduplicated, the files that appeared, the meta files that
// changed, and the positions to resume from. Paths live in unexported fields
// and are never printed.
type Delta struct {
	valid     bool   // false for the zero Delta, which was never read
	id, seq   uint64 // the cursor it was read from
	full      bool
	changed   bool
	sessionID string
	files     []fileCursor
	metas     []metaCursor
	part      *table
	parentErr error     // why the parent could not be opened, for ParseSession
	read      time.Time // when it was read: mtimes are clamped to it
}

// Full reports whether the delta was read from the start of every file (first
// read, Rewound cursor, or a change that was not an append).
func (d Delta) Full() bool { return d.full }

// Empty reports whether nothing changed: no new complete line, no file added
// or gone, no meta change, no change of a file's end-of-file state (an
// incomplete last line appearing or disappearing) or of its read issue. A
// held-back line that only grows, a persistent issue, or an mtime that alone
// moved do not count.
func (d Delta) Empty() bool { return !d.changed }

// String shows counts only.
func (d Delta) String() string {
	return fmt.Sprintf("agentusage.Delta{full: %t, empty: %t, files: %d}", d.full, d.Empty(), len(d.files))
}

// GoString shows counts only.
func (d Delta) GoString() string { return d.String() }

// Aggregate is a session's usage built from successive deltas. It is not safe
// for concurrent use: one goroutine (Bubble Tea's Update) owns it.
type Aggregate struct {
	parent    string
	id, seq   uint64
	fresh     bool
	sessionID string
	files     []fileCursor // parent first, then sub-agents by rank
	metas     []metaCursor
	t         *table
	lastMod   time.Time // newest mtime seen by the last non-empty delta, see Activity
}

// String shows counts only: an Aggregate holds paths, and the value receiver
// also covers a dereferenced *a.
func (a Aggregate) String() string {
	return fmt.Sprintf("agentusage.Aggregate{files: %d, applied: %d}", len(a.files), a.seq)
}

// GoString shows counts only.
func (a Aggregate) GoString() string { return a.String() }

// NewAggregate starts an empty aggregate for the session whose parent
// transcript is parentTranscript (<dir>/<uuid>.jsonl). It does no I/O.
func NewAggregate(parentTranscript string) *Aggregate {
	base := filepath.Base(parentTranscript)
	return &Aggregate{
		parent:    parentTranscript,
		id:        aggregateIDs.Add(1),
		fresh:     true,
		sessionID: sanitizeLabel(strings.TrimSuffix(base, transcriptExt)),
		files:     []fileCursor{newFileCursor(parentTranscript, sanitizeLabel(base), "", parentRank)},
		t:         newTable(),
	}
}

// Cursor returns a copy of the aggregate's position, to hand to ReadDelta.
func (a *Aggregate) Cursor() Cursor {
	return Cursor{
		parent: a.parent, id: a.id, seq: a.seq, fresh: a.fresh,
		files: slices.Clone(a.files), metas: slices.Clone(a.metas),
	}
}

// Apply folds d in, or replaces everything with it when d.Full(). It returns
// ErrStaleDelta, and changes nothing, if d was not read from a.Cursor(). It
// takes ownership of d.
func (a *Aggregate) Apply(d Delta) error {
	if !d.valid || d.id != a.id || d.seq != a.seq {
		return ErrStaleDelta
	}
	switch {
	case d.full:
		a.t = boundedTable(d.part)
	case d.part != nil:
		a.t.mergeCapped(d.part)
	}
	a.files, a.metas, a.sessionID = d.files, d.metas, d.sessionID
	if d.changed {
		if n := newestMod(d); n.After(a.lastMod) {
			a.lastMod = n // never moves back when the newest file disappears
		}
	}
	a.fresh = false
	a.seq++
	return nil
}

// boundedTable returns part, or a copy of it holding at most maxNoIDEntries
// entries without a message id.
func boundedTable(part *table) *table {
	if part == nil {
		return newTable()
	}
	if part.noID <= maxNoIDEntries {
		return part
	}
	t := newTable()
	t.mergeCapped(part)
	return t
}

// Usage returns the session's usage so far, same shape and rules as
// ParseSession; the result shares no memory with the aggregate.
func (a *Aggregate) Usage() Usage {
	agents := []Agent{{Main: true, State: StateUnknown}}
	var diag Diagnostics
	var truncated []string
	var issues []issueAt
	metaOf := make(map[string]metaCursor, len(a.metas))
	for _, m := range a.metas {
		metaOf[m.rank] = m
	}
	for _, fc := range a.files {
		if fc.tail {
			truncated = append(truncated, fc.name)
		}
		if fc.hasIssue {
			fi := FileIssue{File: fc.name, Offset: fc.issueOff, Kind: fc.issue}
			issues = append(issues, issueAt{p: pos{rank: fc.rank, off: fc.issueOff}, fi: fi})
		}
		if fc.rank != parentRank {
			agents = append(agents, subAgent(fc, metaOf[fc.rank], &diag))
		}
	}
	agents, diag2, versions := a.t.finishWith(agents, issues)
	diag2.MetaMissing, diag2.MetaUnreadable, diag2.TruncatedFiles = diag.MetaMissing, diag.MetaUnreadable, truncated
	return Usage{SessionID: a.sessionID, Agents: agents, Versions: versions, Diagnostics: diag2, Billed: a.t.billedFor(a.sessionID)}
}

// subAgent builds a sub-agent's identity from its meta cursor, counting a
// missing or unreadable meta file in diag.
func subAgent(fc fileCursor, m metaCursor, diag *Diagnostics) Agent {
	ag := Agent{ID: fc.label, key: fc.rank}
	switch {
	case m.path == "":
		diag.MetaMissing++
	case m.unreadable:
		diag.MetaUnreadable++
	default:
		ag.Type, ag.State, ag.SpawnDepth = m.typ, m.state, m.depth
	}
	return ag
}

// ReadDelta lists the session again (parent, subagents/, meta files, Lstat
// only) and reads what was appended since c. Files whose size did not change
// are not opened. It fails only if the session cannot be listed; errors name
// base names only, and on error there is nothing to apply. A parent that is
// missing, a link or not a regular file is an error. Only a failure to open a
// listed file (for example chmod 000), or a removal between the Lstat and the
// open, yields an IssueOpenFailed without an error (Full at tick 0, then Empty
// while it persists), which the caller can detect through FileIssue.
//
// When ReadDelta returns an error the caller must not treat the session as
// Settled: Activity keeps its last state and says nothing about the failure.
//
// Declared oracle gap (meta files): a meta file that cannot be read (chmod 000)
// counts as MetaUnreadable here, whereas ParseSession(LocateSession) counts it
// MetaMissing. Meta files are compared by Lstat size and mtime (D7), so an
// unreadable meta stays so after chmod +r until it is rewritten.
func ReadDelta(c Cursor) (Delta, error) {
	files, infos, err := listForPoll(c.parent)
	if err != nil {
		return Delta{}, err
	}
	return readSession(files, infos, c, timeNow()), nil
}

// listForPoll lists the session like LocateSession, with meta presence from
// Lstat only (D7), and returns the Lstat result of each meta file found.
func listForPoll(parent string) (SessionFiles, map[string]os.FileInfo, error) {
	infos := map[string]os.FileInfo{}
	files, err := locateSession(parent, func(path string) bool {
		fi, err := os.Lstat(path)
		if err != nil || !fi.Mode().IsRegular() {
			return false
		}
		infos[path] = fi
		return true
	})
	return files, infos, err
}

// target is one transcript of a listing, with its meta file ("" if absent).
type target struct {
	fc   fileCursor
	meta string
}

// sessionTargets turns a listing into fresh cursors: the parent first, then
// the sub-agents in rank order, which is the order ParseSession reads them.
func sessionTargets(files SessionFiles) []target {
	out := []target{{fc: newFileCursor(files.Parent, sanitizeLabel(filepath.Base(files.Parent)), "", parentRank)}}
	for _, sa := range files.SubAgents {
		id := sanitizeLabel(sa.ID)
		name := sanitizeLabel(filepath.Base(sa.Transcript))
		rank := transcriptRank(id, name, sa.ID, sa.Transcript)
		fc := newFileCursor(sa.Transcript, name, rank, rank)
		fc.label = id
		out = append(out, target{fc: fc, meta: sa.Meta})
	}
	slices.SortFunc(out[1:], func(a, b target) int { return cmp.Compare(a.fc.rank, b.fc.rank) })
	return out
}

// readSession reads the files of a listing from c. A change that is not an
// append (D4) restarts every file from the start and the delta is Full.
// ParseSession passes a fresh cursor and no meta Lstat information.
func readSession(files SessionFiles, infos map[string]os.FileInfo, c Cursor, now time.Time) Delta {
	targets := sessionTargets(files)
	full := c.fresh
	p, ok := readTargets(targets, c, now)
	if !ok {
		c, full = c.Rewound(), true
		// ok is true by construction: a rewound cursor knows no file, so no
		// file can ask for another restart.
		p, _ = readTargets(targets, c, now)
	}
	metas, metaChanged := readMetas(targets[1:], infos, c)
	return Delta{
		valid: true, id: c.id, seq: c.seq, full: full,
		changed:   full || p.changed || metaChanged,
		sessionID: sanitizeLabel(files.ID),
		files:     p.files, metas: metas, part: p.part, parentErr: p.parentErr, read: now,
	}
}

// sessionPass is the outcome of one pass over the transcripts.
type sessionPass struct {
	files     []fileCursor
	part      *table
	changed   bool
	parentErr error
}

// readTargets reads every target from its cursor in c, or from the start when
// c does not know it. ok is false when a file asks for a restart (D4): a
// file read before is gone, or was not simply appended to.
func readTargets(targets []target, c Cursor, now time.Time) (sessionPass, bool) {
	prior := make(map[string]fileCursor, len(c.files))
	for _, fc := range c.files {
		prior[fc.rank] = fc
	}
	var p sessionPass
	for _, tg := range targets {
		fc, had := prior[tg.fc.rank]
		delete(prior, tg.fc.rank)
		if !had {
			fc, p.changed = tg.fc, true
		}
		fr := readFile(fc, now)
		if fr.reset {
			return sessionPass{}, false
		}
		if fr.next.rank == parentRank && fr.err != nil {
			p.parentErr = fr.err
		}
		p.changed = p.changed || fr.part.hasLines() || fc.issueState() != fr.next.issueState() ||
			fc.tail != fr.next.tail
		p.files = append(p.files, fr.next)
		p.addPart(fr.part)
	}
	for _, gone := range prior {
		if gone.read() {
			return sessionPass{}, false
		}
		p.changed = true
	}
	return p, true
}

// addPart folds the lines of one file into the pass, spilling no-id lines past
// maxNoIDEntries like a sequential read does (totals stay exact).
func (p *sessionPass) addPart(part *table) {
	switch {
	case part == nil:
	case p.part == nil:
		p.part = part
	default:
		p.part.mergeCapped(part) // keeps the pass within the no-id bound across files
	}
}

// issueState is what a cursor says about its last read, for comparisons.
type issueState struct {
	has  bool
	kind IssueKind
	off  int64
}

func (fc fileCursor) issueState() issueState {
	return issueState{has: fc.hasIssue, kind: fc.issue, off: fc.issueOff}
}

// readMetas returns the meta cursors of the sub-agents in targets, reading a
// meta file only when its Lstat size or mtime differs from c's (D7), and
// whether any of them tells something new. An unreadable meta file is
// MetaUnreadable here but MetaMissing for ParseSession(LocateSession): a
// declared oracle gap, see ReadDelta.
func readMetas(subs []target, infos map[string]os.FileInfo, c Cursor) ([]metaCursor, bool) {
	prior := make(map[string]metaCursor, len(c.metas))
	for _, m := range c.metas {
		prior[m.rank] = m
	}
	out := make([]metaCursor, 0, len(subs))
	changed := false
	for _, tg := range subs {
		m := nextMeta(tg, infos, prior)
		old, had := prior[m.rank]
		changed = changed || !had || !old.sameResult(m)
		out = append(out, m)
	}
	return out, changed
}

// nextMeta returns the meta cursor of one sub-agent: the prior one when its
// file did not change, else a fresh read through readMetaFile.
func nextMeta(tg target, infos map[string]os.FileInfo, prior map[string]metaCursor) metaCursor {
	m := metaCursor{rank: tg.fc.rank}
	if tg.meta == "" {
		return m
	}
	m.path = tg.meta
	fi := infos[tg.meta]
	if fi != nil {
		m.size, m.modTime = fi.Size(), fi.ModTime()
	}
	if old, ok := prior[m.rank]; ok && old.unchangedSince(tg.meta, fi) {
		return old
	}
	md, err := readMetaFile(tg.meta)
	if err != nil {
		m.unreadable = true
		return m
	}
	m.typ, m.state, m.depth = md.AgentType, md.state(), md.SpawnDepth
	return m
}

// newestMod returns the newest mtime among the files and metas of d, clamped
// to the time d was read.
func newestMod(d Delta) time.Time {
	var newest time.Time
	for _, fc := range d.files {
		if fc.modTime.After(newest) {
			newest = fc.modTime
		}
	}
	for _, m := range d.metas {
		if m.modTime.After(newest) {
			newest = m.modTime
		}
	}
	if newest.After(d.read) {
		newest = d.read
	}
	return newest
}

// Activity tells a poller when a session has gone quiet. It holds times and
// counts only, never a path.
type Activity struct {
	// LastChange is the newest mtime among the parent, the sub-agents and the
	// metas as of the last read that found a change, capped at that read's
	// time. Zero before the first Apply. An empty delta does not advance it.
	LastChange time.Time
	// Unfinished counts sub-agents whose meta says finished: false.
	Unfinished int
	// failing is set while any transcript (parent or sub-agent) cannot be
	// opened or read (IssueOpenFailed, IssueReadError).
	failing bool
}

// Settled reports whether polling can stop: something was applied, no
// transcript is failing to open or read (the poller keeps trying), no
// sub-agent is unfinished, and nothing changed for at least quiet before now.
// If ReadDelta returned an error, the caller must not consider the session
// Settled: Activity then holds the last applied state.
func (a Activity) Settled(now time.Time, quiet time.Duration) bool {
	return !a.LastChange.IsZero() && !a.failing && a.Unfinished == 0 && now.Sub(a.LastChange) >= quiet
}

// Activity returns what the reads applied so far say about the session's life.
func (a *Aggregate) Activity() Activity {
	act := Activity{LastChange: a.lastMod}
	for _, m := range a.metas {
		if m.path != "" && !m.unreadable && m.state == StateUnfinished {
			act.Unfinished++
		}
	}
	for _, f := range a.files {
		if f.hasIssue && (f.issue == IssueOpenFailed || f.issue == IssueReadError) {
			act.failing = true
			break
		}
	}
	return act
}
