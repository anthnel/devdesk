package agentusage

import (
	"fmt"
	"reflect"
	"testing"
)

// tline is one line to feed a table: which agent and file it belongs to, where
// it sits, and its bytes.
type tline struct {
	agent, name string
	p           pos
	b           []byte
}

func parentPos(off int64) pos { return pos{rank: "0", off: off} }

// subPos mirrors the rank the plan gives a sub-agent's file.
func subPos(id, name string, off int64) pos {
	return pos{rank: "1" + id + "\x00" + name, off: off}
}

func pl(off int64, b []byte) tline { return tline{"", "p.jsonl", parentPos(off), b} }

func sl(id string, off int64, b []byte) tline {
	name := "agent-" + id + ".jsonl"
	return tline{id, name, subPos(id, name, off), b}
}

// verLine is a usage line carrying a client version.
func verLine(ver, id string) []byte {
	return []byte(fmt.Sprintf(`{"version":%q,"message":{"id":%q,"model":"x","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`, ver, id))
}

func tableAgents(subs ...string) []Agent {
	agents := []Agent{{Main: true}}
	for _, id := range subs {
		agents = append(agents, Agent{ID: id})
	}
	return agents
}

// feed builds a table from lines in the order given.
func feed(lines []tline) *table {
	t := newTable()
	for _, l := range lines {
		t.addLine(l.agent, l.name, l.p, l.b)
	}
	return t
}

type tableOut struct {
	Agents   []Agent
	Diag     Diagnostics
	Versions []string
}

// result runs finish and fails if it returned the wrong number of agents, so
// a missing implementation fails here instead of panicking on an index.
func result(t *testing.T, tb *table, agents []Agent) tableOut {
	t.Helper()
	a, d, v := tb.finish(agents)
	if len(a) != len(agents) {
		t.Fatalf("finish returned %d agents, want %d", len(a), len(agents))
	}
	return tableOut{a, d, v}
}

// interleavings returns every merge of a and b that keeps each one's own order.
func interleavings(a, b []tline) [][]tline {
	if len(a) == 0 {
		return [][]tline{append([]tline(nil), b...)}
	}
	if len(b) == 0 {
		return [][]tline{append([]tline(nil), a...)}
	}
	var out [][]tline
	for _, rest := range interleavings(a[1:], b) {
		out = append(out, append([]tline{a[0]}, rest...))
	}
	for _, rest := range interleavings(a, b[1:]) {
		out = append(out, append([]tline{b[0]}, rest...))
	}
	return out
}

func TestTableOrderFreedom(t *testing.T) {
	parent := []tline{
		pl(0, msgLine("m1", "opus", "", 1, 1, 1, 1)),
		pl(100, msgLine("m2", "opus", "end_turn", 2, 2, 2, 2)),
		pl(200, msgLine("m1", "opus", "end_turn", 3, 3, 3, 3)),
	}
	sub := []tline{
		sl("a1", 0, msgLine("m1", "sonnet", "end_turn", 5, 5, 5, 5)),
		sl("a1", 100, msgLine("m3", "sonnet", "end_turn", 7, 7, 7, 7)),
	}
	all := interleavings(parent, sub)
	if len(all) != 10 {
		t.Fatalf("interleavings = %d, want 10", len(all))
	}
	var first tableOut
	for i, lines := range all {
		got := result(t, feed(lines), tableAgents("a1"))
		if i == 0 {
			first = got
			if got.Agents[0].Tokens != (Tokens{2, 2, 2, 2}) || got.Agents[0].Messages != 1 {
				t.Fatalf("parent = %+v, want only m2", got.Agents[0])
			}
			if got.Agents[1].Tokens != (Tokens{12, 12, 12, 12}) || got.Agents[1].Messages != 2 {
				t.Fatalf("a1 = %+v, want m1 (v from a1) + m3", got.Agents[1])
			}
			if got.Diag.CrossFileDuplicates != 1 {
				t.Fatalf("CrossFileDuplicates = %d, want 1", got.Diag.CrossFileDuplicates)
			}
			if !reflect.DeepEqual(got.Agents[0].Models, []string{"opus"}) || !reflect.DeepEqual(got.Agents[1].Models, []string{"sonnet"}) {
				t.Fatalf("models = %v / %v", got.Agents[0].Models, got.Agents[1].Models)
			}
			continue
		}
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("interleaving %d differs:\n got %+v\nwant %+v", i, got, first)
		}
	}
}

// The winner is decided by rank, not by arrival: the sub-agent file fed first
// still owns the shared message.
func TestTableSubAgentFedFirstStillWins(t *testing.T) {
	tb := feed([]tline{
		sl("a1", 0, msgLine("m1", "x", "end_turn", 9, 9, 9, 9)),
		pl(0, msgLine("m1", "x", "end_turn", 1, 1, 1, 1)),
	})
	got := result(t, tb, tableAgents("a1"))
	if got.Agents[0].Messages != 0 || got.Agents[1].Tokens != (Tokens{9, 9, 9, 9}) {
		t.Fatalf("agents = %+v, want m1 on a1", got.Agents)
	}
	if got.Diag.CrossFileDuplicates != 1 {
		t.Fatalf("CrossFileDuplicates = %d, want 1", got.Diag.CrossFileDuplicates)
	}
}

func TestTableThreeFilesShareAMessage(t *testing.T) {
	lines := []tline{
		pl(0, msgLine("m1", "x", "end_turn", 1, 1, 1, 1)),
		sl("a1", 0, msgLine("m1", "x", "end_turn", 2, 2, 2, 2)),
		sl("a2", 0, msgLine("m1", "x", "end_turn", 3, 3, 3, 3)),
	}
	orders := [][]int{{0, 1, 2}, {2, 1, 0}, {1, 2, 0}, {2, 0, 1}}
	for _, o := range orders {
		t.Run(fmt.Sprint(o), func(t *testing.T) {
			var in []tline
			for _, i := range o {
				in = append(in, lines[i])
			}
			got := result(t, feed(in), tableAgents("a1", "a2"))
			if got.Diag.CrossFileDuplicates != 2 {
				t.Fatalf("CrossFileDuplicates = %d, want 2", got.Diag.CrossFileDuplicates)
			}
			if got.Agents[0].Messages != 0 || got.Agents[1].Messages != 0 || got.Agents[2].Tokens != (Tokens{3, 3, 3, 3}) {
				t.Fatalf("agents = %+v, want m1 on the highest rank (a2)", got.Agents)
			}
		})
	}
}

// Within one file a later offset wins regardless of arrival, and models are
// ordered by first position, not by arrival.
func TestTableSameFileOrderedByOffset(t *testing.T) {
	got := result(t, feed([]tline{
		pl(200, msgLine("m2", "sonnet", "end_turn", 1, 1, 0, 0)),
		pl(100, msgLine("m1", "opus", "", 1, 1, 0, 0)),
		pl(300, msgLine("m1", "opus", "end_turn", 4, 4, 0, 0)),
	}), tableAgents())
	if want := []string{"opus", "sonnet"}; !reflect.DeepEqual(got.Agents[0].Models, want) {
		t.Fatalf("Models = %v, want %v", got.Agents[0].Models, want)
	}
	if got.Agents[0].Tokens != (Tokens{5, 5, 0, 0}) || got.Agents[0].PartialMessages != 0 {
		t.Fatalf("agent = %+v, want m1 at offset 300 plus m2", got.Agents[0])
	}
	if got.Diag.CrossFileDuplicates != 0 {
		t.Fatalf("CrossFileDuplicates = %d, want 0", got.Diag.CrossFileDuplicates)
	}
}

// Merge = add: for every split point, merging the two halves equals feeding
// everything to one table.
func TestTableMergeEqualsAdd(t *testing.T) {
	lines := []tline{
		pl(0, msgLine("m1", "opus", "", 1, 1, 1, 1)),
		pl(100, []byte(`not json`)),
		pl(150, msgLine("", "opus", "end_turn", 1, 2, 3, 4)),
		pl(200, msgLine("m1", "opus", "end_turn", 3, 3, 3, 3)),
		pl(300, []byte(`{"type":"queue-operation"}`)),
		sl("a1", 0, msgLine("m1", "sonnet", "end_turn", 5, 5, 5, 5)),
		sl("a1", 100, verLine("2.0.0", "m3")),
		sl("a1", 200, []byte(`{"message":{"id":"n","model":"x"}}`)),
		sl("a2", 0, msgLine("m1", "haiku", "", 6, 6, 6, 6)),
		sl("a2", 90, msgLine("", "haiku", "", 7, 7, 7, 7)),
	}
	agents := func() []Agent { return tableAgents("a1", "a2") }
	want := result(t, feed(lines), agents())
	for k := 0; k <= len(lines); k++ {
		t.Run(fmt.Sprintf("split=%d", k), func(t *testing.T) {
			t1, t2 := feed(lines[:k]), feed(lines[k:])
			t1.merge(t2)
			if got := result(t, t1, agents()); !reflect.DeepEqual(got, want) {
				t.Fatalf("merged differs:\n got %+v\nwant %+v", got, want)
			}
			// Commutative: merging the other way round gives the same table.
			u1, u2 := feed(lines[:k]), feed(lines[k:])
			u2.merge(u1)
			if got := result(t, u2, agents()); !reflect.DeepEqual(got, want) {
				t.Fatalf("reverse merge differs:\n got %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestTableVersionsOrderedByFirstPosition(t *testing.T) {
	got := result(t, feed([]tline{
		sl("a1", 0, verLine("1.0.0", "a")),
		sl("a1", 10, verLine("2.0.0", "b")),
		sl("a1", 20, verLine("3.0.0", "c")),
		pl(0, verLine("9.0.0", "p")),
		pl(10, verLine("2.0.0", "q")),
	}), tableAgents("a1"))
	want := []string{"9.0.0", "2.0.0", "1.0.0", "3.0.0"}
	if !reflect.DeepEqual(got.Versions, want) {
		t.Fatalf("Versions = %v, want %v (parent's first, then by position)", got.Versions, want)
	}
}

// The per-file cap on unreadable/oversized issues holds across a merge while
// the counters keep counting.
func TestTableLineIssueCapAcrossMerge(t *testing.T) {
	half := maxFileIssuesPerFile * 6 / 10
	var lo, hi []tline
	for i := 0; i < half; i++ {
		lo = append(lo, pl(int64(i*10), []byte(`not json`)))
		hi = append(hi, pl(int64((half+i)*10), []byte(`not json`)))
	}
	t1, t2 := feed(lo), feed(hi)
	t1.merge(t2)
	got := result(t, t1, tableAgents())
	if got.Diag.UnreadableLines != 2*half {
		t.Fatalf("UnreadableLines = %d, want %d", got.Diag.UnreadableLines, 2*half)
	}
	if len(got.Diag.Files) != maxFileIssuesPerFile {
		t.Fatalf("kept issues = %d, want cap %d", len(got.Diag.Files), maxFileIssuesPerFile)
	}
	for i, f := range got.Diag.Files {
		if f.Offset != int64(i*10) || f.Kind != IssueUnreadable {
			t.Fatalf("issue %d = %+v, want the first %d by offset", i, f, maxFileIssuesPerFile)
		}
	}
}

// A no-id line at the same offset of two files is two messages: the key
// carries the file, so neither overwrites the other nor counts as a cross-file
// duplicate, whatever the arrival order.
func TestTableNoIDSameOffsetInTwoFiles(t *testing.T) {
	a := pl(0, msgLine("", "x", "end_turn", 1, 1, 1, 1))
	b := sl("a1", 0, msgLine("", "x", "end_turn", 2, 2, 2, 2))
	for name, in := range map[string][]tline{"parent first": {a, b}, "sub first": {b, a}} {
		t.Run(name, func(t *testing.T) {
			got := result(t, feed(in), tableAgents("a1"))
			if got.Agents[0].Messages != 1 || got.Agents[1].Messages != 1 {
				t.Fatalf("messages = %d/%d, want 1/1", got.Agents[0].Messages, got.Agents[1].Messages)
			}
			if got.Agents[0].Tokens != (Tokens{1, 1, 1, 1}) || got.Agents[1].Tokens != (Tokens{2, 2, 2, 2}) {
				t.Fatalf("tokens = %+v / %+v", got.Agents[0].Tokens, got.Agents[1].Tokens)
			}
			if got.Diag.CrossFileDuplicates != 0 || got.Diag.NoMessageID != 2 {
				t.Fatalf("diag = %+v, want 0 cross-file duplicates and 2 no-id", got.Diag)
			}
		})
	}
}
