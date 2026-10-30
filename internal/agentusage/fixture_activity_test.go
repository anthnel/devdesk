package agentusage

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// DEVDESK-46 step 3: the testdata/v2.1.302-activity fixture. Every expected
// value is written by hand from the files (see testdata/README.md).

func utc(h, m, s int) time.Time {
	return time.Date(2026, 10, 24, h, m, s, 0, time.UTC)
}

func TestParseSessionActivity(t *testing.T) {
	u := parseFixture(t, "v2.1.302-activity")
	want := []Agent{
		// 2 Agent calls; a queued_command claims toolUses 99 and is ignored;
		// timestamps are 10:00:00, 10:00:30, 10:20:00 and 10:10:00 (out of order);
		// the api-request-blob replaying toolu_p1 has a null timestamp and counts nothing.
		{ID: "", Main: true, State: StateUnknown, Tokens: Tokens{6, 80, 1200, 1000}, Messages: 2,
			ToolCalls: 2, FirstActivity: utc(10, 0, 0), LastActivity: utc(10, 20, 0), Models: []string{"model-alpha"}},
		// 5 distinct tool_use blocks over 7 assistant lines (one message spread
		// over 3 lines, one streamed twice), 2 blobs replaying calls and 2
		// tool results. Span 10:01:00 to 10:07:30 although msg_b2 is
		// written with 10:03:00 then 10:02:30.
		{ID: "b01", Type: "coder", State: StateUnfinished, SpawnDepth: 1, Tokens: Tokens{26, 180, 1400, 50}, Messages: 4,
			ToolCalls: 5, FirstActivity: utc(10, 1, 0), LastActivity: utc(10, 7, 30), Models: []string{"model-beta"}},
		// 1 call and no timestamp anywhere: the duration is unknown.
		{ID: "b02", Type: "tester", State: StateFinished, SpawnDepth: 1, Tokens: Tokens{1, 2, 3, 4}, Messages: 1,
			ToolCalls: 1, Models: []string{"model-gamma"}},
	}
	if len(u.Agents) != len(want) {
		t.Fatalf("agents = %d, want %d: %+v", len(u.Agents), len(want), u.Agents)
	}
	for i, w := range want {
		if !reflect.DeepEqual(u.Agents[i], w) {
			t.Errorf("agent %d = %+v\nwant      %+v", i, u.Agents[i], w)
		}
	}
	if got := u.Agents[1].Duration(); got != 6*time.Minute+30*time.Second {
		t.Errorf("b01 Duration = %v, want 6m30s", got)
	}
	if got := u.Agents[0].Duration(); got != 20*time.Minute {
		t.Errorf("parent Duration = %v, want 20m", got)
	}
	if got := u.Agents[2].Duration(); got != 0 || !u.Agents[2].FirstActivity.IsZero() {
		t.Errorf("b02 Duration = %v, First = %v, want 0 and zero", got, u.Agents[2].FirstActivity)
	}
	if u.Diagnostics.Degraded() {
		t.Errorf("Diagnostics = %+v, want clean", u.Diagnostics)
	}
}

// Criterion 3: counters only. The sentinel sits in a tool input, a thinking
// block, a description, a tool result, a user prompt and one tool_use.id; none of it may reach any rendering of the
// result, and Agent holds no new string.
func TestActivityFixtureNeverLeaks(t *testing.T) {
	u := parseFixture(t, "v2.1.302-activity")
	for _, s := range []string{fmt.Sprintf("%+v", u), fmt.Sprintf("%#v", u), fmt.Sprintf("%v", u.Agents)} {
		if strings.Contains(s, "SENTINEL") || strings.Contains(s, "toolu_") {
			t.Errorf("Usage leaks content or a tool id: %s", s)
		}
	}
	// Same through the incremental path.
	s, _ := cloneSession(t, fixtureSession(t, "v2.1.302-activity"), false)
	a := NewAggregate(s.parent())
	readApply(t, a)
	if out := fmt.Sprintf("%+v %#v", a.Usage(), a.Usage()); strings.Contains(out, "SENTINEL") || strings.Contains(out, "toolu_") {
		t.Errorf("Aggregate leaks: %s", out)
	}
	// Errors: a transcript that cannot be opened names a base name only.
	files := fixtureSession(t, "v2.1.302-activity")
	files.Parent += ".gone"
	if _, err := ParseSession(files); err == nil || strings.Contains(err.Error(), "SENTINEL") || strings.Contains(err.Error(), "toolu_") {
		t.Errorf("error = %v", err)
	}
	// The only new fields of Agent are an int and two times.
	known := map[string]bool{"ID": true, "Main": true, "Type": true, "Models": true, "Tokens": true, "Messages": true,
		"PartialMessages": true, "State": true, "SpawnDepth": true, "key": true}
	timeType := reflect.TypeOf(time.Time{})
	at := reflect.TypeOf(Agent{})
	for i := range at.NumField() {
		f := at.Field(i)
		switch {
		case known[f.Name]:
		case f.Name == "ToolCalls" && f.Type.Kind() == reflect.Int:
		case (f.Name == "FirstActivity" || f.Name == "LastActivity") && f.Type == timeType:
		default:
			t.Errorf("unexpected Agent field %s %s: counters and times only", f.Name, f.Type)
		}
	}
}
