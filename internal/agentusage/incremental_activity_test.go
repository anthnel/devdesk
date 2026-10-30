package agentusage

import (
	"testing"
	"time"
)

// DEVDESK-46 step 2: the new fields obey the incremental oracle (D3) and a
// timestamped line makes a delta non-empty.

func critLines() [][]byte {
	return [][]byte{
		toolLine("m1", 0, "toolu_1", at(10, 1, 0)),
		toolLine("m2", 0, "toolu_2", at(10, 2, 0)),
		toolLine("m2", 0, "toolu_2", at(10, 2, 0)),
		toolLine("m3", 0, "toolu_3", at(10, 4, 0)),
		toolLine("m4", 0, "toolu_4", at(10, 6, 0)),
		toolLine("m4", 0, "toolu_4", at(10, 6, 0)),
		toolLine("m5", 0, "toolu_5", at(10, 7, 30)),
	}
}

func nl(b []byte) []byte { return append(append([]byte{}, b...), '\n') }

// The 7 lines of criterion 4 arrive in three chunks, one cut mid-line: the
// count grows 2, 4, 5 and equals ParseSession each time.
func TestIncrementalToolCallsEqualFull(t *testing.T) {
	s := newSess(t)
	s.put(s.parent(), ln("p1"))
	s.putMeta("b01", metaUnfinished, time.Minute)
	ls := critLines()
	s.put(s.sub("b01"), cat(nl(ls[0]), nl(ls[1])))
	a := NewAggregate(s.parent())
	readApply(t, a)
	assertMatchesParse(t, a, s)
	if got := agentByID(t, a.Usage(), "b01").ToolCalls; got != 2 {
		t.Fatalf("after chunk 1: ToolCalls = %d, want 2", got)
	}

	half := nl(ls[5])
	s.add(s.sub("b01"), cat(nl(ls[2]), nl(ls[3]), nl(ls[4]), half[:len(half)/2]))
	readApply(t, a)
	assertMatchesParse(t, a, s)
	if got := agentByID(t, a.Usage(), "b01").ToolCalls; got != 4 {
		t.Fatalf("after chunk 2: ToolCalls = %d, want 4", got)
	}

	s.add(s.sub("b01"), cat(half[len(half)/2:], nl(ls[6])))
	readApply(t, a)
	assertMatchesParse(t, a, s)
	ag := agentByID(t, a.Usage(), "b01")
	if ag.ToolCalls != 5 {
		t.Errorf("after chunk 3: ToolCalls = %d, want 5", ag.ToolCalls)
	}
	if ag.Duration() != 6*time.Minute+30*time.Second {
		t.Errorf("Duration = %v, want 6m30s", ag.Duration())
	}
}

// A repeated line in a later delta does not count twice.
func TestIncrementalStreamingRepeatAcrossDeltas(t *testing.T) {
	s := newSess(t)
	s.put(s.parent(), nl(toolLine("m1", 0, "t1", at(10, 0, 0))))
	a := NewAggregate(s.parent())
	readApply(t, a)
	s.add(s.parent(), nl(toolLine("m1", 0, "t1", at(10, 0, 0))))
	readApply(t, a)
	assertMatchesParse(t, a, s)
	if got := mainToolCalls(t, a.Usage()); got != 1 {
		t.Errorf("ToolCalls = %d, want 1", got)
	}
}

func mainToolCalls(t *testing.T, u Usage) int {
	t.Helper()
	for _, ag := range u.Agents {
		if ag.Main {
			return ag.ToolCalls
		}
	}
	t.Fatalf("no main agent in %+v", u.Agents)
	return 0
}

// Out-of-order timestamps across deltas: the span is min..max whatever the
// arrival order, and equals the full read.
func TestIncrementalSpanIsOrderFree(t *testing.T) {
	s := newSess(t)
	s.put(s.parent(), nl(userLine(at(10, 5, 0))))
	a := NewAggregate(s.parent())
	readApply(t, a)
	s.add(s.parent(), cat(nl(userLine(at(10, 1, 0))), nl(userLine(at(10, 9, 0))), nl(userLine(at(10, 3, 0)))))
	readApply(t, a)
	assertMatchesParse(t, a, s)
	var main Agent
	for _, ag := range a.Usage().Agents {
		if ag.Main {
			main = ag
		}
	}
	if main.Duration() != 8*time.Minute {
		t.Errorf("Duration = %v, want 8m", main.Duration())
	}
}

// A timestamped line makes the delta non-empty even though it carries no
// usage; a line with neither timestamp nor message stays Empty.
func TestDeltaWithTimestampedLineIsNotEmpty(t *testing.T) {
	tests := []struct {
		name  string
		line  []byte
		empty bool
	}{
		{"timestamped user line", nl(userLine(at(10, 0, 0))), false},
		{"mode line, no timestamp", []byte("{\"type\":\"mode\",\"mode\":\"x\"}\n"), true},
		{"api-request-blob, null timestamp", nl(blobLine("t1")), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newSess(t)
			s.put(s.parent(), ln("p1"))
			a := NewAggregate(s.parent())
			readApply(t, a)
			s.add(s.parent(), tc.line)
			d := readApply(t, a)
			if d.Empty() != tc.empty {
				t.Errorf("Empty = %v, want %v", d.Empty(), tc.empty)
			}
			assertMatchesParse(t, a, s)
		})
	}
}
