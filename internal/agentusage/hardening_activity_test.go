package agentusage

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// DEVDESK-46 review fixes: bounded tool table, bounded keys, per-line dedup,
// and a content decoder that keeps nothing but tool_use type and id.

func distinctToolLines(n int) []tline {
	ls := make([]tline, 0, n)
	for i := range n {
		ls = append(ls, pl(int64(i)*10, toolLine(fmt.Sprintf("m%d", i), 0, fmt.Sprintf("t%d", i), "")))
	}
	return ls
}

// Past maxToolEntries the table stops growing and the surplus is counted, so
// distinct calls are not lost.
func TestToolEntriesAreCapped(t *testing.T) {
	if testing.Short() {
		t.Skip("builds more than maxToolEntries lines")
	}
	n := maxToolEntries + 25
	tb := feed(distinctToolLines(n))
	if len(tb.tools) > maxToolEntries {
		t.Errorf("len(tools) = %d, want at most %d", len(tb.tools), maxToolEntries)
	}
	if got := mainOf(t, tb).ToolCalls; got != n {
		t.Errorf("ToolCalls = %d, want %d", got, n)
	}
}

// The cap holds across merges and the total does not depend on their order.
func TestToolEntriesCapHoldsAcrossMerges(t *testing.T) {
	if testing.Short() {
		t.Skip("builds more than maxToolEntries lines")
	}
	n := maxToolEntries + 25
	all := distinctToolLines(n)
	half := n / 2
	build := func(first, second []tline) *table {
		tb := feed(first)
		tb.merge(feed(second))
		return tb
	}
	for name, tb := range map[string]*table{
		"merge(A,B)": build(all[:half], all[half:]),
		"merge(B,A)": build(all[half:], all[:half]),
	} {
		if len(tb.tools) > maxToolEntries {
			t.Errorf("%s: len(tools) = %d, want at most %d", name, len(tb.tools), maxToolEntries)
		}
		if got := mainOf(t, tb).ToolCalls; got != n {
			t.Errorf("%s: ToolCalls = %d, want %d", name, got, n)
		}
	}
}

// A huge tool id or message id is stored in bounded form, and two ids that
// share their first maxLabelLen bytes stay distinct.
func TestLongToolIDsAreBoundedWithoutCollision(t *testing.T) {
	prefix := strings.Repeat("x", maxLabelLen+500)
	tb := feed([]tline{
		pl(0, toolLine(prefix+"M1", 0, prefix+"a", "")),
		pl(1000, toolLine(prefix+"M1", 0, prefix+"b", "")),
		pl(2000, toolLine(prefix+"M1", 0, prefix+"a", "")), // repeat
	})
	if got := mainOf(t, tb).ToolCalls; got != 2 {
		t.Errorf("ToolCalls = %d, want 2", got)
	}
	for k := range tb.tools {
		if len(k.id) > maxLabelLen || len(k.msg) > maxLabelLen {
			t.Errorf("stored key too long: id %d bytes, msg %d bytes", len(k.id), len(k.msg))
		}
	}
}

// Without a message id, the same tool_use id twice in one line is one call.
func TestToolCallsNoMessageIDSameIDInOneLine(t *testing.T) {
	b := blocksLine("assistant", "", 0, "", tuBlock("t1"), tuBlock("t1"), tuBlock("t2"))
	if got := mainOf(t, feed([]tline{pl(0, b)})).ToolCalls; got != 2 {
		t.Errorf("ToolCalls = %d, want 2", got)
	}
	// Blocks with no id cannot be told apart: each counts.
	b = blocksLine("assistant", "", 0, "", tuBlock(""), tuBlock(""))
	if got := mainOf(t, feed([]tline{pl(0, b)})).ToolCalls; got != 2 {
		t.Errorf("ToolCalls (no ids) = %d, want 2", got)
	}
}

// wireBlocks keeps tool_use blocks only, with their position; text and tool
// output are decoded into nothing.
func TestWireBlocksKeepsOnlyToolUse(t *testing.T) {
	var bs wireBlocks
	in := `[{"type":"text","text":"SECRET"},"str",5,{"type":"tool_use","id":"a","input":{"k":"SECRET"}},{"type":"tool_result","content":"SECRET"},{"type":"tool_use","id":9}]`
	if err := json.Unmarshal([]byte(in), &bs); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(bs) != 2 || bs[0].idx != 3 || bs[0].ID != "a" || bs[1].idx != 5 || bs[1].ID != "" {
		t.Errorf("wireBlocks = %+v", bs)
	}
	for _, in := range []string{`{"type":"tool_use"}`, `5`, `null`, `"s"`} {
		var b wireBlocks
		if err := json.Unmarshal([]byte(in), &b); err != nil || len(b) != 0 {
			t.Errorf("Unmarshal(%s) = %+v, %v; want no block, no error", in, b, err)
		}
	}
}

// trimTools selects the evicted entries in one pass when the excess is small
// and by sorting when it is large; both must keep exactly the maxToolEntries
// smallest positions and count the rest per agent.
func TestTrimToolsKeepsTheSmallestPositions(t *testing.T) {
	if testing.Short() {
		t.Skip("builds more than maxToolEntries entries")
	}
	for _, excess := range []int{1, 7, maxSelectInsertion, maxSelectInsertion + 1, 1000} {
		tb := newTable()
		n := maxToolEntries + excess
		// Insert in a scrambled order so neither path relies on arrival order.
		for i := range n {
			j := (i * 7919) % n
			agent := "a"
			if j%2 == 1 {
				agent = "b"
			}
			k := toolKey{msg: fmt.Sprintf("m%d", j), id: fmt.Sprintf("t%d", j)}
			tb.tools[k] = toolSeen{p: pos{rank: "0", off: int64(j)}, agent: agent}
		}
		tb.trimTools()
		if len(tb.tools) != maxToolEntries {
			t.Fatalf("excess %d: len(tools) = %d, want %d", excess, len(tb.tools), maxToolEntries)
		}
		if got := tb.extraTools["a"] + tb.extraTools["b"]; got != excess {
			t.Errorf("excess %d: extraTools total = %d", excess, got)
		}
		for k, s := range tb.tools {
			if s.p.off >= int64(maxToolEntries) {
				t.Fatalf("excess %d: kept %v at offset %d, past the smallest %d", excess, k, s.p.off, maxToolEntries)
			}
		}
		// A second trim at the cap changes nothing.
		before := tb.extraTools["a"] + tb.extraTools["b"]
		tb.trimTools()
		if after := tb.extraTools["a"] + tb.extraTools["b"]; after != before {
			t.Errorf("excess %d: trim at the cap evicted %d entries", excess, after-before)
		}
	}
}
