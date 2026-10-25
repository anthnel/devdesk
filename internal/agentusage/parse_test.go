package agentusage

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fixtureSession locates the single session of testdata/<name>.
func fixtureSession(t *testing.T, name string) SessionFiles {
	t.Helper()
	parents, err := filepath.Glob(filepath.Join("testdata", name, "projects", "*", "*.jsonl"))
	if err != nil || len(parents) != 1 {
		t.Fatalf("fixture %s: parents = %v, err %v", name, parents, err)
	}
	files, err := LocateSession(parents[0])
	if err != nil {
		t.Fatalf("LocateSession: %v", err)
	}
	return files
}

func parseFixture(t *testing.T, name string) Usage {
	t.Helper()
	u, err := ParseSession(fixtureSession(t, name))
	if err != nil {
		t.Fatalf("ParseSession(%s): %v", name, err)
	}
	return u
}

// Expected values below are computed by hand from testdata/v2.1.302-basic.
func TestParseSessionBasic(t *testing.T) {
	u := parseFixture(t, "v2.1.302-basic")
	if u.SessionID != "00000000-0000-4000-8000-000000000302" {
		t.Errorf("SessionID = %q", u.SessionID)
	}
	want := []Agent{
		{ID: "", Main: true, State: StateUnknown, Type: "", Tokens: Tokens{13, 320, 1200, 1050}, Messages: 2, Models: []string{"model-alpha"}},
		{ID: "a01", Type: "coder", State: StateFinished, SpawnDepth: 1, Tokens: Tokens{9, 140, 1400, 200}, Messages: 2, Models: []string{"model-beta"}},
		{ID: "a02", Type: "tester", State: StateUnfinished, SpawnDepth: 1, Tokens: Tokens{5, 38, 2000, 300}, Messages: 2, PartialMessages: 1, Models: []string{"model-beta"}},
		{ID: "a03", Type: "reviewer", State: StateFinished, SpawnDepth: 2, Tokens: Tokens{6, 90, 40, 10}, Messages: 1, Models: []string{"model-gamma"}},
	}
	// Criterion 1: four agents, four exact totals each.
	if len(u.Agents) != len(want) {
		t.Fatalf("agents = %d, want %d: %+v", len(u.Agents), len(want), u.Agents)
	}
	for i, w := range want {
		if !reflect.DeepEqual(u.Agents[i], w) {
			t.Errorf("agent %d = %+v\nwant      %+v", i, u.Agents[i], w)
		}
	}
	// Criterion 6: only a02 carries a partial output.
	for i, a := range u.Agents {
		if got, want := a.PartialOutput(), a.ID == "a02"; got != want {
			t.Errorf("agent %d (%q) PartialOutput = %v, want %v", i, a.ID, got, want)
		}
	}
	// Criterion 9: x.jsonl under tool-results/ (7777777 output) is not read.
	if got, want := u.Total(), (Tokens{33, 588, 4640, 1560}); got != want || got.Total() != 6821 {
		t.Errorf("Total = %+v (%d), want %+v (6821)", got, got.Total(), want)
	}
	// Criterion 7: nullable usage fields change nothing, and nothing is skipped
	// (the user line without id or usage is ignored, not a NoUsage).
	wantDiag := Diagnostics{}
	if !reflect.DeepEqual(u.Diagnostics, wantDiag) {
		t.Errorf("Diagnostics = %+v, want %+v", u.Diagnostics, wantDiag)
	}
	// Versions: every decodable line counts (2.1.301 only on an ignored line,
	// 2.1.303 only on a <synthetic> one), first appearance order.
	if want := []string{"2.1.301", "2.1.302", "2.1.303"}; !reflect.DeepEqual(u.Versions, want) {
		t.Errorf("Versions = %v, want %v", u.Versions, want)
	}
}

func TestParseSessionNeverLeaksContent(t *testing.T) {
	for _, name := range []string{"v2.1.302-basic", "synthetic-other-version", "damaged"} {
		u := parseFixture(t, name)
		if s := fmt.Sprintf("%+v", u); strings.Contains(s, sentinel) {
			t.Errorf("%s: Usage leaks the sentinel: %s", name, s)
		}
	}
	// The parent cannot be opened: the error must not carry content or the directory.
	_, err := ParseSession(SessionFiles{ID: "x", Parent: filepath.Join(t.TempDir(), sentinel, "x.jsonl")})
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Errorf("error leaks: %v", err)
	}
}

// Criterion 8.
func TestParseSessionOtherVersion(t *testing.T) {
	u := parseFixture(t, "synthetic-other-version")
	if len(u.Agents) != 2 {
		t.Fatalf("agents = %+v", u.Agents)
	}
	if got, want := u.Agents[0].Tokens, (Tokens{12, 24, 36, 48}); got != want || u.Agents[0].Messages != 2 {
		t.Errorf("parent = %+v messages %d, want %+v / 2", got, u.Agents[0].Messages, want)
	}
	x := u.Agents[1]
	if x.ID != "x1" || x.Type != "coder" || x.Tokens != (Tokens{100, 200, 0, 0}) {
		t.Errorf("x1 = %+v", x)
	}
	for _, a := range u.Agents {
		if a.State != StateUnknown {
			t.Errorf("agent %q State = %d, want unknown (meta without finished)", a.ID, a.State)
		}
	}
	if u.Diagnostics.NoUsage != 2 {
		t.Errorf("NoUsage = %d, want 2 (renamed + absent usage)", u.Diagnostics.NoUsage)
	}
	if u.Diagnostics.UnreadableLines != 0 || u.Diagnostics.MetaMissing != 0 || u.Diagnostics.MetaUnreadable != 0 {
		t.Errorf("Diagnostics = %+v", u.Diagnostics)
	}
	if !u.Diagnostics.Degraded() {
		t.Error("Degraded() = false, want true")
	}
	if !reflect.DeepEqual(u.Versions, []string{"9.0.0"}) {
		t.Errorf("Versions = %v, want [9.0.0]", u.Versions)
	}
}

// Criterion 4.
func TestParseSessionDamaged(t *testing.T) {
	files := fixtureSession(t, "damaged")
	u := parseFixture(t, "damaged")
	if len(u.Agents) != 2 {
		t.Fatalf("agents = %+v", u.Agents)
	}
	if got, want := u.Agents[0].Tokens, (Tokens{11, 22, 33, 44}); got != want {
		t.Errorf("parent = %+v, want %+v (lines around the bad one are read)", got, want)
	}
	if got, want := u.Agents[1].Tokens, (Tokens{100, 200, 300, 400}); got != want {
		t.Errorf("d1 = %+v, want %+v (complete line before the cut is read)", got, want)
	}
	d := u.Diagnostics
	if d.UnreadableLines != 1 {
		t.Errorf("UnreadableLines = %d, want 1 (the cut tail is a truncated file, not a line)", d.UnreadableLines)
	}
	if !reflect.DeepEqual(d.TruncatedFiles, []string{"agent-d1.jsonl"}) {
		t.Errorf("TruncatedFiles = %v, want [agent-d1.jsonl] (base name only)", d.TruncatedFiles)
	}
	raw, err := os.ReadFile(files.Parent)
	if err != nil {
		t.Fatal(err)
	}
	wantOff := int64(bytes.Index(raw, []byte("this is not json")))
	wantFiles := []FileIssue{{File: filepath.Base(files.Parent), Offset: wantOff, Kind: IssueUnreadable}}
	if !reflect.DeepEqual(d.Files, wantFiles) {
		t.Errorf("Files = %+v, want %+v", d.Files, wantFiles)
	}
	if !d.Degraded() {
		t.Error("Degraded() = false")
	}
}

func TestParseSessionOversizedLine(t *testing.T) {
	dir := t.TempDir()
	first := string(msgLine("m1", "x", "end_turn", 1, 2, 3, 4))
	const pre, suf = `{"pad":"`, `"}`
	big := pre + strings.Repeat("a", MaxLineBytes+1-len(pre)-len(suf)) + suf
	last := string(msgLine("m2", "x", "end_turn", 10, 20, 30, 40))
	parent := filepath.Join(dir, sessUUID+".jsonl")
	writeFile(t, parent, first+"\n"+big+"\n"+last+"\n")
	u, err := ParseSession(SessionFiles{ID: sessUUID, Parent: parent})
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Agents) == 0 {
		t.Fatal("no agents")
	}
	if got, want := u.Agents[0].Tokens, (Tokens{11, 22, 33, 44}); got != want {
		t.Errorf("tokens = %+v, want %+v (lines around the oversized one are read)", got, want)
	}
	if u.Diagnostics.OversizedLines != 1 || u.Diagnostics.UnreadableLines != 0 {
		t.Errorf("Diagnostics = %+v", u.Diagnostics)
	}
	want := []FileIssue{{File: sessUUID + ".jsonl", Offset: int64(len(first) + 1), Kind: IssueOversized}}
	if !reflect.DeepEqual(u.Diagnostics.Files, want) {
		t.Errorf("Files = %+v, want %+v", u.Diagnostics.Files, want)
	}
}

func TestParseSessionParentMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := ParseSession(SessionFiles{ID: "x", Parent: filepath.Join(dir, "x.jsonl")})
	if err == nil {
		t.Fatal("want an error when the parent transcript cannot be opened")
	}
	if strings.Contains(err.Error(), dir) || !strings.Contains(err.Error(), "x.jsonl") {
		t.Errorf("error = %q, want the base name only, not the directory", err)
	}
}

func TestParseSessionSubAgentProblemsAreNotFatal(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, sessUUID+".jsonl")
	writeFile(t, parent, string(msgLine("m0", "x", "end_turn", 1, 1, 1, 1))+"\n")
	ok := filepath.Join(dir, "agent-b-ok.jsonl")
	writeFile(t, ok, string(msgLine("m1", "x", "end_turn", 2, 2, 2, 2))+"\n")
	badMeta := filepath.Join(dir, "agent-c-bad.meta.json")
	writeFile(t, badMeta, "not json")
	writeFile(t, filepath.Join(dir, "agent-c-bad.jsonl"), string(msgLine("m2", "x", "end_turn", 4, 4, 4, 4))+"\n")
	files := SessionFiles{ID: sessUUID, Parent: parent, SubAgents: []SubAgentFiles{
		{ID: "a-gone", Transcript: filepath.Join(dir, "agent-a-gone.jsonl")}, // never created
		{ID: "b-ok", Transcript: ok},                                         // no meta
		{ID: "c-bad", Transcript: filepath.Join(dir, "agent-c-bad.jsonl"), Meta: badMeta},
	}}
	u, err := ParseSession(files)
	if err != nil {
		t.Fatalf("a sub-agent problem must not be a global error: %v", err)
	}
	if len(u.Agents) != 4 {
		t.Fatalf("agents = %+v, want parent + 3 in ID order", u.Agents)
	}
	for i, id := range []string{"", "a-gone", "b-ok", "c-bad"} {
		if u.Agents[i].ID != id {
			t.Errorf("agent %d ID = %q, want %q", i, u.Agents[i].ID, id)
		}
	}
	if got := u.Agents[2].Tokens; got != (Tokens{2, 2, 2, 2}) {
		t.Errorf("b-ok tokens = %+v", got)
	}
	if got := u.Agents[3].Tokens; got != (Tokens{4, 4, 4, 4}) {
		t.Errorf("c-bad tokens = %+v (a bad meta does not stop the transcript)", got)
	}
	for _, a := range u.Agents[1:] {
		if a.State != StateUnknown || a.Type != "" {
			t.Errorf("agent %q = %+v, want unknown state and no type", a.ID, a)
		}
	}
	d := u.Diagnostics
	if d.MetaMissing != 2 || d.MetaUnreadable != 1 {
		// a-gone and b-ok have no meta; c-bad has an unreadable one.
		t.Errorf("MetaMissing = %d (want 2), MetaUnreadable = %d (want 1)", d.MetaMissing, d.MetaUnreadable)
	}
	want := []FileIssue{{File: "agent-a-gone.jsonl", Offset: 0, Kind: IssueOpenFailed}}
	if !reflect.DeepEqual(d.Files, want) {
		t.Errorf("Files = %+v, want %+v", d.Files, want)
	}
}
