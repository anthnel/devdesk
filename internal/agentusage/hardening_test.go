package agentusage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// --- 1. bounded memory ---

func TestFilesCappedPerFileCountersKeepCounting(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, sessUUID+".jsonl")
	writeFile(t, parent, strings.Repeat("not json\n", 1000))
	u, err := ParseSession(SessionFiles{ID: sessUUID, Parent: parent})
	if err != nil {
		t.Fatal(err)
	}
	d := u.Diagnostics
	if d.UnreadableLines != 1000 || len(d.Files) != maxFileIssuesPerFile {
		t.Fatalf("UnreadableLines=%d len(Files)=%d, want 1000 and %d", d.UnreadableLines, len(d.Files), maxFileIssuesPerFile)
	}
}

func TestFilesCapIsPerFile(t *testing.T) {
	a := newAggregator([]Agent{{Main: true}})
	for _, f := range []string{"a.jsonl", "b.jsonl"} {
		for i := 0; i < 150; i++ {
			a.addLine(0, f, int64(i*10), []byte("garbage"))
		}
	}
	if a.diag.UnreadableLines != 300 || len(a.diag.Files) != 2*maxFileIssuesPerFile {
		t.Fatalf("Unreadable=%d len(Files)=%d, want 300 and %d", a.diag.UnreadableLines, len(a.diag.Files), 2*maxFileIssuesPerFile)
	}
}

// Choice: past maxNoIDEntries, a line without message.id is still counted
// exactly once in NoMessageID, Messages and its tokens, but gets no
// per-message table entry (it can no longer be deduplicated; as no-id lines
// are keyed by offset they were never deduplicated anyway).
func TestNoIDEntriesCapped(t *testing.T) {
	a := newAggregator([]Agent{{Main: true}})
	const extra = 50
	n := maxNoIDEntries + extra
	for i := 0; i < n; i++ {
		a.addLine(0, "p.jsonl", int64(i), msgLine("", "m", "end_turn", 1, 0, 0, 0))
	}
	if len(a.entries) > maxNoIDEntries {
		t.Fatalf("entries = %d, want <= %d", len(a.entries), maxNoIDEntries)
	}
	got := a.finish()[0]
	if a.diag.NoMessageID != n || got.Tokens.Input != int64(n) || got.Messages != n {
		t.Fatalf("NoMessageID=%d input=%d messages=%d, want %d each", a.diag.NoMessageID, got.Tokens.Input, got.Messages, n)
	}
}

// --- 2. models and versions ---

func TestModelsCappedFirstSeenFirst(t *testing.T) {
	a := newAggregator([]Agent{{Main: true}})
	for i := 0; i < 10000; i++ {
		a.addLine(0, "p.jsonl", int64(i), msgLine(fmt.Sprintf("id%d", i), fmt.Sprintf("model-%d", i), "end_turn", 1, 0, 0, 0))
	}
	got := a.finish()[0]
	if len(got.Models) != maxModelsPerAgent {
		t.Fatalf("len(Models) = %d, want %d", len(got.Models), maxModelsPerAgent)
	}
	for i, m := range got.Models {
		if m != fmt.Sprintf("model-%d", i) {
			t.Fatalf("Models[%d] = %q, want first-seen order", i, m)
		}
	}
	if got.Messages != 10000 {
		t.Errorf("Messages = %d, want 10000", got.Messages)
	}
}

func TestLongModelTruncated(t *testing.T) {
	long := strings.Repeat("m", 4000)
	a := newAggregator([]Agent{{Main: true}})
	a.addLine(0, "p.jsonl", 0, msgLine("i1", long, "end_turn", 1, 0, 0, 0))
	got := a.finish()[0]
	if len(got.Models) != 1 || got.Models[0] != long[:maxLabelLen] {
		t.Fatalf("len(Models) = %d, want one entry of %d bytes", len(got.Models), maxLabelLen)
	}
}

func TestVersionsCappedAndTruncated(t *testing.T) {
	a := newAggregator([]Agent{{Main: true}})
	for i := 0; i < 500; i++ {
		l := fmt.Sprintf(`{"version":"v%d","message":null}`, i)
		a.addLine(0, "p.jsonl", int64(i), []byte(l))
	}
	a.noteVersion(strings.Repeat("v", 4000))
	if v := a.versions(); len(v) != maxVersions || v[0] != "v0" {
		t.Fatalf("len(versions) = %d (first %q), want %d", len(v), v[0], maxVersions)
	}
	b := newAggregator([]Agent{{Main: true}})
	b.noteVersion(strings.Repeat("v", 4000))
	if v := b.versions(); len(v) != 1 || len(v[0]) != maxLabelLen {
		t.Fatalf("versions = %q, want one of %d bytes", v, maxLabelLen)
	}
}

// --- 3. sanitizing ---
// Choice: every control character (0x00-0x1f, 0x7f) becomes '?', then the
// label is truncated to maxLabelLen bytes.

func TestSanitizeLabel(t *testing.T) {
	for in, want := range map[string]string{
		"plain":            "plain",
		"a\x1b[31mb":       "a?[31mb",
		"a\r\nb\x00c\x7fd": "a??b?c?d",
		"":                 "",
	} {
		if got := sanitizeLabel(in); got != want {
			t.Errorf("sanitizeLabel(%q) = %q, want %q", in, got, want)
		}
	}
	if got := sanitizeLabel(strings.Repeat("x", 4000)); len(got) != maxLabelLen {
		t.Errorf("len = %d, want %d", len(got), maxLabelLen)
	}
}

func TestDecodeLineSanitizesModelAndVersion(t *testing.T) {
	line := `{"version":"2.1\u001b[2J\r\n","message":{"id":"i","model":"m\u001b]0;x\u0007\n","usage":{}}}`
	r, kind := decodeLine([]byte(line))
	if kind != kindUsage || hasControl(r.Model) || hasControl(r.Version) || r.Model == "" {
		t.Fatalf("kind=%v model=%q version=%q: want sanitized", kind, r.Model, r.Version)
	}
}

func TestReadMetaSanitizesAgentType(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent-x.meta.json")
	writeFile(t, p, `{"agentType":"co\u001bder\n"}`)
	m, err := readMeta(p)
	if err != nil || hasControl(m.AgentType) || m.AgentType == "" {
		t.Fatalf("AgentType = %q, err %v", m.AgentType, err)
	}
}

func TestControlCharactersInFileNamesNeverReachTheUsage(t *testing.T) {
	proj := t.TempDir()
	id := "ab\x1b[2Jcd\nef"
	parent := filepath.Join(proj, id+".jsonl")
	writeFile(t, parent, string(msgLine("m1", "x", "end_turn", 1, 1, 0, 0))+"\ngarbage\n"+`{"message":{"id":"p`)
	sub := filepath.Join(proj, id, subagentsDir)
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Skipf("filesystem refuses control characters in names: %v", err)
	}
	writeFile(t, filepath.Join(sub, "agent-s\x1b9\r.jsonl"), "garbage\n"+`{"message":{"id":"p`)
	files, err := LocateSession(parent)
	if err != nil {
		t.Fatal(err)
	}
	u, err := ParseSession(files)
	if err != nil {
		t.Fatal(err)
	}
	all := []string{files.ID, u.SessionID}
	for _, a := range u.Agents {
		all = append(all, a.ID, a.Type)
	}
	for _, sa := range files.SubAgents {
		all = append(all, sa.ID)
	}
	all = append(all, u.Diagnostics.TruncatedFiles...)
	for _, f := range u.Diagnostics.Files {
		all = append(all, f.File)
	}
	if len(u.Diagnostics.TruncatedFiles) == 0 || len(u.Diagnostics.Files) == 0 || len(u.Agents) != 2 {
		t.Fatalf("setup produced no truncated/issue/sub-agent data: %+v", u)
	}
	for _, s := range all {
		if hasControl(s) {
			t.Errorf("label %q contains a control character", s)
		}
	}
}

// --- 4. LocateSession ---

func TestLocateSessionEmptyID(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".jsonl")
	writeFile(t, p, "{}\n")
	if _, err := LocateSession(p); err == nil {
		t.Fatal("a parent named exactly .jsonl must be refused")
	}
}

func TestLocateSessionErrorsNeverCiteTheFullPath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission errors cannot be provoked as root")
	}
	parent, dir := session(t)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// sessionDir unreadable: Lstat of <dir>/subagents fails with EACCES.
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	_, err := LocateSession(parent)
	if err == nil || strings.Contains(err.Error(), filepath.Dir(parent)) {
		t.Fatalf("err = %v, want an error naming only a base name", err)
	}
	// subagents unreadable: opening or reading it fails.
	_ = os.Chmod(dir, 0o755)
	sub := filepath.Join(dir, subagentsDir)
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })
	_, err = LocateSession(parent)
	if err == nil || strings.Contains(err.Error(), filepath.Dir(parent)) {
		t.Fatalf("err = %v, want an error naming only a base name", err)
	}
}

// --- 5. ParseSession on a non-regular parent ---

func TestParseSessionNonRegularParentHasItsOwnError(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.jsonl")
	writeFile(t, real, "{}\n")
	link := filepath.Join(dir, "link.jsonl")
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	for name, p := range map[string]string{"symlink": link, "directory": dir} {
		_, err := ParseSession(SessionFiles{Parent: p})
		if err == nil || errors.Is(err, errNotPlain) || !errors.Is(err, errNotRegular) {
			t.Errorf("%s: err = %v, want errNotRegular and not errNotPlain", name, err)
		}
	}
}

// --- 6. record: model without id or usage ---

func TestDecodeLineModelWithoutIDOrUsageIsNoUsage(t *testing.T) {
	for name, tc := range map[string]struct {
		line string
		want lineKind
	}{
		"assistant model only": {`{"type":"assistant","version":"2.1.302","message":{"role":"assistant","model":"model-alpha","stop_reason":null,"content":[]}}`, kindNoUsage},
		"user line":            {`{"type":"user","version":"2.1.302","message":{"role":"user","content":"hello"}}`, kindIgnored},
		"tool_result":          {`{"type":"user","version":"2.1.302","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`, kindIgnored},
	} {
		if _, got := decodeLine([]byte(tc.line)); got != tc.want {
			t.Errorf("%s: kind = %v, want %v", name, got, tc.want)
		}
	}
}

// --- 7. mid-file read error ---

type diskFailReader struct {
	data string
	pos  int
}

func (f *diskFailReader) Read(p []byte) (int, error) {
	if f.pos >= len(f.data) {
		return 0, errors.New("disk on fire")
	}
	n := copy(p, f.data[f.pos:])
	f.pos += n
	return n, nil
}

var _ io.Reader = (*diskFailReader)(nil)

func TestParseReaderMidFileReadError(t *testing.T) {
	l1 := string(msgLine("m1", "x", "end_turn", 3, 4, 0, 0)) + "\n"
	l2 := string(msgLine("m2", "x", "end_turn", 5, 6, 0, 0)) + "\n"
	a := newAggregator([]Agent{{Main: true}})
	parseReader(a, 0, "p.jsonl", &diskFailReader{data: l1 + l2 + `{"message":{"id`})
	got := a.finish()[0]
	if got.Tokens.Input != 8 || got.Messages != 2 {
		t.Fatalf("kept tokens=%+v messages=%d, want both complete lines", got.Tokens, got.Messages)
	}
	if a.diag.UnreadableLines != 0 {
		t.Errorf("UnreadableLines = %d, want 0 (a read error is not an unreadable line)", a.diag.UnreadableLines)
	}
	want := FileIssue{File: "p.jsonl", Offset: int64(len(l1) + len(l2)), Kind: IssueReadError}
	if len(a.diag.Files) != 1 || a.diag.Files[0] != want {
		t.Fatalf("Files = %+v, want [%+v]", a.diag.Files, want)
	}
}

// --- 8. Files sorted by offset ---

func TestFilesSortedByOffsetWithinAFile(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, sessUUID+".jsonl")
	big := strings.Repeat("x", MaxLineBytes+10)
	writeFile(t, parent, "garbage\n"+big+"\ngarbage2\n"+big+"\ngarbage3\n")
	u, err := ParseSession(SessionFiles{ID: sessUUID, Parent: parent})
	if err != nil {
		t.Fatal(err)
	}
	fs := u.Diagnostics.Files
	if len(fs) != 5 || !sort.SliceIsSorted(fs, func(i, j int) bool { return fs[i].Offset < fs[j].Offset }) {
		t.Fatalf("Files = %+v, want 5 entries by increasing Offset", fs)
	}
}

// --- 9. negative spawnDepth ---

func TestReadMetaNegativeSpawnDepthIsZero(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent-x.meta.json")
	writeFile(t, p, `{"spawnDepth":-3}`)
	m, err := readMeta(p)
	if err != nil || m.SpawnDepth != 0 {
		t.Fatalf("SpawnDepth = %d, err %v, want 0", m.SpawnDepth, err)
	}
}

func TestSanitizeExtendedControls(t *testing.T) {
	for in, want := range map[string]string{
		"a\u009bb":        "a?b",     // C1 CSI
		"a\x9bb":          "a?b",     // raw byte, invalid UTF-8
		"a\u202eb":        "a?b",     // bidi override
		"a\u2066b\u2069c": "a?b?c",   // bidi isolates
		"héllo ✓":         "héllo ✓", // ordinary non-ASCII is kept
	} {
		if got := sanitize(in, 128); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}
