package agentusage

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// DEVDESK-46 step 2: tool calls and activity span per agent. Transcripts are
// built inline; every value is invented.

// toolLine is a streamed assistant line: one tool_use block per line, as
// Claude Code writes them. ts "" writes no timestamp; msgID "" no message id;
// toolID "" a tool_use without id.
func toolLine(msgID string, abi int, toolID, ts string) []byte {
	return blocksLine("assistant", msgID, abi, ts, tuBlock(toolID))
}

func tuBlock(toolID string) string {
	if toolID == "" {
		return `{"type":"tool_use","name":"X","input":{}}`
	}
	return fmt.Sprintf(`{"type":"tool_use","id":%q,"name":"X","input":{}}`, toolID)
}

// blocksLine writes a line of root type typ holding the given content blocks
// and a usage object.
func blocksLine(typ, msgID string, abi int, ts string, blocks ...string) []byte {
	var parts []string
	parts = append(parts, fmt.Sprintf(`"type":%q`, typ), fmt.Sprintf(`"apiBlockIndex":%d`, abi))
	if ts != "" {
		parts = append(parts, fmt.Sprintf(`"timestamp":%q`, ts))
	}
	id := ""
	if msgID != "" {
		id = fmt.Sprintf(`"id":%q,`, msgID)
	}
	parts = append(parts, fmt.Sprintf(`"message":{%s"model":"m","stop_reason":null,"content":[%s],"usage":{"input_tokens":1,"output_tokens":1}}`,
		id, strings.Join(blocks, ",")))
	return []byte("{" + strings.Join(parts, ",") + "}")
}

// blobLine replays tool_use blocks the way an api-request-blob line does.
func blobLine(toolIDs ...string) []byte {
	var bs []string
	for _, id := range toolIDs {
		bs = append(bs, tuBlock(id))
	}
	return []byte(`{"type":"api-request-blob","timestamp":null,"message":{"content":[` + strings.Join(bs, ",") + `]}}`)
}

func userLine(ts string) []byte {
	return []byte(fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"x","content":"out"}]}}`, ts))
}

func at(h, m, s int) string { return fmt.Sprintf("2026-10-24T%02d:%02d:%02dZ", h, m, s) }

func mainOf(t *testing.T, tb *table, subs ...string) Agent {
	t.Helper()
	return result(t, tb, tableAgents(subs...)).Agents[0]
}

func TestToolCallsCount(t *testing.T) {
	tests := []struct {
		name  string
		lines [][]byte
		want  int
	}{
		{"same tool id in two messages", [][]byte{toolLine("m1", 0, "t1", ""), toolLine("m2", 0, "t1", "")}, 2},
		{"same (msg, tool id) on three lines", [][]byte{toolLine("m1", 0, "t1", ""), toolLine("m1", 0, "t1", ""), toolLine("m1", 0, "t1", "")}, 1},
		{"two tool ids in one message", [][]byte{toolLine("m1", 0, "t1", ""), toolLine("m1", 1, "t2", "")}, 2},
		{"duplicate block inside one line", [][]byte{blocksLine("assistant", "m1", 0, "", tuBlock("t1"), tuBlock("t1"))}, 1},
		{"no tool id: same (msg, abi, idx) twice", [][]byte{toolLine("m1", 0, "", ""), toolLine("m1", 0, "", "")}, 1},
		{"no tool id: two abi", [][]byte{toolLine("m1", 0, "", ""), toolLine("m1", 1, "", "")}, 2},
		{"no message id: two tool_use in one line", [][]byte{blocksLine("assistant", "", 0, "", tuBlock("t1"), tuBlock("t2"))}, 2},
		{"api-request-blob ignored", [][]byte{toolLine("m1", 0, "t1", ""), blobLine("t1", "t2", "t3")}, 1},
		{"api-request-blob alone", [][]byte{blobLine("t1", "t2")}, 0},
		{"user tool results ignored", [][]byte{userLine(at(10, 0, 0)), userLine(at(10, 0, 1))}, 0},
		{"attachment with toolUses 99 ignored", [][]byte{
			[]byte(`{"type":"attachment","timestamp":"` + at(10, 0, 0) + `","attachment":{"type":"queued_command","usage":{"toolUses":99}}}`),
			toolLine("m1", 0, "t1", ""),
		}, 1},
		{"synthetic assistant never counts", [][]byte{[]byte(`{"type":"assistant","message":{"id":"m1","model":"<synthetic>","content":[{"type":"tool_use","id":"t1"}],"usage":{"input_tokens":0,"output_tokens":0}}}`)}, 0},
		{"assistant with an id but no usage still counts", [][]byte{[]byte(`{"type":"assistant","message":{"id":"m1","model":"m","content":[{"type":"tool_use","id":"t1"}]}}`)}, 1},
		{"unreadable line counts nothing", [][]byte{[]byte(`{"type":"assistant","message":{"id":"m1","model":"m","content":[{"type":"tool_use","id":"t1"}],"usage":{"input_tokens":-1}}}`)}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ls []tline
			for i, b := range tt.lines {
				ls = append(ls, pl(int64(i*1000), b))
			}
			if got := mainOf(t, feed(ls)).ToolCalls; got != tt.want {
				t.Errorf("ToolCalls = %d, want %d", got, tt.want)
			}
		})
	}
}

// Without a message id each line is its own event: the same bytes at two
// offsets are two calls, and a plain counter stays exact.
func TestToolCallsWithoutMessageID(t *testing.T) {
	b := blocksLine("assistant", "", 0, "", tuBlock("t1"), tuBlock("t2"))
	got := mainOf(t, feed([]tline{pl(0, b), pl(5000, b)})).ToolCalls
	if got != 4 {
		t.Errorf("ToolCalls = %d, want 4", got)
	}
}

// Criterion 4: 5 distinct tool_use blocks in a sub-agent transcript show 5,
// whatever repeats, replays or answers surround them.
func TestToolCallsCriterion4(t *testing.T) {
	lines := [][]byte{
		toolLine("m1", 0, "toolu_1", at(10, 1, 0)),
		toolLine("m2", 0, "toolu_2", at(10, 2, 0)),
		toolLine("m2", 0, "toolu_2", at(10, 2, 0)), // streaming repeat
		userLine(at(10, 3, 0)),
		toolLine("m3", 0, "toolu_3", at(10, 4, 0)),
		blobLine("toolu_1", "toolu_2", "toolu_3"), // replay
		userLine(at(10, 5, 0)),
		toolLine("m4", 0, "toolu_4", at(10, 6, 0)),
		toolLine("m4", 0, "toolu_4", at(10, 6, 0)), // streaming repeat
		userLine(at(10, 7, 0)),
		toolLine("m5", 0, "toolu_5", at(10, 7, 30)),
	}
	var ls []tline
	for i, b := range lines {
		ls = append(ls, sl("b01", int64(i*1000), b))
	}
	ls = append(ls, pl(0, toolLine("p1", 0, "toolu_p", at(10, 0, 0))))
	out := result(t, feed(ls), tableAgents("b01")).Agents
	if out[1].ToolCalls != 5 {
		t.Errorf("sub-agent ToolCalls = %d, want 5", out[1].ToolCalls)
	}
	if out[0].ToolCalls != 1 {
		t.Errorf("orchestrator ToolCalls = %d, want 1", out[0].ToolCalls)
	}
}

// A message seen in two files is counted once, for the later position (the
// sub-agent), like its tokens.
func TestToolCallsCrossFileCountedOnce(t *testing.T) {
	b := toolLine("m1", 0, "t1", "")
	out := result(t, feed([]tline{pl(0, b), sl("a1", 0, b)}), tableAgents("a1")).Agents
	if out[0].ToolCalls != 0 || out[1].ToolCalls != 1 {
		t.Errorf("ToolCalls main=%d sub=%d, want 0 and 1", out[0].ToolCalls, out[1].ToolCalls)
	}
}

// Past maxNoIDEntries lines without message id the counts stay exact.
func TestToolCallsExactPastTheNoIDCap(t *testing.T) {
	if testing.Short() {
		t.Skip("builds more than maxNoIDEntries lines")
	}
	n := maxNoIDEntries + 25
	tb := newTable()
	b := blocksLine("assistant", "", 0, "", tuBlock("t1"))
	for i := range n {
		tb.addLine("", "p.jsonl", parentPos(int64(i)*10), b)
	}
	if got := mainOf(t, tb).ToolCalls; got != n {
		t.Errorf("ToolCalls = %d, want %d", got, n)
	}
}

func TestActivitySpan(t *testing.T) {
	tests := []struct {
		name        string
		lines       [][]byte
		first, last string // "" = zero
		dur         time.Duration
	}{
		{"out of order timestamps", [][]byte{userLine(at(10, 5, 0)), userLine(at(10, 1, 0)), userLine(at(10, 9, 0))}, at(10, 1, 0), at(10, 9, 0), 8 * time.Minute},
		{"single line", [][]byte{userLine(at(10, 5, 0))}, at(10, 5, 0), at(10, 5, 0), 0},
		{"no timestamp at all", [][]byte{toolLine("m1", 0, "t1", ""), []byte(`{"type":"mode","mode":"x"}`)}, "", "", 0},
		{"null timestamps ignored", [][]byte{blobLine("t1"), userLine(at(10, 2, 0))}, at(10, 2, 0), at(10, 2, 0), 0},
		{"synthetic line carries a timestamp", [][]byte{
			[]byte(`{"type":"assistant","timestamp":"` + at(10, 3, 0) + `","message":{"id":"m","model":"<synthetic>","usage":{"input_tokens":0,"output_tokens":0}}}`),
			userLine(at(10, 1, 0)),
		}, at(10, 1, 0), at(10, 3, 0), 2 * time.Minute},
		{"offset spelling is normalised", [][]byte{
			[]byte(`{"type":"user","timestamp":"2026-10-24T12:00:00+02:00"}`), userLine(at(10, 30, 0)),
		}, at(10, 0, 0), at(10, 30, 0), 30 * time.Minute},
		{"unreadable line carries no time", [][]byte{
			[]byte(`{"type":"assistant","timestamp":"` + at(11, 0, 0) + `","message":{"id":"m","model":"m","usage":{"input_tokens":-1}}}`),
			userLine(at(10, 0, 0)),
		}, at(10, 0, 0), at(10, 0, 0), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ls []tline
			for i, b := range tt.lines {
				ls = append(ls, pl(int64(i*1000), b))
			}
			ag := mainOf(t, feed(ls))
			for _, c := range []struct {
				name      string
				got       time.Time
				want      string
				wantEmpty bool
			}{{"First", ag.FirstActivity, tt.first, tt.first == ""}, {"Last", ag.LastActivity, tt.last, tt.last == ""}} {
				if c.wantEmpty {
					if !c.got.IsZero() {
						t.Errorf("%sActivity = %v, want zero", c.name, c.got)
					}
					continue
				}
				if want := tsAt(t, c.want); !c.got.Equal(want) || c.got.Location() != time.UTC {
					t.Errorf("%sActivity = %v (%v), want %v UTC", c.name, c.got, c.got.Location(), want)
				}
			}
			if got := ag.Duration(); got != tt.dur {
				t.Errorf("Duration = %v, want %v", got, tt.dur)
			}
		})
	}
}

func TestSpansAreKeptPerAgent(t *testing.T) {
	tb := feed([]tline{
		pl(0, userLine(at(10, 0, 0))), pl(100, userLine(at(10, 20, 0))),
		sl("a1", 0, userLine(at(10, 5, 0))), sl("a1", 100, userLine(at(10, 7, 0))),
		sl("a2", 0, toolLine("m1", 0, "t1", "")), // no timestamp
	})
	out := result(t, tb, tableAgents("a1", "a2")).Agents
	if d := out[0].Duration(); d != 20*time.Minute {
		t.Errorf("main Duration = %v", d)
	}
	if d := out[1].Duration(); d != 2*time.Minute {
		t.Errorf("a1 Duration = %v", d)
	}
	if !out[2].FirstActivity.IsZero() || out[2].Duration() != 0 || out[2].ToolCalls != 1 {
		t.Errorf("a2 = %+v, want zero times and 1 tool call", out[2])
	}
}

func TestDurationNeedsBothBounds(t *testing.T) {
	t0 := tsAt(t, at(10, 0, 0))
	tests := []struct {
		name        string
		first, last time.Time
		want        time.Duration
	}{
		{"both", t0, t0.Add(90 * time.Second), 90 * time.Second},
		{"first only", t0, time.Time{}, 0},
		{"last only", time.Time{}, t0, 0},
		{"neither", time.Time{}, time.Time{}, 0},
	}
	for _, tt := range tests {
		if got := (Agent{FirstActivity: tt.first, LastActivity: tt.last}).Duration(); got != tt.want {
			t.Errorf("%s: Duration = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// Tool calls and spans merge order-free, like the rest of the table: the
// table of A+B equals merge(A,B) and merge(B,A).
func TestToolsAndSpansMergeIsOrderFree(t *testing.T) {
	a := []tline{
		pl(0, toolLine("m1", 0, "t1", at(10, 5, 0))),
		pl(1000, toolLine("m1", 1, "t2", at(10, 1, 0))),
		sl("a1", 0, toolLine("m1", 0, "t1", at(10, 3, 0))), // cross-file repeat
	}
	b := []tline{
		pl(2000, toolLine("m2", 0, "t1", at(10, 9, 0))),
		pl(3000, toolLine("m2", 0, "t1", at(10, 9, 0))), // streaming repeat
		sl("a1", 1000, toolLine("m3", 0, "t3", at(10, 2, 0))),
		sl("a1", 2000, blobLine("t1", "t3")),
		sl("a2", 0, userLine(at(10, 4, 0))),
	}
	agents := tableAgents("a1", "a2")
	want := result(t, feed(append(append([]tline{}, a...), b...)), agents)
	if want.Agents[0].ToolCalls == 0 || want.Agents[1].ToolCalls == 0 {
		t.Fatalf("test data counts no tools: %+v", want.Agents)
	}
	ab := feed(a)
	ab.merge(feed(b))
	ba := feed(b)
	ba.merge(feed(a))
	for name, tb := range map[string]*table{"merge(A,B)": ab, "merge(B,A)": ba} {
		if got := result(t, tb, agents); !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %+v\nwant %+v", name, got.Agents, want.Agents)
		}
	}
}

func TestHasLinesWithTimestamps(t *testing.T) {
	tests := []struct {
		name string
		line []byte
		want bool
	}{
		{"timestamped user line", userLine(at(10, 0, 0)), true},
		{"tool_use line", toolLine("m1", 0, "t1", ""), true},
		{"line with neither timestamp nor message", []byte(`{"type":"mode","mode":"x"}`), false},
		{"api-request-blob with null timestamp", blobLine("t1"), false},
		{"user line without timestamp", []byte(`{"type":"user","message":{"role":"user","content":"hi"}}`), false},
	}
	for _, tt := range tests {
		if got := feed([]tline{pl(0, tt.line)}).hasLines(); got != tt.want {
			t.Errorf("%s: hasLines = %v, want %v", tt.name, got, tt.want)
		}
	}
}
