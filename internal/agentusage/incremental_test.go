package agentusage

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Tests of the session level of the incremental reader (DEVDESK-44 step 4):
// ReadDelta, Aggregate, ParseSession re-routed. The oracle of the story is
// D3: applying successive deltas equals ParseSession on the final files.

// --- helpers -------------------------------------------------------------

const incSessUUID = "00000000-0000-4000-8000-0000000000aa"

// sess is a session laid out under a temp dir the way Claude Code does it:
// <dir>/<uuid>.jsonl and <dir>/<uuid>/subagents/agent-<id>.{jsonl,meta.json}.
type sess struct {
	t    *testing.T
	dir  string
	uuid string
}

func newSess(t *testing.T) *sess { return newSessIn(t, "proj") }

// newSessIn isolates HOME and lays the session out in a directory called name.
func newSessIn(t *testing.T, name string) *sess {
	t.Helper()
	isolateReadFileHome(t)
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return &sess{t: t, dir: dir, uuid: incSessUUID}
}

func (s *sess) parent() string       { return filepath.Join(s.dir, s.uuid+".jsonl") }
func (s *sess) subDir() string       { return filepath.Join(s.dir, s.uuid, "subagents") }
func (s *sess) sub(id string) string { return filepath.Join(s.subDir(), "agent-"+id+".jsonl") }
func (s *sess) metaPath(id string) string {
	return filepath.Join(s.subDir(), "agent-"+id+".meta.json")
}

// put writes a file, creating its directory.
func (s *sess) put(path string, data []byte) {
	s.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		s.t.Fatal(err)
	}
	putFile(s.t, path, data)
}

// add appends to a file, creating it if needed.
func (s *sess) add(path string, data []byte) {
	s.t.Helper()
	if _, err := os.Lstat(path); err != nil {
		s.put(path, data)
		return
	}
	appendFile(s.t, path, data)
}

// putMeta writes a meta file whose mtime is age before now.
func (s *sess) putMeta(id, body string, age time.Duration) {
	s.t.Helper()
	s.put(s.metaPath(id), []byte(body))
	at := time.Now().Add(-age).Truncate(time.Second)
	if err := os.Chtimes(s.metaPath(id), at, at); err != nil {
		s.t.Fatal(err)
	}
}

const (
	metaUnfinished = `{"agentType":"coder","finished":false}`
	metaFinished   = `{"agentType":"coder","finished": true}` // same size as metaUnfinished
)

func (s *sess) located() SessionFiles {
	s.t.Helper()
	files, err := LocateSession(s.parent())
	if err != nil {
		s.t.Fatalf("LocateSession: %v", err)
	}
	return files
}

// readApply reads from the aggregate's cursor and applies the result.
func readApply(t *testing.T, a *Aggregate) Delta {
	t.Helper()
	d, err := ReadDelta(a.Cursor())
	if err != nil {
		t.Fatalf("ReadDelta: %v", err)
	}
	if err := a.Apply(d); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return d
}

// normUsage makes empty and nil slices equal; the two are not a difference
// the contract draws.
func normUsage(u Usage) Usage {
	u.Agents = slices.Clone(u.Agents)
	for i := range u.Agents {
		if len(u.Agents[i].Models) == 0 {
			u.Agents[i].Models = nil
		}
	}
	if len(u.Agents) == 0 {
		u.Agents = nil
	}
	if len(u.Versions) == 0 {
		u.Versions = nil
	}
	if len(u.Diagnostics.TruncatedFiles) == 0 {
		u.Diagnostics.TruncatedFiles = nil
	}
	if len(u.Diagnostics.Files) == 0 {
		u.Diagnostics.Files = nil
	}
	return u
}

func assertSameUsage(t *testing.T, got, want Usage) {
	t.Helper()
	if !reflect.DeepEqual(normUsage(got), normUsage(want)) {
		t.Errorf("usage differs:\n got %+v\nwant %+v", got, want)
	}
}

// assertMatchesParse is the oracle of the story (D3): the aggregate equals
// ParseSession on the files as they are now.
func assertMatchesParse(t *testing.T, a *Aggregate, s *sess) {
	t.Helper()
	want, err := ParseSession(s.located())
	if err != nil {
		t.Fatalf("ParseSession: %v", err)
	}
	assertSameUsage(t, a.Usage(), want)
	// Privacy guard: no tool id (one carries the sentinel) reaches a Usage.
	if out := fmt.Sprintf("%+v", a.Usage()); strings.Contains(out, "SENTINEL") || strings.Contains(out, "toolu_") {
		t.Errorf("Usage leaks a tool id or content: %s", out)
	}
}

func agentByID(t *testing.T, u Usage, id string) Agent {
	t.Helper()
	for _, ag := range u.Agents {
		if ag.ID == id && !ag.Main {
			return ag
		}
	}
	t.Fatalf("no sub-agent %q in %+v", id, u.Agents)
	return Agent{}
}

// mainMessages returns the message count of the orchestrator, failing the
// test instead of panicking when the usage has no agent.
func mainMessages(t *testing.T, u Usage) int {
	t.Helper()
	if len(u.Agents) == 0 {
		t.Fatalf("no agent in %+v", u)
	}
	return u.Agents[0].Messages
}

func hasIssue(u Usage, file string, kind IssueKind) bool {
	for _, fi := range u.Diagnostics.Files {
		if fi.File == file && fi.Kind == kind {
			return true
		}
	}
	return false
}

// countMetaReads counts the meta files read through readMetaFile.
func countMetaReads(t *testing.T) *int {
	t.Helper()
	n := new(int)
	orig := readMetaFile
	readMetaFile = func(path string) (meta, error) {
		*n++
		return orig(path)
	}
	t.Cleanup(func() { readMetaFile = orig })
	return n
}

// src is a file to rebuild chunk by chunk.
type src struct {
	path string
	data []byte
}

// cloneSession copies a fixture session under a temp dir, keeping its session
// id. Transcripts are created empty and returned as sources when empty is
// set, written whole otherwise; meta files are always written whole.
func cloneSession(t *testing.T, files SessionFiles, empty bool) (*sess, []src) {
	t.Helper()
	s := newSess(t)
	s.uuid = strings.TrimSuffix(filepath.Base(files.Parent), transcriptExt)
	read := func(p string) []byte {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var srcs []src
	place := func(from, to string) {
		data := read(from)
		if empty {
			s.put(to, nil)
			srcs = append(srcs, src{to, data})
			return
		}
		s.put(to, data)
	}
	place(files.Parent, s.parent())
	for _, sa := range files.SubAgents {
		place(sa.Transcript, s.sub(sa.ID))
		if sa.Meta != "" {
			s.put(s.metaPath(sa.ID), read(sa.Meta))
		}
	}
	return s, srcs
}

// nextChunk returns the end of the chunk of data starting at off: size bytes,
// or through the next newline when size is 0.
func nextChunk(data []byte, off, size int) int {
	if size > 0 {
		return min(off+size, len(data))
	}
	if i := bytes.IndexByte(data[off:], '\n'); i >= 0 {
		return off + i + 1
	}
	return len(data)
}

// --- equivalence (D3) ------------------------------------------------------

// Fixtures rebuilt chunk by chunk, files interleaved round-robin, one
// ReadDelta + Apply after every chunk: the result is ParseSession's.
func TestReadDeltaEquivalenceFixtures(t *testing.T) {
	chunkings := []struct {
		name string
		size int // 0: line by line
	}{{"1 byte", 1}, {"7 bytes", 7}, {"64 bytes", 64}, {"4096 bytes", 4096}, {"line by line", 0}}
	for _, name := range []string{"v2.1.302-basic", "v2.1.302-activity", "synthetic-other-version", "damaged"} {
		for _, ck := range chunkings {
			t.Run(name+"/"+ck.name, func(t *testing.T) {
				orig := fixtureSession(t, name)
				s, srcs := cloneSession(t, orig, true)
				a := NewAggregate(s.parent())
				if d := readApply(t, a); !d.Full() {
					t.Error("first delta: Full = false, want true")
				}
				offs := make([]int, len(srcs))
				for pending := true; pending; {
					pending = false
					for i, sr := range srcs {
						if offs[i] >= len(sr.data) {
							continue
						}
						pending = true
						end := nextChunk(sr.data, offs[i], ck.size)
						appendFile(t, sr.path, sr.data[offs[i]:end])
						offs[i] = end
						readApply(t, a)
						if ck.size != 1 { // the 1-byte run is checked at the end only
							assertMatchesParse(t, a, s)
						}
						if t.Failed() {
							t.Fatalf("diverged after %d bytes of %s", end, filepath.Base(sr.path))
						}
					}
				}
				assertMatchesParse(t, a, s)
				want, err := ParseSession(orig)
				if err != nil {
					t.Fatal(err)
				}
				assertSameUsage(t, a.Usage(), want)
				if name == "v2.1.302-basic" {
					// hand-written totals of TestParseSessionBasic, summed
					if got, want := a.Usage().Total(), (Tokens{33, 588, 4640, 1560}); got != want {
						t.Errorf("Total = %+v, want %+v", got, want)
					}
				}
			})
		}
	}
}

// randomLines builds the content of one random transcript: repeated ids (also
// across files), lines without id, bad and blank lines, and sometimes a last
// line that is still being written.
func randomLines(rng *rand.Rand) []byte {
	pool := []string{"m1", "m2", "m3", "m4", "m5"}
	stops := []string{"end_turn", "", "tool_use"}
	var buf bytes.Buffer
	for n := rng.Intn(12); n > 0; n-- {
		switch rng.Intn(14) {
		case 0:
			buf.WriteString("not json {{{\n")
		case 1:
			buf.WriteString("\n")
		case 2, 3:
			buf.Write(msgLine("", "claude-n", stops[rng.Intn(3)], int64(rng.Intn(9)), 1, 2, 3))
			buf.WriteByte('\n')
		case 10, 11, 12, 13:
			buf.Write(randomActivityLine(rng, pool))
			buf.WriteByte('\n')
		default:
			buf.Write(msgLine(pool[rng.Intn(len(pool))], "claude-"+strconv.Itoa(rng.Intn(3)),
				stops[rng.Intn(3)], int64(rng.Intn(50)), int64(rng.Intn(50)), int64(rng.Intn(50)), int64(rng.Intn(50))))
			buf.WriteByte('\n')
		}
	}
	if rng.Intn(3) == 0 {
		whole := msgLine("m9", "claude-0", "end_turn", 1, 1, 1, 1)
		buf.Write(whole[:1+rng.Intn(len(whole)-1)])
	}
	return buf.Bytes()
}

// activityIDs is the pool of tool ids of the random generator; one carries
// the sentinel, which must never reach a Usage.
var activityIDs = []string{"t1", "t2", "toolu_SENTINEL-DO-NOT-LEAK_1", "t4", ""}

// activityStamps is out of order on purpose, holds a non-UTC spelling of an
// instant, and "" (no timestamp).
var activityStamps = []string{at(10, 5, 0), at(10, 1, 0), at(10, 9, 0), "2026-10-24T12:04:00+02:00", at(10, 3, 0), ""}

// randomActivityLine returns an assistant line with 0 to 3 tool_use blocks
// (sometimes without a message id or a tool id), a user tool-result line, or
// an api-request-blob replaying tool_use blocks.
func randomActivityLine(rng *rand.Rand, msgIDs []string) []byte {
	stamp := activityStamps[rng.Intn(len(activityStamps))]
	switch rng.Intn(4) {
	case 0:
		if stamp == "" {
			stamp = at(10, 2, 0)
		}
		return userLine(stamp)
	case 1:
		var ids []string
		for n := rng.Intn(3); n > 0; n-- {
			ids = append(ids, activityIDs[rng.Intn(len(activityIDs))])
		}
		return blobLine(ids...)
	}
	var blocks []string
	for n := rng.Intn(4); n > 0; n-- {
		blocks = append(blocks, tuBlock(activityIDs[rng.Intn(len(activityIDs))]))
	}
	id := msgIDs[rng.Intn(len(msgIDs))]
	if rng.Intn(4) == 0 {
		id = ""
	}
	return blocksLine("assistant", id, rng.Intn(3), stamp, blocks...)
}

// Random sessions, random chunking, sub-agents appearing late, metas added
// late: the aggregate equals ParseSession after every delta. The seed is
// logged; AGENTUSAGE_SEED replays a run.
func TestReadDeltaEquivalenceRandom(t *testing.T) {
	seed := time.Now().UnixNano()
	if v := os.Getenv("AGENTUSAGE_SEED"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		seed = n
	}
	t.Logf("seed %d", seed)
	for run := 0; run < 200; run++ {
		rng := rand.New(rand.NewSource(seed + int64(run)))
		s := newSess(t)
		type file struct {
			path    string
			data    []byte
			off     int
			created bool
			meta    string // written at a random step; "" for none
			id      string
		}
		files := []*file{{path: s.parent(), created: true}}
		files[0].data = randomLines(rng)
		s.put(s.parent(), nil)
		for _, id := range []string{"a1", "b2", "c3", "d4"}[:rng.Intn(5)] {
			f := &file{path: s.sub(id), data: randomLines(rng), id: id}
			if rng.Intn(2) == 0 {
				f.meta = []string{metaUnfinished, metaFinished}[rng.Intn(2)]
			}
			files = append(files, f)
		}
		a := NewAggregate(s.parent())
		readApply(t, a)
		for {
			var open []*file
			for _, f := range files {
				if !f.created || f.off < len(f.data) {
					open = append(open, f)
				}
			}
			if len(open) == 0 {
				break
			}
			f := open[rng.Intn(len(open))]
			end := min(f.off+1+rng.Intn(300), len(f.data))
			if !f.created {
				s.put(f.path, nil)
				f.created = true
			}
			s.add(f.path, f.data[f.off:end])
			f.off = end
			if f.meta != "" && rng.Intn(4) == 0 {
				s.putMeta(f.id, f.meta, time.Minute)
				f.meta = ""
			}
			readApply(t, a)
			assertMatchesParse(t, a, s)
			if t.Failed() {
				t.Fatalf("run %d (seed %d) diverged", run, seed+int64(run))
			}
		}
		for _, f := range files {
			if f.meta != "" {
				s.putMeta(f.id, f.meta, time.Minute)
			}
		}
		readApply(t, a)
		assertMatchesParse(t, a, s)
		if t.Failed() {
			t.Fatalf("run %d (seed %d) diverged at the end", run, seed+int64(run))
		}
	}
}

// --- volume and reads (criteria 1 and 2) --------------------------------------

// Criterion 2 at session level: 6 MiB of parent and two sub-agents, one line
// appended to one of them: about that line is read, and nothing else opened.
func TestReadDeltaVolumeIsTheAppendedBytes(t *testing.T) {
	s := newSess(t)
	var buf bytes.Buffer
	for i := 0; buf.Len() < 6<<20; i++ {
		buf.Write(ln(fmt.Sprintf("old-%d", i)))
	}
	s.put(s.parent(), buf.Bytes())
	s.put(s.sub("b1"), ln("s1"))
	s.put(s.sub("b2"), ln("s2"))
	p := countOpens(t)
	a := NewAggregate(s.parent())
	readApply(t, a)

	extra := ln("s3")
	s.add(s.sub("b2"), extra)
	*p = probe{}
	d := readApply(t, a)
	if d.Full() || d.Empty() {
		t.Errorf("Full=%v Empty=%v, want a partial, non-empty delta", d.Full(), d.Empty())
	}
	if limit := int64(len(extra)) + 1; p.bytes > limit {
		t.Errorf("bytes read = %d, want <= %d", p.bytes, limit)
	}
	if n := p.perPath[s.parent()] + p.perPath[s.sub("b1")]; n != 0 {
		t.Errorf("the parent and the other sub-agent were opened %d times, want 0", n)
	}
	if got := agentByID(t, a.Usage(), "b2").Messages; got != 2 {
		t.Errorf("b2 messages = %d, want 2", got)
	}
}

// Criterion 1: nothing written means no transcript opened, no meta read, and
// every delta empty.
func TestReadDeltaQuietSessionReadsNothing(t *testing.T) {
	s := newSess(t)
	s.put(s.parent(), cat(ln("p1"), ln("p2")))
	for _, id := range []string{"a1", "b2", "c3"} {
		s.put(s.sub(id), ln("s-"+id))
		s.putMeta(id, metaUnfinished, time.Minute)
	}
	p := countOpens(t)
	metas := countMetaReads(t)
	a := NewAggregate(s.parent())
	first := readApply(t, a)
	if !first.Full() || first.Empty() {
		t.Fatalf("first delta: Full=%v Empty=%v, want true and false", first.Full(), first.Empty())
	}
	if p.perPath[s.parent()] != 1 || *metas != 3 {
		t.Fatalf("first read: parent opens %d, meta reads %d, want 1 and 3", p.perPath[s.parent()], *metas)
	}
	want := a.Usage()
	*p, *metas = probe{}, 0
	for i := 0; i < 5; i++ {
		d := readApply(t, a)
		if !d.Empty() || d.Full() {
			t.Fatalf("pass %d: Empty=%v Full=%v, want true and false", i, d.Empty(), d.Full())
		}
	}
	if p.calls != 0 || *metas != 0 {
		t.Errorf("a quiet session: %d transcript open calls, %d meta reads, want 0 and 0", p.calls, *metas)
	}
	assertSameUsage(t, a.Usage(), want)
}

// What makes a delta Empty or Full.
func TestDeltaEmptyAndFull(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, s *sess)
		empty  bool
		full   bool
	}{
		{"nothing", func(*testing.T, *sess) {}, true, false},
		{"mtime only", func(t *testing.T, s *sess) {
			at := time.Now().Add(-time.Minute)
			if err := os.Chtimes(s.parent(), at, at); err != nil {
				t.Fatal(err)
			}
		}, true, false},
		{"line appended to the parent", func(_ *testing.T, s *sess) { s.add(s.parent(), ln("p2")) }, false, false},
		{"line appended to a sub-agent", func(_ *testing.T, s *sess) { s.add(s.sub("b1"), ln("s2")) }, false, false},
		{"sub-agent appears", func(_ *testing.T, s *sess) { s.add(s.sub("a0"), ln("n1")) }, false, false},
		{"sub-agent disappears", func(t *testing.T, s *sess) {
			for _, p := range []string{s.sub("b1"), s.metaPath("b1")} {
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
			}
		}, false, true},
		{"meta rewritten", func(_ *testing.T, s *sess) { s.putMeta("b1", metaFinished, time.Second) }, false, false},
		{"meta appears", func(_ *testing.T, s *sess) { s.putMeta("c9", metaUnfinished, time.Second) }, false, false},
		{"meta removed", func(t *testing.T, s *sess) {
			if err := os.Remove(s.metaPath("b1")); err != nil {
				t.Fatal(err)
			}
		}, false, false},
		{"parent truncated", func(_ *testing.T, s *sess) { s.put(s.parent(), nil) }, false, true},
		{"parent rotated", func(t *testing.T, s *sess) {
			tmp := s.parent() + ".new"
			s.put(tmp, cat(ln("p1"), ln("rotated")))
			if err := os.Rename(tmp, s.parent()); err != nil {
				t.Fatal(err)
			}
		}, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newSess(t)
			s.put(s.parent(), ln("p1"))
			s.put(s.sub("b1"), ln("s1"))
			s.putMeta("b1", metaUnfinished, time.Minute)
			s.put(s.sub("c9"), ln("s9")) // no meta yet
			a := NewAggregate(s.parent())
			readApply(t, a)
			tc.mutate(t, s)
			d := readApply(t, a)
			if d.Empty() != tc.empty || d.Full() != tc.full {
				t.Errorf("Empty=%v Full=%v, want %v and %v", d.Empty(), d.Full(), tc.empty, tc.full)
			}
			assertMatchesParse(t, a, s)
		})
	}
}

// --- sub-agents coming and going, parent rewritten (D4) ----------------------

func TestReadDeltaNewSubAgentIsNotAFullRead(t *testing.T) {
	s := newSess(t)
	s.put(s.parent(), ln("p1"))
	s.put(s.sub("b1"), ln("s1"))
	s.put(s.sub("c1"), ln("s2"))
	a := NewAggregate(s.parent())
	readApply(t, a)

	s.put(s.sub("a0"), cat(ln("n1"), ln("n2"))) // sorts before the others
	d := readApply(t, a)
	if d.Full() {
		t.Error("Full = true, want false: a new file is read from 0 inside a normal delta")
	}
	var ids []string
	for _, ag := range a.Usage().Agents {
		ids = append(ids, ag.ID)
	}
	if want := []string{"", "a0", "b1", "c1"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("agents = %q, want %q", ids, want)
	}
	if got := agentByID(t, a.Usage(), "a0").Messages; got != 2 {
		t.Errorf("a0 messages = %d, want 2", got)
	}
	assertMatchesParse(t, a, s)
}

func TestReadDeltaSubAgentDeletedIsAFullRead(t *testing.T) {
	s := newSess(t)
	s.put(s.parent(), ln("p1"))
	s.put(s.sub("b1"), ln("s1"))
	s.put(s.sub("c1"), ln("s2"))
	a := NewAggregate(s.parent())
	readApply(t, a)

	if err := os.Remove(s.sub("b1")); err != nil {
		t.Fatal(err)
	}
	if d := readApply(t, a); !d.Full() {
		t.Error("Full = false, want true: a file read before is gone")
	}
	for _, ag := range a.Usage().Agents {
		if ag.ID == "b1" {
			t.Errorf("b1 still listed: %+v", ag)
		}
	}
	assertMatchesParse(t, a, s)
}

func TestReadDeltaSubAgentComesAndGoes(t *testing.T) {
	s := newSess(t)
	s.put(s.parent(), ln("p1"))
	a := NewAggregate(s.parent())
	readApply(t, a)
	steps := []struct {
		name string
		do   func()
		full bool
	}{
		{"appears", func() { s.put(s.sub("z1"), ln("z-a")) }, false},
		{"grows", func() { s.add(s.sub("z1"), ln("z-b")) }, false},
		{"goes", func() { _ = os.Remove(s.sub("z1")) }, true},
		{"comes back with other content", func() { s.put(s.sub("z1"), ln("z-c")) }, false},
	}
	for _, st := range steps {
		st.do()
		if d := readApply(t, a); d.Full() != st.full {
			t.Errorf("%s: Full = %v, want %v", st.name, d.Full(), st.full)
		}
		assertMatchesParse(t, a, s)
	}
	if got := agentByID(t, a.Usage(), "z1").Messages; got != 1 {
		t.Errorf("z1 messages = %d, want 1 (nothing of its earlier life may remain)", got)
	}
}

func TestReadDeltaParentRewrittenIsAFullRead(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, s *sess)
	}{
		{"truncated", func(_ *testing.T, s *sess) { s.put(s.parent(), ln("p1")) }},
		{"rotated, larger", func(t *testing.T, s *sess) {
			tmp := s.parent() + ".new"
			s.put(tmp, cat(ln("x1"), ln("x2"), ln("x3"), ln("x4")))
			if err := os.Rename(tmp, s.parent()); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newSess(t)
			s.put(s.parent(), cat(ln("p1"), ln("p2"), ln("p3")))
			s.put(s.sub("b1"), ln("s1"))
			a := NewAggregate(s.parent())
			readApply(t, a)
			tc.mutate(t, s)
			if d := readApply(t, a); !d.Full() {
				t.Error("Full = false, want true")
			}
			assertMatchesParse(t, a, s)
		})
	}
}

// ReadDelta fails only on the parent (or the listing); nothing is applied and
// no error carries a path.
func TestReadDeltaParentUnusable(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, s *sess) string // returns the parent path to use
		isNot bool                               // errors.Is(err, os.ErrNotExist) expected
		base  string
	}{
		{"never existed", func(_ *testing.T, s *sess) string { return s.parent() }, true, incSessUUID + ".jsonl"},
		{"deleted after a read", func(t *testing.T, s *sess) string {
			s.put(s.parent(), ln("p1"))
			a := NewAggregate(s.parent())
			readApply(t, a)
			if err := os.Remove(s.parent()); err != nil {
				t.Fatal(err)
			}
			return s.parent()
		}, true, incSessUUID + ".jsonl"},
		{"a directory", func(t *testing.T, s *sess) string {
			if err := os.Mkdir(s.parent(), 0o700); err != nil {
				t.Fatal(err)
			}
			return s.parent()
		}, false, incSessUUID + ".jsonl"},
		{"no .jsonl suffix", func(*testing.T, *sess) string { return "x.txt" }, false, "x.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newSessIn(t, "SENTINEL-DO-NOT-LEAK-dir")
			parent := tc.setup(t, s)
			a := NewAggregate(parent)
			before, cursorBefore := a.Usage(), a.Cursor()
			_, err := ReadDelta(a.Cursor())
			if err == nil {
				t.Fatal("err = nil, want an error")
			}
			if tc.isNot && !errors.Is(err, os.ErrNotExist) {
				t.Errorf("errors.Is(err, os.ErrNotExist) = false: %v", err)
			}
			if msg := err.Error(); !strings.Contains(msg, tc.base) || strings.Contains(msg, "SENTINEL") || strings.Contains(msg, s.dir) {
				t.Errorf("error %q must name %q and no directory", msg, tc.base)
			}
			assertSameUsage(t, a.Usage(), before)
			if !reflect.DeepEqual(a.Cursor(), cursorBefore) {
				t.Error("the aggregate's cursor changed although nothing was applied")
			}
		})
	}
}

// --- meta files (D7) -----------------------------------------------------------

func TestReadDeltaMetaChanges(t *testing.T) {
	t.Run("finished flips with the same size", func(t *testing.T) {
		s := newSess(t)
		s.put(s.parent(), ln("p1"))
		s.put(s.sub("b1"), ln("s1"))
		s.putMeta("b1", metaUnfinished, time.Minute)
		a := NewAggregate(s.parent())
		readApply(t, a)
		if got := agentByID(t, a.Usage(), "b1").State; got != StateUnfinished {
			t.Fatalf("State = %v, want unfinished", got)
		}
		s.putMeta("b1", metaFinished, 30*time.Second) // same size, later mtime
		readApply(t, a)
		if got := agentByID(t, a.Usage(), "b1").State; got != StateFinished {
			t.Errorf("State = %v, want finished", got)
		}
		assertMatchesParse(t, a, s)
	})
	t.Run("meta appears after the transcript, then goes", func(t *testing.T) {
		s := newSess(t)
		s.put(s.parent(), ln("p1"))
		s.put(s.sub("b1"), ln("s1"))
		a := NewAggregate(s.parent())
		readApply(t, a)
		if got := a.Usage().Diagnostics.MetaMissing; got != 1 {
			t.Fatalf("MetaMissing = %d, want 1", got)
		}
		s.putMeta("b1", metaUnfinished, time.Second)
		readApply(t, a)
		if got := a.Usage().Diagnostics.MetaMissing; got != 0 {
			t.Errorf("MetaMissing = %d after the meta appeared, want 0", got)
		}
		if got := agentByID(t, a.Usage(), "b1"); got.Type != "coder" || got.State != StateUnfinished {
			t.Errorf("agent = %+v, want a coder, unfinished", got)
		}
		assertMatchesParse(t, a, s)
		if err := os.Remove(s.metaPath("b1")); err != nil {
			t.Fatal(err)
		}
		readApply(t, a)
		if got := a.Usage().Diagnostics.MetaMissing; got != 1 {
			t.Errorf("MetaMissing = %d after the meta was removed, want 1", got)
		}
		assertMatchesParse(t, a, s)
	})
	t.Run("malformed meta is unreadable until rewritten", func(t *testing.T) {
		s := newSess(t)
		s.put(s.parent(), ln("p1"))
		s.put(s.sub("b1"), ln("s1"))
		s.putMeta("b1", "{ nope", time.Minute)
		a := NewAggregate(s.parent())
		readApply(t, a)
		if got := a.Usage().Diagnostics.MetaUnreadable; got != 1 {
			t.Fatalf("MetaUnreadable = %d, want 1", got)
		}
		s.putMeta("b1", metaFinished, time.Second)
		readApply(t, a)
		if got := a.Usage().Diagnostics.MetaUnreadable; got != 0 {
			t.Errorf("MetaUnreadable = %d after the rewrite, want 0", got)
		}
		assertMatchesParse(t, a, s)
	})
}

// D7: a meta file is read again only when its Lstat size or mtime changed.
func TestReadDeltaMetaIsReadOnlyWhenItsLstatChanges(t *testing.T) {
	s := newSess(t)
	s.put(s.parent(), ln("p1"))
	s.put(s.sub("b1"), ln("s1"))
	s.put(s.sub("b2"), ln("s2"))
	s.putMeta("b1", metaUnfinished, time.Minute)
	s.putMeta("b2", metaUnfinished, time.Minute)
	reads := countMetaReads(t)
	a := NewAggregate(s.parent())
	readApply(t, a)
	if *reads != 2 {
		t.Fatalf("first read: %d meta reads, want 2", *reads)
	}

	// Transcripts grow, metas do not move: no meta is read.
	s.add(s.sub("b1"), ln("s3"))
	s.add(s.parent(), ln("p2"))
	readApply(t, a)
	readApply(t, a)
	if *reads != 2 {
		t.Errorf("meta reads = %d after transcript-only changes, want 2", *reads)
	}

	// One meta changes: that one only.
	s.putMeta("b1", metaFinished, 10*time.Second)
	readApply(t, a)
	if *reads != 3 {
		t.Errorf("meta reads = %d after one meta changed, want 3", *reads)
	}
	readApply(t, a)
	if *reads != 3 {
		t.Errorf("meta reads = %d on the pass after, want 3", *reads)
	}
}

// --- ParseSession is the same computation (D5) ---------------------------------

func TestParseSessionGoesThroughTheIncrementalPath(t *testing.T) {
	t.Run("every transcript is read through openTranscript, once", func(t *testing.T) {
		isolateReadFileHome(t)
		files := fixtureSession(t, "v2.1.302-basic")
		p := countOpens(t)
		if _, err := ParseSession(files); err != nil {
			t.Fatal(err)
		}
		if want := 1 + len(files.SubAgents); p.calls != want {
			t.Errorf("openTranscript calls = %d, want %d", p.calls, want)
		}
		for path, n := range p.perPath {
			if n != 1 {
				t.Errorf("%s opened %d times, want 1", filepath.Base(path), n)
			}
		}
	})
	t.Run("a parent that cannot be opened keeps being the error", func(t *testing.T) {
		isolateReadFileHome(t)
		files := fixtureSession(t, "v2.1.302-basic")
		swapOpen(t, func(string) (transcriptFile, error) { return nil, os.ErrPermission })
		u, err := ParseSession(files)
		if !errors.Is(err, os.ErrPermission) {
			t.Errorf("err = %v, want os.ErrPermission", err)
		}
		if !reflect.DeepEqual(normUsage(u), normUsage(Usage{})) {
			t.Errorf("usage = %+v, want the zero Usage", u)
		}
	})
	t.Run("a sub-agent that cannot be opened is not fatal", func(t *testing.T) {
		isolateReadFileHome(t)
		files := fixtureSession(t, "v2.1.302-basic")
		var bad string
		for _, sa := range files.SubAgents {
			if sa.ID == "a02" {
				bad = sa.Transcript
			}
		}
		orig := openTranscript
		swapOpen(t, func(path string) (transcriptFile, error) {
			if path == bad {
				return nil, os.ErrPermission
			}
			return orig(path)
		})
		u, err := ParseSession(files)
		if err != nil {
			t.Fatal(err)
		}
		if got := agentByID(t, u, "a02"); got.Tokens != (Tokens{}) || got.Messages != 0 {
			t.Errorf("a02 = %+v, want no tokens", got)
		}
		if !hasIssue(u, "agent-a02.jsonl", IssueOpenFailed) {
			t.Errorf("no IssueOpenFailed for agent-a02.jsonl in %+v", u.Diagnostics.Files)
		}
	})
}

// --- stale deltas (D9), rewound cursors, copies (D8) ---------------------------

func TestApplyRefusesAStaleDelta(t *testing.T) {
	setup := func(t *testing.T) (*sess, *Aggregate) {
		s := newSess(t)
		s.put(s.parent(), ln("p1"))
		a := NewAggregate(s.parent())
		readApply(t, a)
		return s, a
	}
	tests := []struct {
		name string
		run  func(t *testing.T, s *sess, a *Aggregate) (stale Delta)
	}{
		{"second read from the same cursor", func(t *testing.T, s *sess, a *Aggregate) Delta {
			c := a.Cursor()
			s.add(s.parent(), ln("p2"))
			d1, err1 := ReadDelta(c)
			d2, err2 := ReadDelta(c)
			if err1 != nil || err2 != nil {
				t.Fatal(err1, err2)
			}
			if err := a.Apply(d1); err != nil {
				t.Fatalf("first Apply: %v", err)
			}
			return d2
		}},
		{"the same delta twice", func(t *testing.T, s *sess, a *Aggregate) Delta {
			s.add(s.parent(), ln("p2"))
			d := readApply(t, a)
			return d
		}},
		{"a Full delta read from a cursor before an Apply", func(t *testing.T, s *sess, a *Aggregate) Delta {
			old, err := ReadDelta(a.Cursor().Rewound())
			if err != nil {
				t.Fatal(err)
			}
			s.add(s.parent(), ln("p2"))
			readApply(t, a)
			return old
		}},
		{"another aggregate's cursor", func(t *testing.T, s *sess, _ *Aggregate) Delta {
			other := NewAggregate(s.parent())
			d, err := ReadDelta(other.Cursor())
			if err != nil {
				t.Fatal(err)
			}
			return d
		}},
		{"a delta that was never read", func(*testing.T, *sess, *Aggregate) Delta { return Delta{} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, a := setup(t)
			stale := tc.run(t, s, a)
			before, cursorBefore := a.Usage(), a.Cursor()
			if err := a.Apply(stale); !errors.Is(err, ErrStaleDelta) {
				t.Fatalf("Apply = %v, want ErrStaleDelta", err)
			}
			assertSameUsage(t, a.Usage(), before)
			if !reflect.DeepEqual(a.Cursor(), cursorBefore) {
				t.Error("a refused delta moved the cursor")
			}
			// The documented recovery: read again from Cursor().
			readApply(t, a)
			assertMatchesParse(t, a, s)
		})
	}
}

// ctrl+r: a rewound cursor reads everything again and nothing is counted twice.
func TestRewoundCursorReadsEverythingAgain(t *testing.T) {
	s := newSess(t)
	s.put(s.parent(), cat(ln("p1"), ln("p2")))
	s.put(s.sub("b1"), ln("s1"))
	s.put(s.sub("b2"), ln("s2"))
	s.putMeta("b1", metaUnfinished, time.Minute)
	a := NewAggregate(s.parent())
	readApply(t, a)
	before := a.Usage()

	c := a.Cursor()
	snap := a.Cursor()
	p := countOpens(t)
	d, err := ReadDelta(c.Rewound())
	if err != nil {
		t.Fatal(err)
	}
	if !d.Full() {
		t.Error("Full = false, want true")
	}
	for _, path := range []string{s.parent(), s.sub("b1"), s.sub("b2")} {
		if p.perPath[path] != 1 {
			t.Errorf("%s opened %d times, want 1", filepath.Base(path), p.perPath[path])
		}
	}
	if err := a.Apply(d); err != nil {
		t.Fatal(err)
	}
	assertSameUsage(t, a.Usage(), before)

	// Rewinding a copy leaves the original cursor alone.
	if !reflect.DeepEqual(c, snap) {
		t.Error("Rewound changed the cursor it was called on")
	}
	if d2 := readApply(t, a); !d2.Empty() || d2.Full() {
		t.Errorf("after the rewound read: Empty=%v Full=%v, want true and false", d2.Empty(), d2.Full())
	}
}

// A Cursor is a value: handing it out, reading from it and advancing the
// aggregate never change one already given.
func TestCursorIsADeepCopy(t *testing.T) {
	s := newSess(t)
	s.put(s.parent(), ln("p1"))
	s.put(s.sub("b1"), ln("s1"))
	a := NewAggregate(s.parent())
	readApply(t, a)

	c1, snap := a.Cursor(), a.Cursor()
	if !reflect.DeepEqual(c1, snap) {
		t.Fatal("two cursors of the same state differ")
	}
	if _, err := ReadDelta(c1); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c1, snap) {
		t.Error("ReadDelta mutated its argument")
	}

	s.add(s.parent(), ln("p2"))
	s.add(s.sub("b1"), ln("s2"))
	readApply(t, a)
	if !reflect.DeepEqual(c1, snap) {
		t.Error("Apply mutated a cursor handed out earlier")
	}
	if reflect.DeepEqual(a.Cursor(), snap) {
		t.Error("the aggregate's cursor did not advance")
	}

	// The old cursor still describes the old position: reading from it sees
	// the two appended lines again, but its delta is stale for the aggregate.
	d, err := ReadDelta(c1)
	if err != nil {
		t.Fatal(err)
	}
	if d.Empty() || d.Full() {
		t.Errorf("from the old cursor: Empty=%v Full=%v, want false and false", d.Empty(), d.Full())
	}
	if err := a.Apply(d); !errors.Is(err, ErrStaleDelta) {
		t.Errorf("Apply from an old cursor = %v, want ErrStaleDelta", err)
	}
}

// Usage shares no memory with the aggregate.
func TestUsageIsADeepCopy(t *testing.T) {
	orig := fixtureSession(t, "damaged")
	s, _ := cloneSession(t, orig, false)
	s.add(s.parent(), msgLine("half", "m", "end_turn", 1, 1, 1, 1)[:30]) // a held-back line
	a := NewAggregate(s.parent())
	readApply(t, a)
	want, err := ParseSession(s.located())
	if err != nil {
		t.Fatal(err)
	}
	u := a.Usage()
	switch {
	case len(u.Agents) == 0 || len(u.Agents[0].Models) == 0:
		t.Fatal("setup: no model on the first agent")
	case len(u.Diagnostics.Files) == 0 || len(u.Diagnostics.TruncatedFiles) == 0 || len(u.Versions) == 0:
		t.Fatalf("setup: files %d, truncated %d, versions %d, want all non-empty",
			len(u.Diagnostics.Files), len(u.Diagnostics.TruncatedFiles), len(u.Versions))
	}
	u.Agents[0].Models[0] = "MUTATED"
	u.Agents[0].Tokens.Input = -1
	u.Diagnostics.Files[0].File = "MUTATED"
	u.Diagnostics.TruncatedFiles[0] = "MUTATED"
	u.Versions[0] = "MUTATED"
	u.Agents = append(u.Agents[:1], u.Agents[1:]...)
	assertSameUsage(t, a.Usage(), want)
	// And a later Apply does not write through a Usage handed out before.
	first := a.Usage()
	s.add(s.parent(), ln("later"))
	readApply(t, a)
	assertSameUsage(t, first, want)
}

// --- privacy ----------------------------------------------------------------------

func TestIncrementalNeverLeaksPathsOrContent(t *testing.T) {
	const sentinel = "SENTINEL-DO-NOT-LEAK"
	s, _ := cloneSession(t, fixtureSession(t, "damaged"), false)
	s2 := newSessIn(t, sentinel+"-dir")
	s2.uuid = s.uuid
	s2.put(s2.parent(), mustRead(t, s.parent()))
	s2.put(s2.sub("d1"), mustRead(t, s.sub("d1")))
	s2.put(s2.metaPath("d1"), mustRead(t, s.metaPath("d1")))

	a := NewAggregate(s2.parent())
	d1 := readApply(t, a)
	s2.add(s2.parent(), mustRead(t, s.parent())) // more sentinel content
	s2.add(s2.parent(), []byte(`{"cwd":"`+sentinel+`-cwd","message":{"id":"`+sentinel+`-id"}}`+"\n"))
	d2 := readApply(t, a)
	c := a.Cursor()

	var out strings.Builder
	// *Aggregate and the dereferenced value both carry the parent path and the
	// file cursors; fmt must not reach them under any verb.
	for _, v := range []any{c, c.Rewound(), d1, d2, a, *a} {
		fmt.Fprintf(&out, "%v %+v %#v %s\n", v, v, v, v)
	}
	fmt.Fprintf(&out, "%+v\n", a.Usage())
	if err := os.Remove(s2.parent()); err != nil {
		t.Fatal(err)
	}
	_, err := ReadDelta(a.Cursor())
	if err == nil {
		t.Fatal("ReadDelta on a deleted parent: err = nil")
	}
	fmt.Fprintf(&out, "%v %+v\n", err, err)
	text := out.String()
	for _, bad := range []string{sentinel, s2.dir, os.TempDir() + string(filepath.Separator)} {
		if strings.Contains(text, bad) {
			t.Errorf("output leaks %q:\n%s", bad, text)
		}
	}
	if text == "" || strings.TrimSpace(c.String()) == "" {
		t.Error("a redacted value must still print something")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCursorAndDeltaStringsAreRedacted(t *testing.T) {
	s := newSess(t)
	s.put(s.parent(), ln("p1"))
	s.put(s.sub("b1"), ln("s1"))
	s.put(s.sub("b2"), ln("s2"))
	a := NewAggregate(s.parent())
	d := readApply(t, a)
	c := a.Cursor()
	const want = "agentusage.Cursor{files: 3}"
	if got := c.String(); got != want {
		t.Errorf("Cursor.String = %q, want %q", got, want)
	}
	if got := c.GoString(); got != want {
		t.Errorf("Cursor.GoString = %q, want %q", got, want)
	}
	for name, got := range map[string]string{"String": d.String(), "GoString": d.GoString()} {
		if got == "" || strings.Contains(got, s.dir) {
			t.Errorf("Delta.%s = %q: must be non-empty and hold no directory", name, got)
		}
	}
}

// --- TOCTOU at session level -------------------------------------------------------

// A transcript swapped for another file between the listing and the open is
// not an append: the delta is a Full one and the result is ParseSession's.
func TestReadDeltaSwappedHandleTriggersAFullRead(t *testing.T) {
	s := newSess(t)
	s.put(s.parent(), ln("p1"))
	s.put(s.sub("b1"), cat(ln("s1"), ln("s2")))
	a := NewAggregate(s.parent())
	readApply(t, a)

	s.add(s.sub("b1"), ln("s3")) // grown: it will be opened
	other := filepath.Join(s.dir, "other.jsonl")
	s.put(other, cat(ln("x1"), ln("x2"), ln("x3"), ln("x4")))
	orig := openTranscript
	swapped := false
	swapOpen(t, func(path string) (transcriptFile, error) {
		if path == s.sub("b1") && !swapped {
			swapped = true
			return orig(other)
		}
		return orig(path)
	})
	d := readApply(t, a)
	if !swapped {
		t.Fatal("setup: the swap never fired")
	}
	if !d.Full() {
		t.Error("Full = false, want true: the handle was another file")
	}
	assertMatchesParse(t, a, s)
}

// --- the no-id cap across deltas (declared exception to D3) --------------------------

// aggEntries is the number of per-message entries the aggregate holds.
func aggEntries(a *Aggregate) int {
	if a.t == nil {
		return 0
	}
	return len(a.t.entries)
}

// merge folds deltas without the arrival-order cap a single read applies, so
// the aggregate itself must keep the memory bound: after N deltas of lines
// without id the table holds at most maxNoIDEntries entries, and the totals
// are still exact (a spilled line counts, it just has no entry). Which lines
// are the ones spilled follows position (mergeCapped keeps the smallest
// positions), not arrival: the declared exception to D3.
func TestAggregateBoundsNoIDEntriesAcrossDeltas(t *testing.T) {
	s := newSess(t)
	const perDelta, deltas = 35000, 3 // 105000 > maxNoIDEntries
	total := perDelta * deltas
	if total <= maxNoIDEntries {
		t.Fatal("setup: not enough lines to pass the cap")
	}
	batch := bytes.Repeat(append(msgLine("", "m", "end_turn", 1, 0, 0, 0), '\n'), perDelta)
	s.put(s.parent(), nil)
	a := NewAggregate(s.parent())
	for i := 0; i < deltas; i++ {
		s.add(s.parent(), batch)
		readApply(t, a)
	}
	u := a.Usage()
	if len(u.Agents) == 0 || u.Agents[0].Messages != total || u.Agents[0].Tokens.Input != int64(total) || u.Diagnostics.NoMessageID != total {
		t.Fatalf("totals: agents %+v, NoMessageID %d, want %d lines counted once each", u.Agents, u.Diagnostics.NoMessageID, total)
	}
	if n := aggEntries(a); n > maxNoIDEntries {
		t.Errorf("entries = %d, want <= %d", n, maxNoIDEntries)
	}
	assertMatchesParse(t, a, s) // same totals as one sequential read
}

// --- issues are states carried by the cursors (D6) ------------------------------------

func TestSessionIssuesComeFromCursorsAndClear(t *testing.T) {
	t.Run("open failure of a sub-agent", func(t *testing.T) {
		s := newSess(t)
		s.put(s.parent(), ln("p1"))
		s.put(s.sub("b1"), ln("s1"))
		a := NewAggregate(s.parent())
		readApply(t, a)

		s.add(s.sub("b1"), ln("s2")) // grown, so it is opened and the open fails
		orig := openTranscript
		swapOpen(t, func(path string) (transcriptFile, error) {
			if path == s.sub("b1") {
				return nil, os.ErrPermission
			}
			return orig(path)
		})
		if d := readApply(t, a); !d.Full() {
			t.Error("a file read before that cannot be opened: Full = false, want true")
		}
		u := a.Usage()
		if !hasIssue(u, "agent-b1.jsonl", IssueOpenFailed) {
			t.Errorf("no IssueOpenFailed for agent-b1.jsonl in %+v", u.Diagnostics.Files)
		}
		if got := agentByID(t, u, "b1"); got.Messages != 0 || got.Tokens != (Tokens{}) {
			t.Errorf("b1 = %+v, want no tokens while it cannot be opened", got)
		}
		if got := mainMessages(t, u); got != 1 {
			t.Errorf("parent messages = %d, want 1 (the restart must not lose or double it)", got)
		}

		openTranscript = orig // the file can be opened again
		readApply(t, a)
		if hasIssue(a.Usage(), "agent-b1.jsonl", IssueOpenFailed) {
			t.Errorf("IssueOpenFailed still there after a successful read: %+v", a.Usage().Diagnostics.Files)
		}
		if got := agentByID(t, a.Usage(), "b1").Messages; got != 2 {
			t.Errorf("b1 messages = %d, want 2", got)
		}
		assertMatchesParse(t, a, s)
	})
	t.Run("read error in the parent", func(t *testing.T) {
		s := newSess(t)
		l := int64(len(ln("a")))
		s.put(s.parent(), cat(ln("a"), ln("b"), ln("c")))
		faultFirstOpen(t, 2*l+l/2)
		a := NewAggregate(s.parent())
		readApply(t, a)
		u := a.Usage()
		var found bool
		for _, fi := range u.Diagnostics.Files {
			if fi.Kind == IssueReadError && fi.File == incSessUUID+".jsonl" && fi.Offset == 2*l {
				found = true
			}
		}
		if !found {
			t.Errorf("no IssueReadError at %d in %+v", 2*l, u.Diagnostics.Files)
		}
		if len(u.Agents) == 0 || u.Agents[0].Messages != 2 {
			t.Errorf("agents = %+v, want the two lines before the error", u.Agents)
		}

		d := readApply(t, a) // the file did not change, the bytes are read again
		if d.Empty() {
			t.Error("Empty = true after a read error: the missing bytes must be read again")
		}
		for _, fi := range a.Usage().Diagnostics.Files {
			if fi.Kind == IssueReadError {
				t.Errorf("IssueReadError still there after a clean read: %+v", fi)
			}
		}
		assertMatchesParse(t, a, s)
		if got := mainMessages(t, a.Usage()); got != 3 {
			t.Errorf("messages = %d, want 3, each counted once", got)
		}
	})
}
