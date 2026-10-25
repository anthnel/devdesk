package agentusageview

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/agentusage"
	"github.com/anthnel/devdesk/internal/config"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// TestMain points HOME and CLAUDE_CONFIG_DIR at a temp dir for the whole
// process, so no test can read the developer's real transcripts.
func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	sharedcomponents.FooterMsgDuration = time.Millisecond

	home, err := os.MkdirTemp("", "agentusageview-home-")
	if err != nil {
		panic(err)
	}
	for k, v := range map[string]string{
		"HOME":              home,
		"USERPROFILE":       home,
		"CLAUDE_CONFIG_DIR": filepath.Join(home, ".claude"),
	} {
		if err := os.Setenv(k, v); err != nil {
			panic(err)
		}
	}

	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}

// ── Fixtures ─────────────────────────────────────────────────────────────────

// isolate gives one test its own HOME and config dir.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	return home
}

var base = time.Date(2026, 9, 1, 10, 30, 0, 0, time.Local)

func session(id, project string, age time.Duration) agentusage.Session {
	return agentusage.Session{ID: id, Project: project, ModTime: base.Add(-age)}
}

// newModel builds a sized model with the first read still in flight.
func newModel(t *testing.T) Model {
	t.Helper()
	isolate(t)
	return feed(t, New(config.Default()), testutil.Resize(120, 20))
}

func feed(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want agentusageview.Model", updated)
	}
	return next
}

// loaded delivers a listing carrying the model's current generation.
func loaded(t *testing.T, m Model, sessions ...agentusage.Session) Model {
	t.Helper()
	return feed(t, m, sessionListLoadedMsg{
		gen:     m.gen,
		root:    filepath.Join(os.Getenv("HOME"), ".claude", "projects"),
		listing: agentusage.Listing{Sessions: sessions},
	})
}

func sessionsValue(t *testing.T, m Model) string {
	t.Helper()
	for _, h := range m.GetHeaderInfo("") {
		if h.Key == "Sessions" {
			return h.Value
		}
	}
	t.Fatalf("no Sessions header field in %+v", m.GetHeaderInfo(""))
	return ""
}

func footer(m Model) string { return m.RenderFooter(120) }

// ── Loading ──────────────────────────────────────────────────────────────────

func TestInitReadsTheListingAndStartsTheSpinner(t *testing.T) {
	m := New(config.Default())
	if !m.loading {
		t.Fatal("a new model is loading")
	}
	if m.Init() == nil {
		t.Fatal("Init returns the read and the spinner tick")
	}
}

func TestWhileLoadingTheTableKeepsItsHeadersAndHasNoRows(t *testing.T) {
	m := newModel(t)
	view := m.View()
	for _, title := range []string{"Project", "Last active"} {
		if !strings.Contains(view, title) {
			t.Errorf("loading view lacks column %q:\n%s", title, view)
		}
	}
	if got := sessionsValue(t, m); got != "0" {
		t.Errorf("Sessions = %q while loading, want 0", got)
	}
	f := footer(m)
	if !strings.Contains(f, "Reading Claude Code sessions...") {
		t.Errorf("footer should show the loading status:\n%s", f)
	}
	if strings.Contains(f, "No Claude Code sessions") {
		t.Error("the empty message must wait for the load to finish")
	}
}

func TestASpinnerTickWhileNotLoadingIsDropped(t *testing.T) {
	m := loaded(t, newModel(t), session("a", "/x", 0))
	_, cmd := m.Update(spinnerTick())
	if cmd != nil {
		t.Error("a tick after the load must not reschedule itself")
	}
}

// ── Rows ─────────────────────────────────────────────────────────────────────

func TestRowsKeepTheOrderGivenNewestFirst(t *testing.T) {
	m := newModel(t)
	home := os.Getenv("HOME")
	m = loaded(t, m,
		session("11111111-aaaa", filepath.Join(home, "work", "newest"), 1*time.Hour),
		session("22222222-bbbb", filepath.Join(home, "work", "middle"), 5*time.Hour),
		session("33333333-cccc", "/srv/oldest", 48*time.Hour),
	)
	view := m.View()
	order := []string{"newest", "middle", "oldest"}
	last := -1
	for _, name := range order {
		i := strings.Index(view, name)
		if i < 0 {
			t.Fatalf("project %q missing from view:\n%s", name, view)
		}
		if i < last {
			t.Errorf("%q is out of order (recent-to-old expected):\n%s", name, view)
		}
		last = i
	}
	if got := sessionsValue(t, m); got != "3" {
		t.Errorf("Sessions = %q, want 3", got)
	}
}

func TestProjectIsTheFullPathWithHomeFoldedToATilde(t *testing.T) {
	m := newModel(t)
	home := os.Getenv("HOME")
	m = loaded(t, m, session("11111111-aaaa", filepath.Join(home, "projects", "devdesk"), time.Hour))
	view := m.View()
	if !strings.Contains(view, "~/projects/devdesk") {
		t.Errorf("project should read ~/projects/devdesk:\n%s", view)
	}
	if strings.Contains(view, home) {
		t.Errorf("the home directory must be folded, found %q", home)
	}
}

func TestAMissingProjectIsADash(t *testing.T) {
	m := loaded(t, newModel(t), session("11111111-aaaa", "", time.Hour))
	if !strings.Contains(m.View(), "-") {
		t.Errorf("an unknown project renders a dash:\n%s", m.View())
	}
}

func TestLastActiveReadsAsLocalDateAndTime(t *testing.T) {
	m := loaded(t, newModel(t), session("11111111-aaaa", "/srv/p", 0))
	want := base.Format("2006-01-02 15:04")
	if !strings.Contains(m.View(), want) {
		t.Errorf("view should contain %q:\n%s", want, m.View())
	}
}

func TestSessionColumnShowsTheLeadingIDAndSearchUsesTheFullOne(t *testing.T) {
	cols := sessionColumns()
	if len(cols) != 3 {
		t.Fatalf("want 3 columns (Project, Last active, Session), got %d", len(cols))
	}
	for i, title := range []string{"Project", "Last active", "Session"} {
		if cols[i].Title != title {
			t.Errorf("column %d = %q, want %q", i, cols[i].Title, title)
		}
	}
	full := "abcdef12-3456-7890-abcd-ef1234567890"
	row := sessionRow{Session: session(full, "/srv/p", 0), project: "/srv/p"}
	if cell := cols[2].Cell(row); cell == "" || !strings.HasPrefix(full, cell) {
		t.Errorf("Session cell %q should be a prefix of the ID", cell)
	}
	if cols[2].Search == nil || cols[2].Search(row) != full {
		t.Error("Session column must search on the full ID")
	}
	if cols[0].Search == nil || cols[0].Search(row) != "/srv/p" {
		t.Error("Project column must search on the project")
	}
}

func TestTheProjectColumnTruncatesFromTheLeft(t *testing.T) {
	cols := sessionColumns()
	if len(cols) == 0 || !cols[0].TruncateHead {
		t.Error("Project must truncate from the left so the leaf stays visible")
	}
	if len(cols) > 0 && cols[0].Flex == 0 {
		t.Error("Project should flex to take the spare width")
	}
}

// ── Empty, error, stale ──────────────────────────────────────────────────────

// Rule 139: zero sessions is a table with headers and no rows; the message
// belongs to the footer.
func TestZeroSessionsKeepsTheTableAndSaysSoInTheFooter(t *testing.T) {
	m := loaded(t, newModel(t))
	view := m.View()
	for _, title := range []string{"Project", "Last active"} {
		if !strings.Contains(view, title) {
			t.Errorf("empty view lacks column %q:\n%s", title, view)
		}
	}
	if strings.Contains(view, "No Claude Code sessions") {
		t.Error("the body must stay a table; the message is the footer's")
	}
	if got := sessionsValue(t, m); got != "0" {
		t.Errorf("Sessions = %q, want 0", got)
	}
	if f := footer(m); !strings.Contains(f, "No Claude Code sessions found") {
		t.Errorf("footer should say no sessions were found:\n%s", f)
	}
	if f := footer(m); strings.Contains(f, "Reading Claude Code sessions") {
		t.Error("loading status must be gone once the read finished")
	}
}

func TestTheEmptyMessageNamesTheRootWithHomeFolded(t *testing.T) {
	m := loaded(t, newModel(t))
	if f := footer(m); !strings.Contains(f, "~/.claude/projects") {
		t.Errorf("footer should name the folded root:\n%s", f)
	}
}

func TestAFilterWithNoMatchIsNotAnEmptyListing(t *testing.T) {
	m := loaded(t, newModel(t), session("11111111-aaaa", "/srv/alpha", time.Hour))
	m = send(t, m, testutil.Key("/"))
	for _, k := range testutil.Type("zzz") {
		m = feed(t, m, k)
	}
	if got := sessionsValue(t, m); got != "0" {
		t.Errorf("Sessions = %q under a filter with no match, want 0", got)
	}
	if f := footer(m); strings.Contains(f, "No Claude Code sessions") {
		t.Errorf("a filter with no match must not claim there are no sessions:\n%s", f)
	}
}

func TestAnErrorIsReportedInTheFooterAndKeepsTheRows(t *testing.T) {
	m := loaded(t, newModel(t), session("11111111-aaaa", "/srv/alpha", time.Hour))
	m = feed(t, m, sessionListLoadedMsg{gen: m.gen, err: errors.New("agentusage: projects is not a directory")})
	f := footer(m)
	if !strings.Contains(f, "Failed to list Claude Code sessions") {
		t.Errorf("footer should report the failure:\n%s", f)
	}
	if strings.Contains(f, "is not a directory") {
		t.Error("the raw error is logged, not shown (Rule 128)")
	}
	if !strings.Contains(m.View(), "/srv/alpha") {
		t.Error("rows already shown must be kept after a failed refresh")
	}
	if got := sessionsValue(t, m); got != "1" {
		t.Errorf("Sessions = %q, want 1", got)
	}
}

func TestAnErrorOnTheFirstReadShowsNoEmptyMessage(t *testing.T) {
	m := feed(t, newModel(t), sessionListLoadedMsg{gen: 0, err: errors.New("boom")})
	f := footer(m)
	if !strings.Contains(f, "Failed to list Claude Code sessions") {
		t.Errorf("footer should report the failure:\n%s", f)
	}
	if strings.Contains(f, "No Claude Code sessions") {
		t.Error("a failure is not an empty listing")
	}
}

func TestOmittedSessionsAppearInTheHeaderOnlyWhenThereAreSome(t *testing.T) {
	m := newModel(t)
	m = loaded(t, m, session("a", "/x", 0))
	for _, h := range m.GetHeaderInfo("") {
		if h.Key == "Skipped" {
			t.Error("Skipped must be absent when nothing was omitted")
		}
	}
	m = feed(t, m, sessionListLoadedMsg{
		gen:     m.gen,
		listing: agentusage.Listing{Sessions: []agentusage.Session{session("a", "/x", 0)}, Omitted: 4},
	})
	for _, h := range m.GetHeaderInfo("") {
		if h.Key == "Skipped" {
			if h.Value != "4" {
				t.Errorf("Skipped = %q, want 4", h.Value)
			}
			return
		}
	}
	t.Error("Skipped header field missing when sessions were omitted")
}

// ── Generations and refresh ──────────────────────────────────────────────────

func TestAStaleListingIsIgnored(t *testing.T) {
	m := newModel(t)
	m = feed(t, m, testutil.Key("ctrl+r")) // gen advances
	stale := sessionListLoadedMsg{
		gen:     m.gen - 1,
		listing: agentusage.Listing{Sessions: []agentusage.Session{session("old", "/stale/project", 0)}},
	}
	m = feed(t, m, stale)
	if strings.Contains(m.View(), "stale") {
		t.Error("a response from an older generation must not reach the table")
	}
	if !m.loading {
		t.Error("the current read is still in flight; a stale answer must not end the load")
	}
}

func TestCtrlRStartsANewReadWithANewGeneration(t *testing.T) {
	m := loaded(t, newModel(t), session("a", "/x", 0))
	if m.loading {
		t.Fatal("precondition: load finished")
	}
	before := m.gen
	updated, cmd := m.Update(testutil.Key("ctrl+r"))
	m = updated.(Model)
	if m.gen == before {
		t.Error("ctrl+r must advance the generation")
	}
	if !m.loading {
		t.Error("ctrl+r must set loading")
	}
	if cmd == nil {
		t.Fatal("ctrl+r must return the read command")
	}
	if !strings.Contains(m.View(), "/x") {
		t.Error("rows stay on screen during a refresh (Rule 139)")
	}
	if f := footer(m); !strings.Contains(f, "Reading Claude Code sessions...") {
		t.Errorf("footer should show the refresh:\n%s", f)
	}
	// The command built for this generation answers with the same one.
	msg, ok := testutil.MsgOf[sessionListLoadedMsg](listSessionsCmd(m.gen))
	if !ok || msg.gen != m.gen {
		t.Errorf("listSessionsCmd(%d) returned %+v (ok=%v)", m.gen, msg, ok)
	}
}

func TestCtrlRWhileTypingInTheFilterIsNotLost(t *testing.T) {
	// The filter bar owns the keyboard: ctrl+r must not start a read there.
	m := loaded(t, newModel(t), session("a", "/x", 0))
	m = send(t, m, testutil.Key("/"))
	before := m.gen
	m = send(t, m, testutil.Key("ctrl+r"))
	if m.gen != before {
		t.Error("ctrl+r inside the filter must not refresh")
	}
}

// End to end: the Cmd resolves the root and reads the disk itself.
func TestListSessionsCmdReadsTheConfiguredRoot(t *testing.T) {
	home := isolate(t)
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	dir := filepath.Join(cfg, "projects", "-srv-alpha")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "0a1b2c3d-0000-4000-8000-000000000001"
	line := `{"type":"user","cwd":"` + filepath.Join(home, "alpha") + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	// Called directly: testutil.Msgs on the batch would wait for the tick.
	msg, ok := listSessionsCmd(0)().(sessionListLoadedMsg)
	if !ok {
		t.Fatal("listSessionsCmd must return a sessionListLoadedMsg")
	}
	if msg.err != nil || len(msg.listing.Sessions) != 1 {
		t.Fatalf("msg = %+v", msg)
	}
	if msg.root != filepath.Join(cfg, "projects") {
		t.Errorf("root = %q", msg.root)
	}

	m := feed(t, New(config.Default()), testutil.Resize(120, 20))
	m = feed(t, m, msg)
	if !strings.Contains(m.View(), "~/alpha") {
		t.Errorf("row missing after the real read:\n%s", m.View())
	}
}

func TestAMissingRootIsAnEmptyListingNotAnError(t *testing.T) {
	isolate(t)
	msg, ok := listSessionsCmd(0)().(sessionListLoadedMsg)
	if !ok || msg.err != nil || len(msg.listing.Sessions) != 0 {
		t.Fatalf("msg = %+v", msg)
	}
}

// ── Filter ───────────────────────────────────────────────────────────────────

func TestTheFilterNarrowsRowsAndHoldsTheKeyboard(t *testing.T) {
	m := loaded(t, newModel(t),
		session("11111111-aaaa", "/srv/alpha", time.Hour),
		session("22222222-bbbb", "/srv/beta", 2*time.Hour),
	)
	m = send(t, m, testutil.Key("/"))
	if !m.InEditMode() {
		t.Fatal("/ opens the filter and InEditMode must say so")
	}
	for _, k := range testutil.Type("alpha") {
		m = feed(t, m, k)
	}
	if got := sessionsValue(t, m); got != "1" {
		t.Errorf("Sessions = %q, want 1", got)
	}
	view := m.View()
	if !strings.Contains(view, "alpha") || strings.Contains(view, "beta") {
		t.Errorf("only the alpha row should remain:\n%s", view)
	}
	if !m.FilterBarVisible() {
		t.Error("the filter bar is visible while filtering")
	}
}

func TestFooterHeightFollowsTheFilterBar(t *testing.T) {
	m := loaded(t, newModel(t), session("a", "/x", 0))
	if m.GetFooterHeight() != 2 {
		t.Errorf("footer height = %d, want 2 (Rule 124)", m.GetFooterHeight())
	}
	m = send(t, m, testutil.Key("/"))
	if m.GetFooterHeight() <= 2 {
		t.Error("the filter bar adds to the footer height (Rule 136)")
	}
}

// ── Layout (Rules 108, 116) ──────────────────────────────────────────────────

func TestResizeFillsTheViewportExactly(t *testing.T) {
	m := loaded(t, newModel(t), session("11111111-aaaa", "/srv/alpha", time.Hour))
	m = feed(t, m, testutil.Resize(80, 20))
	// RenderedWidth is read through the table the model owns.
	if got := renderedWidth(m); got != 78 {
		t.Errorf("rendered width = %d at 80 columns, want 78", got)
	}
}

func TestANarrowTerminalDropsSessionFirstAndKeepsProject(t *testing.T) {
	m := loaded(t, newModel(t), session("11111111-aaaa", "/srv/alpha", time.Hour))
	m = feed(t, m, testutil.Resize(40, 20))
	view := m.View()
	if strings.Contains(view, "Session") {
		t.Errorf("Session is Optional and goes first at 40 columns:\n%s", view)
	}
	if !strings.Contains(view, "Project") {
		t.Errorf("Project must stay:\n%s", view)
	}
}

// ── Shortcuts (Rules 130, 137, 138) ──────────────────────────────────────────

func TestShortcutKeysAreTheSameInEveryState(t *testing.T) {
	loading := newModel(t)
	empty := loaded(t, newModel(t))
	rows := loaded(t, newModel(t), session("a", "/x", 0))
	failed := feed(t, rows, sessionListLoadedMsg{gen: rows.gen, err: errors.New("boom")})
	filtering := send(t, rows, testutil.Key("/"))

	want := testutil.ShortcutKeys(loading.GetShortcuts())
	if len(want) == 0 {
		t.Fatal("the view advertises no shortcuts")
	}
	for _, k := range []string{"ctrl+r", "/", "?"} {
		if !testutil.HasShortcut(loading.GetShortcuts(), k) {
			t.Errorf("missing shortcut %q", k)
		}
	}
	states := map[string]Model{"empty": empty, "loaded": rows, "error": failed, "filtering": filtering}
	for name, m := range states {
		got := testutil.ShortcutKeys(m.GetShortcuts())
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: keys %v differ from loading's %v", name, got, want)
		}
	}
	for name, m := range map[string]Model{"loading": loading, "empty": empty, "loaded": rows, "error": failed, "filtering": filtering} {
		for _, s := range m.GetShortcuts() {
			if s.Disabled {
				t.Errorf("%s: %q is greyed, but every key always acts here", name, s.Key)
			}
		}
	}
}

func TestShortcutDescriptionsStartWithACapital(t *testing.T) {
	for _, s := range newModel(t).GetShortcuts() {
		if s.Description == "" || s.Description[0] < 'A' || s.Description[0] > 'Z' {
			t.Errorf("%q: description %q must start with a capital (Rule 137)", s.Key, s.Description)
		}
	}
}

// ── Header and help ──────────────────────────────────────────────────────────

func TestTheTitleNamesTheView(t *testing.T) {
	if title := New(config.Default()).GetTitle(); !strings.Contains(title, "Agent usage") {
		t.Errorf("title = %q", title)
	}
	if icon := New(config.Default()).GetIcon(); icon != "" {
		t.Errorf("the icon lives in the title, got %q", icon)
	}
}

func TestHelpDocumentsTheViewAndItsPrivacy(t *testing.T) {
	var p help.Provider = New(config.Default())
	c := p.GetHelpContent()
	if c.Title == "" || c.Description == "" {
		t.Errorf("help needs a title and a description: %+v", c)
	}
	keys := map[string]bool{}
	for _, b := range c.KeyBindings {
		keys[b.Key] = true
	}
	for _, k := range []string{"ctrl+r", "/", "?"} {
		if !keys[k] {
			t.Errorf("help lacks key %q", k)
		}
	}
	if len(c.Sections) < 3 {
		t.Fatalf("help needs at least 3 sections (columns, sources, privacy), got %d", len(c.Sections))
	}
	var all strings.Builder
	for _, s := range c.Sections {
		if s.Title == "" || s.Body == "" {
			t.Errorf("empty help section: %+v", s)
		}
		all.WriteString(s.Title + " " + s.Body + " ")
	}
	text := strings.ToLower(all.String())
	for _, want := range []string{"claude_config_dir", "project", "last active", "prompt"} {
		if !strings.Contains(text, want) {
			t.Errorf("help should mention %q", want)
		}
	}
}

// ── Conventions copied from jobsview ─────────────────────────────────────────

func TestEveryColumnDeclaresItsSizing(t *testing.T) {
	cols := sessionColumns()
	if len(cols) == 0 {
		t.Fatal("the table has no columns")
	}
	for _, col := range cols {
		if col.Sizing == datatable.SizingUnset {
			t.Errorf("column %q has no Sizing", col.Title)
		}
	}
}

// Rule 122: a cell is measured before it is styled.
func TestNoCellCarriesAnEscapeSequence(t *testing.T) {
	testutil.TrueColor(t)
	cols := sessionColumns()
	if len(cols) == 0 {
		t.Fatal("the table has no columns")
	}
	for _, project := range []string{"/srv/alpha", "-", ""} {
		row := sessionRow{Session: session("0a1b2c3d-0000", project, 0), project: project}
		for _, col := range cols {
			if strings.Contains(col.Cell(row), "\x1b") {
				t.Errorf("column %q returns a styled cell for project %q", col.Title, project)
			}
		}
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func send(t *testing.T, m Model, msg tea.Msg) Model { return feed(t, m, msg) }

func spinnerTick() tea.Msg { return spinner.TickMsg{Time: time.Now()} }

// renderedWidth is what the table's lines span (Rule 116).
func renderedWidth(m Model) int { return m.table.RenderedWidth() }
