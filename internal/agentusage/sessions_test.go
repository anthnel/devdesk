package agentusage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// sessionSentinel is planted in every field ListSessions must never keep.
const sessionSentinel = "SENTINEL-DO-NOT-LEAK"

// baseTime is a fixed origin so mtimes are deterministic.
var baseTime = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// writeSession writes <root>/<project>/<id>.jsonl with content and sets its mtime.
func writeSession(t *testing.T, root, project, id, content string, mtime time.Time) string {
	t.Helper()
	dir := filepath.Join(root, project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return p
}

// cwdLine is a transcript line carrying only a working directory.
func cwdLine(cwd string) string {
	b, _ := json.Marshal(cwd) // %q would emit \x1b, which is not valid JSON
	return fmt.Sprintf(`{"type":"user","cwd":%s}`+"\n", b)
}

func mustList(t *testing.T, root string) Listing {
	t.Helper()
	l, err := ListSessions(root)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	return l
}

func ids(l Listing) []string {
	var out []string
	for _, s := range l.Sessions {
		out = append(out, s.ID)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestListSessionsSortsNewestFirst(t *testing.T) {
	root := t.TempDir()
	writeSession(t, root, "p1", "old", cwdLine("/w/a"), baseTime.Add(1*time.Hour))
	writeSession(t, root, "p2", "new", cwdLine("/w/b"), baseTime.Add(3*time.Hour))
	writeSession(t, root, "p1", "mid", cwdLine("/w/a"), baseTime.Add(2*time.Hour))

	l := mustList(t, root)
	if got, want := ids(l), []string{"new", "mid", "old"}; !equalStrings(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	if !l.Sessions[0].ModTime.Equal(baseTime.Add(3 * time.Hour)) {
		t.Errorf("ModTime = %v, want the file mtime", l.Sessions[0].ModTime)
	}
	if l.Omitted != 0 {
		t.Errorf("Omitted = %d, want 0", l.Omitted)
	}
}

func TestListSessionsTiesBreakByIDAscending(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"c", "a", "b"} {
		writeSession(t, root, "p", id, cwdLine("/w"), baseTime)
	}
	if got, want := ids(mustList(t, root)), []string{"a", "b", "c"}; !equalStrings(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestListSessionsReadsProjectFromCwd(t *testing.T) {
	long := "/" + strings.Repeat("a", 5000)
	// A first line larger than MaxLineBytes, then a cwd line.
	oversized := `{"x":"` + strings.Repeat("z", MaxLineBytes+10) + `"}` + "\n" + cwdLine("/work/after-big")

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"cwd on first line", cwdLine("/work/alpha"), "/work/alpha"},
		{"cwd on second line", `{"type":"summary","summary":"s"}` + "\n" + cwdLine("/work/alpha"), "/work/alpha"},
		{"first non-empty cwd wins", cwdLine("") + cwdLine("/work/one") + cwdLine("/work/two"), "/work/one"},
		{"no cwd anywhere", `{"type":"summary"}` + "\n", ""},
		{"empty file", "", ""},
		{"cwd is a number", `{"cwd":42}` + "\n", ""},
		{"cwd is null", `{"cwd":null}` + "\n", ""},
		{"unreadable line then cwd", "not json\n" + cwdLine("/work/ok"), "/work/ok"},
		{"control characters replaced", cwdLine("/w/\x1b[31mred"), "/w/?[31mred"},
		{"capped at maxPathLen", cwdLine(long), long[:maxPathLen]},
		{"oversized first line skipped", oversized, "/work/after-big"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeSession(t, root, "p", "s1", tc.content, baseTime)
			l := mustList(t, root)
			if len(l.Sessions) != 1 {
				t.Fatalf("got %d sessions, want 1 (listed even without a cwd)", len(l.Sessions))
			}
			if got := l.Sessions[0].Project; got != tc.want {
				t.Errorf("Project = %.80q (len %d), want %.80q (len %d)", got, len(got), tc.want, len(tc.want))
			}
			if l.Omitted != 0 {
				t.Errorf("Omitted = %d, want 0", l.Omitted)
			}
		})
	}
}

func TestListSessionsCwdPastProbeLimitIsEmpty(t *testing.T) {
	old := cwdProbeBytes
	cwdProbeBytes = 1024
	t.Cleanup(func() { cwdProbeBytes = old })

	root := t.TempDir()
	filler := `{"pad":"` + strings.Repeat("p", 2048) + `"}` + "\n"
	writeSession(t, root, "p", "s1", filler+cwdLine("/work/too-far"), baseTime)
	l := mustList(t, root)
	if len(l.Sessions) != 1 || l.Sessions[0].Project != "" {
		t.Fatalf("sessions = %+v, want one with empty Project", l.Sessions)
	}
}

func TestListSessionsIgnoresNonSessions(t *testing.T) {
	root := t.TempDir()
	writeSession(t, root, "p", "real", cwdLine("/w"), baseTime)
	// Things that are not sessions and must not count as omitted either.
	writeSession(t, root, "p", "agent-x", cwdLine("/w"), baseTime) // old layout
	writeSession(t, root, "p", "", cwdLine("/w"), baseTime)        // a file named .jsonl
	if err := os.WriteFile(filepath.Join(root, "p", "notes.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "p", "real", "subagents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "agent-1.jsonl"), []byte(cwdLine("/w")), 0o600); err != nil {
		t.Fatal(err)
	}
	// A stray file directly in the root is not a project.
	if err := os.WriteFile(filepath.Join(root, "stray.jsonl"), []byte(cwdLine("/w")), 0o600); err != nil {
		t.Fatal(err)
	}

	l := mustList(t, root)
	if got, want := ids(l), []string{"real"}; !equalStrings(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	if l.Omitted != 0 {
		t.Errorf("Omitted = %d, want 0 (ignored entries are not counted)", l.Omitted)
	}
}

func TestListSessionsCountsSymlinksAsOmitted(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeSession(t, root, "p", "real", cwdLine("/w"), baseTime)

	target := writeSession(t, outside, "x", "target", cwdLine("/elsewhere"), baseTime)
	if err := os.Symlink(target, filepath.Join(root, "p", "linked.jsonl")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "x"), filepath.Join(root, "linkedproj")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	l := mustList(t, root)
	if got, want := ids(l), []string{"real"}; !equalStrings(got, want) {
		t.Fatalf("ids = %v, want %v (links must be refused)", got, want)
	}
	if l.Omitted != 2 {
		t.Errorf("Omitted = %d, want 2 (one linked transcript, one linked project)", l.Omitted)
	}
}

func TestListSessionsFollowsASymlinkedRoot(t *testing.T) {
	real := t.TempDir()
	writeSession(t, real, "p", "s1", cwdLine("/w"), baseTime)
	link := filepath.Join(t.TempDir(), "projects")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	l := mustList(t, link)
	if got, want := ids(l), []string{"s1"}; !equalStrings(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
}

func TestListSessionsUnreadableProjectIsOmitted(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod 000 is not enforced here")
	}
	root := t.TempDir()
	writeSession(t, root, "ok", "s1", cwdLine("/w"), baseTime)
	writeSession(t, root, "locked", "s2", cwdLine("/w"), baseTime)
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	l := mustList(t, root)
	if got, want := ids(l), []string{"s1"}; !equalStrings(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	if l.Omitted != 1 {
		t.Errorf("Omitted = %d, want 1", l.Omitted)
	}
}

func TestListSessionsKeepsTheNewestPastMaxSessions(t *testing.T) {
	old := maxSessions
	maxSessions = 2
	t.Cleanup(func() { maxSessions = old })

	root := t.TempDir()
	writeSession(t, root, "p", "oldest", cwdLine("/w"), baseTime)
	writeSession(t, root, "p", "middle", cwdLine("/w"), baseTime.Add(time.Hour))
	writeSession(t, root, "p", "newest", cwdLine("/w"), baseTime.Add(2*time.Hour))

	l := mustList(t, root)
	if got, want := ids(l), []string{"newest", "middle"}; !equalStrings(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	if l.Omitted != 1 {
		t.Errorf("Omitted = %d, want 1", l.Omitted)
	}
}

func TestListSessionsMissingRootIsEmpty(t *testing.T) {
	l, err := ListSessions(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("err = %v, want nil for a missing root", err)
	}
	if len(l.Sessions) != 0 || l.Omitted != 0 {
		t.Errorf("listing = %+v, want empty", l)
	}
}

func TestListSessionsRootThatIsAFileIsAnErrorWithoutItsPath(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ListSessions(file)
	if err == nil {
		t.Fatal("err = nil, want an error for a root that is a file")
	}
	if strings.Contains(err.Error(), dir) {
		t.Errorf("error %q leaks the full path", err)
	}
	if !strings.Contains(err.Error(), "not-a-dir") {
		t.Errorf("error %q should name the base name", err)
	}
}

func TestListSessionsNeverKeepsTranscriptContent(t *testing.T) {
	root := t.TempDir()
	content := cwdLine("/work/safe") +
		fmt.Sprintf(`{"type":"user","content":%q,"message":{"id":"m","content":%q},"description":%q,"input":{"cmd":%q}}`+"\n",
			sessionSentinel, sessionSentinel, sessionSentinel, sessionSentinel)
	writeSession(t, root, "p", "s1", content, baseTime)

	l := mustList(t, root)
	if len(l.Sessions) != 1 || l.Sessions[0].Project != "/work/safe" {
		t.Fatalf("sessions = %+v, want one with Project /work/safe", l.Sessions)
	}
	if s := fmt.Sprintf("%+v", l); strings.Contains(s, sessionSentinel) {
		t.Errorf("listing leaks transcript content: %s", s)
	}
}

func TestListSessionsSanitizesTheID(t *testing.T) {
	root := t.TempDir()
	writeSession(t, root, "p", "bad\x1bid", cwdLine("/w"), baseTime)
	l := mustList(t, root)
	if len(l.Sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(l.Sessions))
	}
	if strings.ContainsRune(l.Sessions[0].ID, 0x1b) {
		t.Errorf("ID %q still has a control character", l.Sessions[0].ID)
	}
}

// Parent must feed LocateSession unchanged: that is the S-03b contract.
func TestListSessionsParentFeedsLocateSession(t *testing.T) {
	root := t.TempDir()
	parent := writeSession(t, root, "p", "s1", cwdLine("/w"), baseTime)
	sub := filepath.Join(root, "p", "s1", "subagents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "agent-1.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	l := mustList(t, root)
	if len(l.Sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(l.Sessions))
	}
	if l.Sessions[0].Parent != parent {
		t.Errorf("Parent = %q, want %q", l.Sessions[0].Parent, parent)
	}
	files, err := LocateSession(l.Sessions[0].Parent)
	if err != nil {
		t.Fatalf("LocateSession(Parent): %v", err)
	}
	if files.ID != l.Sessions[0].ID || len(files.SubAgents) != 1 {
		t.Errorf("files = %+v, want ID %q and 1 sub-agent", files, l.Sessions[0].ID)
	}
}

// A project directory swapped for a link after the root was listed must be
// refused by projectSessions itself, not only by the type seen in the root.
func TestProjectSessionsRefusesALinkedDirectory(t *testing.T) {
	outside := t.TempDir()
	writeSession(t, outside, "x", "leak", cwdLine("/elsewhere"), baseTime)
	link := filepath.Join(t.TempDir(), "proj")
	if err := os.Symlink(filepath.Join(outside, "x"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	got, omitted := projectSessions(link)
	if len(got) != 0 || omitted != 1 {
		t.Fatalf("got %d sessions, omitted %d; want 0 and 1", len(got), omitted)
	}
}

func TestListSessionsIgnoresADirectoryNamedLikeATranscript(t *testing.T) {
	root := t.TempDir()
	writeSession(t, root, "p", "real", cwdLine("/w"), baseTime)
	if err := os.MkdirAll(filepath.Join(root, "p", "x.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	l := mustList(t, root)
	if got, want := ids(l), []string{"real"}; !equalStrings(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	if l.Omitted != 0 {
		t.Errorf("Omitted = %d, want 0", l.Omitted)
	}
}

func TestListSessionsSanitizesUnicodeControlsInProject(t *testing.T) {
	root := t.TempDir()
	// Raw 0x9B (invalid UTF-8) cannot go through JSON, so it is escaped as
	// \u009b (C1) here; the raw byte is covered by TestSanitizeExtendedControls.
	writeSession(t, root, "p", "s1", cwdLine("/a\u009b31m\u202eb"), baseTime)
	l := mustList(t, root)
	if len(l.Sessions) != 1 || l.Sessions[0].Project != "/a?31m?b" {
		t.Fatalf("sessions = %+v, want project /a?31m?b", l.Sessions)
	}
}
