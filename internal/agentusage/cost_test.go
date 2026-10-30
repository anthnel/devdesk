package agentusage

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

// DEVDESK-47 step 2: the cost-state line and Usage.Billed. Everything here is
// invented; only the shape (type, sessionId, totalCostUSD) matches what Claude
// Code writes. Billed is valid only when the LAST complete line of the parent
// transcript is a cost-state of this session with a finite total >= 0.

// costLine is a cost-state line with its newline. rawTotal "" omits
// totalCostUSD; sessionID "" omits sessionId. Extra unknown keys and a
// modelUsage object are always present: only three keys may be read.
func costLine(sessionID, rawTotal string) []byte {
	var parts []string
	parts = append(parts, `"type":"cost-state"`)
	if sessionID != "" {
		parts = append(parts, fmt.Sprintf(`"sessionId":%q`, sessionID))
	}
	if rawTotal != "" {
		parts = append(parts, `"totalCostUSD":`+rawTotal)
	}
	parts = append(parts, `"totalDuration":123,"totalLinesAdded":4,`+
		`"modelUsage":{"claude-haiku-4-5-20251001":{"inputTokens":9,"costUSD":0.5}}`)
	return []byte("{" + strings.Join(parts, ",") + "}\n")
}

func okCost(total string) []byte { return costLine(incSessUUID, total) }

func TestBilledFromTheLastLineOfTheParent(t *testing.T) {
	u1 := ln("u1")
	tests := []struct {
		name   string
		parent []byte
		want   Billed
	}{
		{"cost-state last", cat(u1, okCost("10")), Billed{USD: 10, Known: true}},
		{"zero is a valid total", cat(u1, okCost("0")), Billed{USD: 0, Known: true}},
		{"fractional total", cat(u1, okCost("7.08")), Billed{USD: 7.08, Known: true}},
		{"exponent total", cat(u1, okCost("1.5e1")), Billed{USD: 15, Known: true}},
		{"no cost-state (running session)", cat(u1, ln("u2")), Billed{}},
		{"empty parent", nil, Billed{}},
		{"a usage line after it", cat(u1, okCost("10"), ln("u2")), Billed{USD: 10}},
		{"an unreadable line after it", cat(u1, okCost("10"), []byte("not json {{{\n")), Billed{USD: 10}},
		{"a valid line without usage after it", cat(u1, okCost("10"), []byte("{\"type\":\"user\"}\n")), Billed{USD: 10}},
		{"two identical cost-states", cat(u1, okCost("10"), okCost("10")), Billed{USD: 10, Known: true}},
		{"cumulative cost-states: the last wins", cat(u1, okCost("7.08"), okCost("12.61")), Billed{USD: 12.61, Known: true}},
		{"resumed and closed again", cat(u1, okCost("7.08"), ln("u2"), okCost("12.61")), Billed{USD: 12.61, Known: true}},
		{"resumed, not closed yet", cat(u1, okCost("7.08"), ln("u2")), Billed{USD: 7.08}},
		{"an incomplete tail after it is not a line", cat(u1, okCost("10"), []byte(`{"type":"cost-st`)), Billed{USD: 10, Known: true}},
		{"an incomplete cost-state is not a cost-state", cat(u1, okCost("7"), bytes.TrimSuffix(okCost("10"), []byte("\n"))), Billed{USD: 7, Known: true}},
		{"another session's id", cat(u1, costLine("11111111-1111-4111-8111-111111111111", "10")), Billed{USD: 10}},
		{"no sessionId", cat(u1, costLine("", "10")), Billed{USD: 10, Known: true}}, // plan H5: absent is accepted
		{"total absent", cat(u1, costLine(incSessUUID, "")), Billed{}},
		{"total is a string", cat(u1, okCost(`"10"`)), Billed{}},
		{"total is null", cat(u1, okCost("null")), Billed{}},
		{"total is a bool", cat(u1, okCost("true")), Billed{}},
		{"total is an object", cat(u1, okCost(`{"a":1}`)), Billed{}},
		{"negative total", cat(u1, okCost("-0.01")), Billed{}},
		{"negative zero is shown as zero", cat(u1, okCost("-0")), Billed{USD: 0, Known: true}},
		{"overflowing total", cat(u1, okCost("1e999")), Billed{}},
		{"NaN (not JSON)", cat(u1, okCost("NaN")), Billed{}},
		{"last cost-state invalid: no fallback to an earlier valid one", cat(u1, okCost("10"), okCost("-1")), Billed{}},
		{"valid after an invalid one", cat(u1, okCost("-1"), okCost("10")), Billed{USD: 10, Known: true}},
		{"cost-state only", okCost("3"), Billed{USD: 3, Known: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSess(t)
			s.put(s.parent(), tt.parent)
			got, err := ParseSession(s.located())
			if err != nil {
				t.Fatal(err)
			}
			if got.Billed.Known != tt.want.Known {
				t.Errorf("Billed.Known = %v, want %v", got.Billed.Known, tt.want.Known)
			}
			if tt.want.Known && got.Billed.USD != tt.want.USD {
				t.Errorf("Billed.USD = %v, want %v", got.Billed.USD, tt.want.USD)
			}
			if !tt.want.Known && got.Billed.USD != 0 {
				t.Errorf("Billed.USD = %v with Known false, want 0 (no figure leaks while unknown)", got.Billed.USD)
			}
			// Aggregate over the same files says the same.
			a := NewAggregate(s.parent())
			readApply(t, a)
			assertMatchesParse(t, a, s)
		})
	}
}

// A cost-state is not a usage line and not a broken one: tokens, messages and
// diagnostics are what they are without it.
func TestCostStateChangesNoCountAndNoDiagnostic(t *testing.T) {
	for _, total := range []string{"10", "-1", `"x"`, ""} {
		t.Run("total="+total, func(t *testing.T) {
			s := newSess(t)
			s.put(s.parent(), cat(ln("u1"), ln("u2")))
			base, err := ParseSession(s.located())
			if err != nil {
				t.Fatal(err)
			}
			s.add(s.parent(), costLine(incSessUUID, total))
			got, err := ParseSession(s.located())
			if err != nil {
				t.Fatal(err)
			}
			if got.Diagnostics.Degraded() {
				t.Errorf("Diagnostics = %+v, want not degraded", got.Diagnostics)
			}
			if fmt.Sprintf("%+v", got.Diagnostics) != fmt.Sprintf("%+v", base.Diagnostics) {
				t.Errorf("Diagnostics moved: %+v -> %+v", base.Diagnostics, got.Diagnostics)
			}
			if got.Total() != base.Total() || got.Agents[0].Messages != base.Agents[0].Messages {
				t.Errorf("counts moved: %+v/%d -> %+v/%d", base.Total(), base.Agents[0].Messages, got.Total(), got.Agents[0].Messages)
			}
			if fmt.Sprint(got.Agents[0].Models) != fmt.Sprint(base.Agents[0].Models) || fmt.Sprint(got.Versions) != fmt.Sprint(base.Versions) {
				t.Errorf("models/versions moved: %v %v -> %v %v", base.Agents[0].Models, base.Versions, got.Agents[0].Models, got.Versions)
			}
		})
	}
}

// A cost-state in a sub-agent transcript is ignored: it neither gives Billed
// to a session that has none nor takes it away from one that has.
func TestCostStateInASubAgentIsIgnored(t *testing.T) {
	tests := []struct {
		name   string
		parent []byte
		want   Billed
	}{
		{"parent without one", ln("u1"), Billed{}},
		{"parent with one", cat(ln("u1"), okCost("10")), Billed{USD: 10, Known: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSess(t)
			s.put(s.parent(), tt.parent)
			s.putMeta("a1", metaFinished, 0)
			s.put(s.sub("a1"), cat(ln("s1"), okCost("99")))
			got, err := ParseSession(s.located())
			if err != nil {
				t.Fatal(err)
			}
			if got.Billed.Known != tt.want.Known || (tt.want.Known && got.Billed.USD != tt.want.USD) {
				t.Errorf("Billed = %+v, want %+v", got.Billed, tt.want)
			}
			a := NewAggregate(s.parent())
			readApply(t, a)
			assertMatchesParse(t, a, s)
		})
	}
}

// Privacy: of a cost-state line only type, sessionId and totalCostUSD are
// read. A sentinel in an unknown key, in modelUsage and in a model name must
// not reach any rendering of Usage, and modelUsage names no model of the agent.
func TestCostStateLeaksNothing(t *testing.T) {
	s := newSess(t)
	line := fmt.Sprintf(`{"type":"cost-state","sessionId":%q,"totalCostUSD":4.2,"SENTINEL-DO-NOT-LEAK":"x",`+
		`"modelUsage":{"SENTINEL-DO-NOT-LEAK-model":{"inputTokens":777,"outputTokens":888,"costUSD":1}},"cwd":"/SENTINEL-DO-NOT-LEAK"}`+"\n", incSessUUID)
	s.put(s.parent(), cat(ln("u1"), []byte(line)))
	u, err := ParseSession(s.located())
	if err != nil {
		t.Fatal(err)
	}
	if !u.Billed.Known || u.Billed.USD != 4.2 {
		t.Fatalf("Billed = %+v, want {4.2 true}", u.Billed)
	}
	for _, out := range []string{fmt.Sprintf("%+v", u), fmt.Sprintf("%#v", u)} {
		if strings.Contains(out, "SENTINEL") {
			t.Errorf("Usage leaks cost-state content: %s", out)
		}
	}
	if m := u.Agents[0].Models; len(m) != 1 || m[0] != "claude-x" {
		t.Errorf("Models = %v, want [claude-x] (modelUsage names no model of the agent)", m)
	}
	if u.Total() != (Tokens{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4}) {
		t.Errorf("Total = %+v: modelUsage counts must not be added", u.Total())
	}
}

// A cost-state has no timestamp. It must not touch the agent's time span, and
// it must not make a quiet session look quiet earlier or later than the file
// activity says: the delta that brings it is a real change (non-Empty, Billed
// appears from it), LastChange follows the file's mtime, and Settled keeps its
// definition.
func TestCostStateDoesNotSkewTimeRangeNorSettled(t *testing.T) {
	setClock(t, actBase.Add(time.Hour))
	s := newSess(t)
	s.putAt(s.parent(), cat(nl(toolLine("m1", 0, "t1", at(10, 1, 0))), nl(toolLine("m2", 0, "t2", at(10, 5, 0)))), actBase)
	a := NewAggregate(s.parent())
	readApply(t, a)
	before := a.Usage().Agents[0]
	if before.FirstActivity.IsZero() || !before.LastActivity.After(before.FirstActivity) {
		t.Fatalf("setup: span = %v .. %v", before.FirstActivity, before.LastActivity)
	}
	if a.Usage().Billed.Known {
		t.Fatal("setup: Billed known before any cost-state")
	}

	s.add(s.parent(), okCost("10"))
	touch(t, s.parent(), actBase.Add(2*time.Minute))
	d := readApply(t, a)

	if d.Empty() {
		t.Error("the delta bringing the cost-state is Empty(): the view would never learn of Billed")
	}
	u := a.Usage()
	if !u.Billed.Known || u.Billed.USD != 10 {
		t.Errorf("Billed = %+v, want {10 true}", u.Billed)
	}
	got := u.Agents[0]
	if !got.FirstActivity.Equal(before.FirstActivity) || !got.LastActivity.Equal(before.LastActivity) || got.Duration() != before.Duration() {
		t.Errorf("span moved: %v..%v -> %v..%v", before.FirstActivity, before.LastActivity, got.FirstActivity, got.LastActivity)
	}
	if got.ToolCalls != before.ToolCalls {
		t.Errorf("ToolCalls = %d, want %d", got.ToolCalls, before.ToolCalls)
	}
	act := a.Activity()
	if want := actBase.Add(2 * time.Minute); !act.LastChange.Equal(want) {
		t.Errorf("LastChange = %v, want the file's mtime %v", act.LastChange, want)
	}
	if act.Settled(actBase.Add(6*time.Minute), actQuiet) {
		t.Error("Settled 4 min after the cost-state, want false (quiet is 5 min)")
	}
	if !act.Settled(actBase.Add(8*time.Minute), actQuiet) {
		t.Error("not Settled 6 min after the cost-state, want true")
	}

	// A cost-state alone gives an agent no time span at all.
	s2 := newSess(t)
	s2.put(s2.parent(), okCost("3"))
	u2, err := ParseSession(s2.located())
	if err != nil {
		t.Fatal(err)
	}
	if m := u2.Agents[0]; !m.FirstActivity.IsZero() || !m.LastActivity.IsZero() || m.Duration() != 0 {
		t.Errorf("span = %v..%v, want none: a cost-state has no timestamp", m.FirstActivity, m.LastActivity)
	}
}

// Declared limit (plan step 2): a cost-state is the one line that makes a
// delta non-Empty without carrying a timestamp, so the view learns of Billed
// on that very delta. A usage line appended after it is a non-Empty delta too,
// and Billed goes back to unknown. The reverse is not true: see
// TestBilledStaleAfterALineWithNoTimestampNorUsage.
func TestBilledGoesUnknownWhenALineFollows(t *testing.T) {
	s := newSess(t)
	s.put(s.parent(), ln("u1"))
	a := NewAggregate(s.parent())
	readApply(t, a)
	s.add(s.parent(), okCost("10"))
	if d := readApply(t, a); d.Empty() || !a.Usage().Billed.Known {
		t.Fatalf("after cost-state: empty=%v billed=%+v", d.Empty(), a.Usage().Billed)
	}
	s.add(s.parent(), ln("u2"))
	if d := readApply(t, a); d.Empty() {
		t.Error("a usage line after the cost-state is an Empty() delta")
	}
	if a.Usage().Billed.Known {
		t.Error("Billed still known after a usage line")
	}
	assertMatchesParse(t, a, s)
	s.add(s.parent(), okCost("11"))
	readApply(t, a)
	if b := a.Usage().Billed; !b.Known || b.USD != 11 {
		t.Errorf("Billed = %+v, want {11 true}", b)
	}
	assertMatchesParse(t, a, s)
}

// Declared limit: a valid JSON line with neither a timestamp nor a message
// appended after a cost-state makes an Empty() delta, yet Aggregate.Apply still
// merges it and a.Usage() is right (Billed unknown). The view only refreshes on
// a non-Empty delta, so it keeps the previous usage (Billed stale) until the
// next non-Empty delta or ctrl+r. Accepted by the plan; kept on purpose.
func TestBilledStaleAfterALineWithNoTimestampNorUsage(t *testing.T) {
	s := newSess(t)
	s.put(s.parent(), cat(ln("u1"), okCost("10")))
	a := NewAggregate(s.parent())
	readApply(t, a)
	if !a.Usage().Billed.Known {
		t.Fatal("Billed unknown after a cost-state")
	}
	s.add(s.parent(), []byte(`{"type":"summary"}`+"\n"))
	d := readApply(t, a)
	if !d.Empty() {
		t.Error("a summary line with no timestamp makes a non-Empty delta")
	}
	if a.Usage().Billed.Known {
		t.Error("Billed still known after a line following the cost-state")
	}
	assertMatchesParse(t, a, s)
}

func TestCostSessionIDShapes(t *testing.T) {
	line := func(sid string) []byte {
		return []byte(`{"type":"cost-state","sessionId":` + sid + `,"totalCostUSD":4}` + "\n")
	}
	tests := []struct {
		name  string
		sid   string
		known bool
	}{
		{"number", "123", false},
		{"object", "{}", false},
		{"array", "[]", false},
		{"bool", "true", false},
		// null is read like an absent sessionId: accepted (plan H5).
		{"null", "null", true},
		{"this session", `"` + incSessUUID + `"`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSess(t)
			s.put(s.parent(), cat(ln("u1"), line(tt.sid)))
			got, err := ParseSession(s.located())
			if err != nil {
				t.Fatal(err)
			}
			if got.Billed.Known != tt.known {
				t.Errorf("Billed.Known = %v, want %v", got.Billed.Known, tt.known)
			}
			a := NewAggregate(s.parent())
			readApply(t, a)
			assertMatchesParse(t, a, s)
		})
	}
}

// An oversized line after the cost-state is a complete line of the parent: it
// moves parentEnd, so the cost-state is no longer the last one.
func TestBilledUnknownAfterAnOversizedLine(t *testing.T) {
	big := []byte(`{"x":"` + strings.Repeat("z", MaxLineBytes+10) + `"}` + "\n")
	s := newSess(t)
	s.put(s.parent(), cat(ln("u1"), okCost("10"), big))
	got, err := ParseSession(s.located())
	if err != nil {
		t.Fatal(err)
	}
	if got.Billed.Known {
		t.Error("Billed known after an oversized line")
	}
	if got.Diagnostics.OversizedLines != 1 {
		t.Errorf("OversizedLines = %d, want 1", got.Diagnostics.OversizedLines)
	}
	a := NewAggregate(s.parent())
	readApply(t, a)
	assertMatchesParse(t, a, s)
}

// The fixture holds a resumed session (two cumulative cost-states, the last
// one wins) and a cost-state in a sub-agent file, which is ignored.
func TestParseSessionCostFixture(t *testing.T) {
	u := parseFixture(t, "v2.1.302-cost")
	if !u.Billed.Known || u.Billed.USD != 12.61 {
		t.Errorf("Billed = %+v, want {12.61 true}", u.Billed)
	}
	if len(u.Agents) != 2 {
		t.Fatalf("agents = %d, want 2", len(u.Agents))
	}
	if got := u.Agents[0].Tokens; got.Input != 30 || got.Output != 15 {
		t.Errorf("orchestrator tokens = %+v, want input 30 output 15", got)
	}
	if out := fmt.Sprintf("%+v", u); strings.Contains(out, "SENTINEL") {
		t.Errorf("Usage leaks the sentinel: %s", out)
	}
}

// The oracle: appended in chunks of 1, 7, 64 bytes or 1 MiB, with partial
// lines at chunk ends, the aggregate equals ParseSession after every read, and
// the final Billed is the expected one.
func TestReadDeltaBilledMatchesParse(t *testing.T) {
	tests := []struct {
		name  string
		lines [][]byte
		want  Billed
	}{
		{"ends with a cost-state", [][]byte{ln("m1"), okCost("5"), ln("m2"), okCost("7.5")}, Billed{USD: 7.5, Known: true}},
		{"cost-state then a normal line", [][]byte{ln("m1"), okCost("5"), ln("m2")}, Billed{}},
		{"cost-state then an unreadable line", [][]byte{ln("m1"), okCost("5"), []byte("garbage\n")}, Billed{}},
		{"invalid last cost-state", [][]byte{ln("m1"), okCost("5"), okCost("-3")}, Billed{}},
		{"other session's cost-state", [][]byte{ln("m1"), costLine("22222222-2222-4222-8222-222222222222", "5")}, Billed{}},
		{"no cost-state", [][]byte{ln("m1"), ln("m2")}, Billed{}},
	}
	for _, tt := range tests {
		for _, size := range []int{1, 7, 64, 1 << 20} {
			t.Run(fmt.Sprintf("%s/chunk %d", tt.name, size), func(t *testing.T) {
				data := cat(tt.lines...)
				data = append(data, `{"type":"cost-state","sessionId":"`...) // a partial line at the end, never completed
				s := newSess(t)
				s.put(s.parent(), nil)
				a := NewAggregate(s.parent())
				readApply(t, a)
				for off := 0; off < len(data); {
					end := min(off+size, len(data))
					s.add(s.parent(), data[off:end])
					off = end
					readApply(t, a)
					assertMatchesParse(t, a, s)
				}
				got := a.Usage().Billed
				if got.Known != tt.want.Known || (tt.want.Known && got.USD != tt.want.USD) {
					t.Errorf("Billed = %+v, want %+v", got, tt.want)
				}
			})
		}
	}
}
