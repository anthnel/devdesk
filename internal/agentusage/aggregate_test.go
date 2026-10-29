package agentusage

import (
	"fmt"
	"reflect"
	"testing"
)

// msgLine builds a transcript line; stop "" means stop_reason null.
func msgLine(id, model, stop string, in, out, cr, cw int64) []byte {
	sr := `null`
	if stop != "" {
		sr = `"` + stop + `"`
	}
	idKey := ""
	if id != "" {
		idKey = `"id":"` + id + `",`
	}
	return []byte(fmt.Sprintf(`{"version":"2.1.302","message":{%s"model":"%s","stop_reason":%s,"usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d}}}`,
		idKey, model, sr, in, out, cr, cw))
}

// testAgg drives a table the way the former aggregator was driven: lines go
// to an agent by index, and its rank follows from the agent and the file.
type testAgg struct {
	t      *table
	agents []Agent
}

func newTestAgg(agents []Agent) *testAgg { return &testAgg{t: newTable(), agents: agents} }

func (a *testAgg) addLine(idx int, file string, off int64, line []byte) {
	ag := a.agents[idx]
	if ag.Main {
		a.t.addLine("", file, pos{rank: parentRank, off: off}, line)
		return
	}
	a.t.addLine(ag.ID, file, pos{rank: subAgentRank(ag.ID, file), off: off}, line)
}

func (a *testAgg) finish() []Agent {
	agents, _, _ := a.t.finish(a.agents)
	return agents
}

func (a *testAgg) diag() Diagnostics {
	_, d, _ := a.t.finish(a.agents)
	return d
}

func (a *testAgg) versions() []string {
	_, _, v := a.t.finish(a.agents)
	return v
}

func twoAgents() *testAgg {
	return newTestAgg([]Agent{{ID: "main", Main: true}, {ID: "a1", Type: "coder", SpawnDepth: 1, State: StateFinished}})
}

func TestAggregateLastLineWins(t *testing.T) {
	a := newTestAgg([]Agent{{ID: "main", Main: true}})
	a.addLine(0, "p.jsonl", 0, msgLine("m1", "x", "", 1, 8, 2, 3))
	a.addLine(0, "p.jsonl", 100, msgLine("m1", "x", "", 4, 120, 5, 6))
	a.addLine(0, "p.jsonl", 200, msgLine("m1", "x", "end_turn", 7, 340, 8, 9))
	got := a.finish()
	if len(got) != 1 {
		t.Fatalf("agents = %d", len(got))
	}
	if want := (Tokens{Input: 7, Output: 340, CacheRead: 8, CacheWrite: 9}); got[0].Tokens != want {
		t.Fatalf("tokens = %+v, want %+v (all four replaced, not added)", got[0].Tokens, want)
	}
	if got[0].Messages != 1 || got[0].PartialMessages != 0 || got[0].PartialOutput() {
		t.Fatalf("messages=%d partial=%d", got[0].Messages, got[0].PartialMessages)
	}
}

func TestAggregateDistinctIDsAdd(t *testing.T) {
	a := newTestAgg([]Agent{{ID: "main", Main: true}})
	a.addLine(0, "p.jsonl", 0, msgLine("m1", "x", "end_turn", 1, 2, 3, 4))
	a.addLine(0, "p.jsonl", 50, msgLine("m2", "x", "end_turn", 10, 20, 30, 40))
	got := a.finish()[0]
	if want := (Tokens{11, 22, 33, 44}); got.Tokens != want || got.Messages != 2 {
		t.Fatalf("got %+v messages %d", got.Tokens, got.Messages)
	}
}

func TestAggregateCrossFileDuplicate(t *testing.T) {
	a := twoAgents()
	a.addLine(0, "p.jsonl", 0, msgLine("m1", "x", "end_turn", 1, 1, 1, 1))
	a.addLine(1, "agent-a1.jsonl", 0, msgLine("m1", "x", "end_turn", 5, 5, 5, 5))
	a.addLine(0, "p.jsonl", 90, msgLine("m0", "x", "end_turn", 2, 2, 2, 2))
	got := a.finish()
	if got[0].Messages != 1 || got[0].Tokens != (Tokens{2, 2, 2, 2}) {
		t.Fatalf("parent = %+v, want only m0", got[0])
	}
	if got[1].Messages != 1 || got[1].Tokens != (Tokens{5, 5, 5, 5}) {
		t.Fatalf("sub-agent = %+v, want m1 attributed to the last file", got[1])
	}
	if a.diag().CrossFileDuplicates != 1 {
		t.Fatalf("CrossFileDuplicates = %d, want 1", a.diag().CrossFileDuplicates)
	}
}

func TestAggregateSameFileRepeatIsNotCrossFile(t *testing.T) {
	a := twoAgents()
	a.addLine(0, "p.jsonl", 0, msgLine("m1", "x", "", 1, 1, 1, 1))
	a.addLine(0, "p.jsonl", 9, msgLine("m1", "x", "end_turn", 1, 2, 1, 1))
	a.finish()
	if a.diag().CrossFileDuplicates != 0 {
		t.Fatalf("CrossFileDuplicates = %d, want 0", a.diag().CrossFileDuplicates)
	}
}

func TestAggregateNoIDCountedOnceEach(t *testing.T) {
	a := newTestAgg([]Agent{{ID: "main", Main: true}})
	// Same content, different offsets: two messages, never merged.
	a.addLine(0, "p.jsonl", 0, msgLine("", "x", "end_turn", 1, 2, 3, 4))
	a.addLine(0, "p.jsonl", 80, msgLine("", "x", "end_turn", 1, 2, 3, 4))
	got := a.finish()[0]
	if got.Messages != 2 || got.Tokens != (Tokens{2, 4, 6, 8}) {
		t.Fatalf("messages=%d tokens=%+v, want 2 and doubled", got.Messages, got.Tokens)
	}
	if a.diag().NoMessageID != 2 {
		t.Fatalf("NoMessageID = %d, want 2", a.diag().NoMessageID)
	}
}

func TestAggregateNoIDSameOffsetDifferentFiles(t *testing.T) {
	a := twoAgents()
	a.addLine(0, "p.jsonl", 0, msgLine("", "x", "end_turn", 1, 1, 1, 1))
	a.addLine(1, "agent-a1.jsonl", 0, msgLine("", "x", "end_turn", 1, 1, 1, 1))
	got := a.finish()
	if got[0].Messages != 1 || got[1].Messages != 1 {
		t.Fatalf("messages = %d/%d, want 1/1 (key includes the file)", got[0].Messages, got[1].Messages)
	}
	if a.diag().CrossFileDuplicates != 0 {
		t.Fatalf("CrossFileDuplicates = %d, want 0", a.diag().CrossFileDuplicates)
	}
}

func TestAggregatePartialMessages(t *testing.T) {
	tests := []struct {
		name  string
		lines [][]byte
		want  int
	}{
		{"last line null", [][]byte{msgLine("m", "x", "", 1, 1, 0, 0)}, 1},
		{"partial then completed", [][]byte{msgLine("m", "x", "", 1, 1, 0, 0), msgLine("m", "x", "end_turn", 1, 9, 0, 0)}, 0},
		{"completed then null last", [][]byte{msgLine("m", "x", "end_turn", 1, 1, 0, 0), msgLine("m", "x", "", 1, 2, 0, 0)}, 1},
		{"two messages one partial", [][]byte{msgLine("a", "x", "end_turn", 1, 1, 0, 0), msgLine("b", "x", "", 1, 1, 0, 0)}, 1},
		{"no id, null stop", [][]byte{msgLine("", "x", "", 1, 1, 0, 0), msgLine("", "x", "", 1, 1, 0, 0)}, 2},
	}
	for _, tt := range tests {
		for _, main := range []bool{true, false} { // orchestrator included
			t.Run(fmt.Sprintf("%s/main=%v", tt.name, main), func(t *testing.T) {
				a := newTestAgg([]Agent{{ID: "x", Main: main}})
				for i, l := range tt.lines {
					a.addLine(0, "f.jsonl", int64(i*100), l)
				}
				got := a.finish()[0]
				if got.PartialMessages != tt.want || got.PartialOutput() != (tt.want > 0) {
					t.Fatalf("PartialMessages = %d, want %d", got.PartialMessages, tt.want)
				}
			})
		}
	}
}

func TestAggregateModels(t *testing.T) {
	a := newTestAgg([]Agent{{ID: "main", Main: true}})
	a.addLine(0, "p.jsonl", 0, msgLine("m1", "opus", "end_turn", 1, 1, 0, 0))
	a.addLine(0, "p.jsonl", 10, msgLine("m2", "sonnet", "end_turn", 1, 1, 0, 0))
	a.addLine(0, "p.jsonl", 20, msgLine("m3", "<synthetic>", "stop_sequence", 0, 0, 0, 0))
	a.addLine(0, "p.jsonl", 30, msgLine("m4", "opus", "end_turn", 1, 1, 0, 0))
	got := a.finish()[0].Models
	if want := []string{"opus", "sonnet"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Models = %v, want %v", got, want)
	}
}

func TestAggregateSyntheticExcluded(t *testing.T) {
	a := newTestAgg([]Agent{{ID: "main", Main: true}})
	a.addLine(0, "p.jsonl", 0, msgLine("s1", "<synthetic>", "stop_sequence", 100, 100, 100, 100))
	got := a.finish()[0]
	if got.Tokens != (Tokens{}) || got.Messages != 0 || len(got.Models) != 0 {
		t.Fatalf("synthetic counted: %+v", got)
	}
	if a.diag().Degraded() {
		t.Fatalf("synthetic must not degrade: %+v", a.diag())
	}
}

func TestAggregateLineDiagnostics(t *testing.T) {
	a := newTestAgg([]Agent{{ID: "main", Main: true}})
	a.addLine(0, "p.jsonl", 0, []byte(`not json`))
	a.addLine(0, "p.jsonl", 10, []byte(`{"message":{"id":"n","model":"x"}}`))
	a.addLine(0, "p.jsonl", 20, []byte(`{"type":"queue-operation"}`))
	got := a.finish()[0]
	if a.diag().UnreadableLines != 1 || a.diag().NoUsage != 1 {
		t.Fatalf("diag = %+v, want 1 unreadable and 1 no-usage", a.diag())
	}
	if got.Messages != 0 || got.Tokens != (Tokens{}) {
		t.Fatalf("agent = %+v", got)
	}
}

func TestAggregateKeepsAgentIdentity(t *testing.T) {
	a := twoAgents()
	a.addLine(1, "agent-a1.jsonl", 0, msgLine("m", "x", "end_turn", 1, 1, 0, 0))
	got := a.finish()[1]
	if got.ID != "a1" || got.Type != "coder" || got.SpawnDepth != 1 || got.State != StateFinished || got.Main {
		t.Fatalf("identity lost: %+v", got)
	}
}

func TestAggregateSaturatingTotals(t *testing.T) {
	a := newTestAgg([]Agent{{ID: "main", Main: true}})
	a.addLine(0, "p.jsonl", 0, msgLine("a", "x", "end_turn", maxI, 0, 0, 0))
	a.addLine(0, "p.jsonl", 10, msgLine("b", "x", "end_turn", maxI, 0, 0, 0))
	if got := a.finish()[0].Tokens.Input; got != maxI {
		t.Fatalf("Input = %d, want saturated MaxInt64", got)
	}
}

func TestPartialOutput(t *testing.T) {
	if (Agent{}).PartialOutput() || !(Agent{PartialMessages: 1}).PartialOutput() {
		t.Fatal("PartialOutput must be PartialMessages > 0")
	}
}

func TestUsageTotal(t *testing.T) {
	u := Usage{Agents: []Agent{
		{Tokens: Tokens{1, 2, 3, 4}},
		{Tokens: Tokens{10, 20, 30, 40}},
	}}
	if got, want := u.Total(), (Tokens{11, 22, 33, 44}); got != want {
		t.Fatalf("Total() = %+v, want %+v", got, want)
	}
	if (Usage{}).Total() != (Tokens{}) {
		t.Fatal("empty usage must total zero")
	}
	sat := Usage{Agents: []Agent{{Tokens: Tokens{Input: maxI}}, {Tokens: Tokens{Input: maxI}}}}
	if got := sat.Total().Input; got != maxI {
		t.Fatalf("Input = %d, want saturated", got)
	}
}

func TestDiagnosticsDegraded(t *testing.T) {
	tests := []struct {
		name string
		d    Diagnostics
		want bool
	}{
		{"zero", Diagnostics{}, false},
		{"UnreadableLines", Diagnostics{UnreadableLines: 1}, true},
		{"OversizedLines", Diagnostics{OversizedLines: 1}, true},
		{"NoUsage", Diagnostics{NoUsage: 1}, true},
		{"NoMessageID", Diagnostics{NoMessageID: 1}, true},
		{"CrossFileDuplicates", Diagnostics{CrossFileDuplicates: 1}, true},
		{"MetaMissing", Diagnostics{MetaMissing: 1}, true},
		{"MetaUnreadable", Diagnostics{MetaUnreadable: 1}, true},
		{"TruncatedFiles", Diagnostics{TruncatedFiles: []string{"a.jsonl"}}, true},
		{"Files", Diagnostics{Files: []FileIssue{{File: "a.jsonl", Kind: IssueOversized}}}, true},
		{"empty non-nil slices", Diagnostics{TruncatedFiles: []string{}, Files: []FileIssue{}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.Degraded(); got != tt.want {
				t.Fatalf("Degraded() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Attribution is per file: the same id twice in one file is a repeat even if
// the caller names two agents for it.
func TestAggregateCrossFileDependsOnlyOnFile(t *testing.T) {
	a := twoAgents()
	a.addLine(0, "p.jsonl", 0, msgLine("m1", "x", "", 1, 1, 1, 1))
	a.addLine(1, "p.jsonl", 9, msgLine("m1", "x", "end_turn", 1, 2, 1, 1))
	a.finish()
	if a.diag().CrossFileDuplicates != 0 {
		t.Fatalf("CrossFileDuplicates = %d, want 0 (same file)", a.diag().CrossFileDuplicates)
	}
}
