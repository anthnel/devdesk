package app

import (
	"image/color"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/ui/configuration"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-258 step 5 and 6a: app.theme "auto" proven on the bytes a real
// tea.Program writes, its input an io.Pipe the test answers on.
//
// Names these tests impose on the implementation (step 6a), matched by type
// name so the package compiles before they exist:
//   - message themeQueryDueMsg{gen int}: the deferred background query, due
//     package var themeQueryDelay (100 ms) after the reset of the background
//     was drawn; its handler sends tea.RequestBackgroundColor and arms
//     themeDetectTimeoutMsg{gen} (themeDetectTimeout);
//   - choosing auto in session with no cached answer returns a Cmd carrying
//     themeQueryDueMsg and nothing else asking the terminal.

const (
	bgQuery   = "\x1b]11;?\x07"
	lightOSC  = "\x1b]11;rgb:ffff/ffff/ffff\x07"
	darkOSC   = "\x1b]11;rgb:0000/0000/0000\x07"
	lightBgOK = "#ffffff"
)

// themeProbe hosts the router without its Init (MCP, scan tools, views): only
// the theme part of Init runs, which is what the bytes depend on.
type themeProbe struct{ a *App }

func (p themeProbe) Init() tea.Cmd { return p.a.themeInitCmd() }
func (p themeProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := p.a.Update(msg)
	return p, cmd
}
func (p themeProbe) View() tea.View { return p.a.View() }

// noQuery drops the OSC 11 query: it shares the "ESC ] 11 ;" prefix with a SET.
func noQuery(out string) string { return strings.ReplaceAll(out, bgQuery, "") }

func setCount(out string) int {
	n := 0
	for _, s := range bgSequences(noQuery(out)) {
		if s != bgReset {
			n++
		}
	}
	return n
}

func quitAfterSettle(p *tea.Program, out *lockedBuffer) error {
	time.Sleep(150 * time.Millisecond) // a second SET or query would show by now
	p.Quit()
	return nil
}

// ── Criterion 1 ──────────────────────────────────────────────────────────────

func TestProgramWithoutAutoNeverQueriesTheBackground(t *testing.T) {
	for _, name := range []string{"", "default", "github-dark"} {
		t.Run("app.theme="+name, func(t *testing.T) {
			a := autoRouter(t, autoConfig(name, "", ""))
			out, err := runProgramWithInput(t, themeProbe{a}, func(p *tea.Program, out *lockedBuffer, _ io.Writer) error {
				if err := waitOut(out, "the first background SET", func(s string) bool { return strings.Contains(s, bgSet) }); err != nil {
					return err
				}
				return quitAfterSettle(p, out)
			})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out, bgQuery) {
				t.Errorf("an OSC 11 query was written without app.theme: auto: %q", out)
			}
		})
	}
}

// ── Criteria 2 and 3: the query comes first, the answer picks the SET ────────

func TestProgramAutoAsksBeforeAnyBackgroundSetThenSetsTheThemeOnce(t *testing.T) {
	cases := []struct {
		name, reply, wantBg, wantTheme string
	}{
		{"light", lightOSC, lightBgOK, "github-light"},
		{"dark", darkOSC, wantDarkBg, "default"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := autoRouter(t, autoConfig("auto", "", ""))
			var atQuery string
			out, err := runProgramWithInput(t, themeProbe{a}, func(p *tea.Program, out *lockedBuffer, in io.Writer) error {
				if err := waitOut(out, "the background query", func(s string) bool { return strings.Contains(s, bgQuery) }); err != nil {
					return err
				}
				atQuery = out.String()
				if _, err := io.WriteString(in, tc.reply); err != nil {
					return err
				}
				if err := waitOut(out, "the theme's background", func(s string) bool { return strings.Contains(s, setOf(tc.wantBg)) }); err != nil {
					return err
				}
				return quitAfterSettle(p, out)
			})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(noQuery(atQuery), bgSet) {
				t.Errorf("a background SET was written before the query was out: %q", atQuery)
			}
			if got := setCount(out); got != 1 {
				t.Errorf("%d background SETs written, want exactly 1: %q", got, bgSequences(noQuery(out)))
			}
			if got := bgSequences(noQuery(out)); len(got) == 0 || got[0] != setOf(tc.wantBg) {
				t.Errorf("first background sequence = %q, want %q", got, setOf(tc.wantBg))
			}
			if a.themeResolution().Name != tc.wantTheme {
				t.Errorf("theme = %q, want %q", a.themeResolution().Name, tc.wantTheme)
			}
		})
	}
}

// ── Criterion 4: no reply, dark after the timeout ────────────────────────────

func TestProgramAutoWithoutAReplyFallsBackToDark(t *testing.T) {
	a := autoRouter(t, autoConfig("auto", "", ""))
	testutil.FastTimers(t, &themeDetectTimeout)
	out, err := runProgramWithInput(t, themeProbe{a}, func(p *tea.Program, out *lockedBuffer, _ io.Writer) error {
		if err := waitOut(out, "the background query", func(s string) bool { return strings.Contains(s, bgQuery) }); err != nil {
			return err
		}
		if err := waitOut(out, "the dark fallback SET", func(s string) bool { return strings.Contains(s, setOf(wantDarkBg)) }); err != nil {
			return err
		}
		return quitAfterSettle(p, out)
	})
	if err != nil {
		t.Fatal(err)
	}
	q, set := strings.Index(out, bgQuery), strings.Index(out, setOf(wantDarkBg))
	if q < 0 || set < q {
		t.Errorf("query at %d, first SET at %d: the query must come first", q, set)
	}
	if got := setCount(out); got != 1 {
		t.Errorf("%d background SETs written, want exactly 1: %q", got, bgSequences(noQuery(out)))
	}
}

// ── Step 6a on the bytes: auto chosen in session, nothing cached ─────────────

// The terminal was never asked and DevDesk's own background is in force: the
// router must reset it (OSC 111), let a frame carry that out, and only then
// query, otherwise the terminal answers with DevDesk's colour.
func TestProgramChoosingAutoInSessionResetsThenAsksThenSetsTheTheme(t *testing.T) {
	a := autoRouter(t, autoConfig("github-dark", "", ""))
	out, err := runProgramWithInput(t, themeProbe{a}, func(p *tea.Program, out *lockedBuffer, in io.Writer) error {
		if err := waitOut(out, "the named theme's SET", func(s string) bool { return strings.Contains(s, setOf("#0d1117")) }); err != nil {
			return err
		}
		p.Send(configuration.ConfigSavedMsg{Config: autoConfig("auto", "", ""), ThemeChanged: true})
		if err := waitOut(out, "the query", func(s string) bool { return strings.Contains(s, bgQuery) }); err != nil {
			return err
		}
		if _, err := io.WriteString(in, lightOSC); err != nil {
			return err
		}
		if err := waitOut(out, "the light SET", func(s string) bool { return strings.Contains(s, setOf(lightBgOK)) }); err != nil {
			return err
		}
		return quitAfterSettle(p, out)
	})
	if err != nil {
		t.Fatal(err)
	}
	q := strings.Index(out, bgQuery)
	r := strings.Index(out[strings.Index(out, setOf("#0d1117")):], bgReset)
	if q < 0 || r < 0 {
		t.Fatalf("query at %d, reset at %d in %q", q, r, out)
	}
	r += strings.Index(out, setOf("#0d1117"))
	if r > q {
		t.Errorf("the background reset (at %d) must be written before the query (at %d)", r, q)
	}
	seqs := bgSequences(noQuery(out[:q]))
	if len(seqs) == 0 || seqs[len(seqs)-1] != bgReset {
		t.Errorf("last background sequence before the query = %q, want a reset", seqs)
	}
	if got := bgSequences(noQuery(out[q:])); len(got) == 0 || got[0] != setOf(lightBgOK) {
		t.Errorf("first background sequence after the query = %q, want %q", got, setOf(lightBgOK))
	}
}

// ── Step 6a at the router level ──────────────────────────────────────────────

func queryDue(msgs []tea.Msg) (tea.Msg, bool) {
	for _, m := range msgs {
		if _, ok := m.(themeQueryDueMsg); ok {
			return m, true
		}
	}
	return nil, false
}

// chooseAutoInSession saves app.theme: auto over a router that has no cached
// answer (it started on a named theme) and returns what the save emitted.
func chooseAutoInSession(t *testing.T, a *App) []tea.Msg {
	t.Helper()
	_, cmd := a.Update(configuration.ConfigSavedMsg{Config: autoConfig("auto", "", ""), ThemeChanged: true})
	return testutil.Msgs(cmd)
}

func TestChoosingAutoInSessionWithoutAnAnswerDefersTheQuery(t *testing.T) {
	a := autoRouter(t, autoConfig("github-dark", "", ""))
	testutil.FastTimers(t, &themeDetectTimeout)
	msgs := chooseAutoInSession(t, a)
	if hasBackgroundRequest(msgs) {
		t.Error("the query went out at once; it must wait for the reset to be drawn")
	}
	if _, ok := queryDue(msgs); !ok {
		t.Errorf("no themeQueryDueMsg in %v", msgs)
	}
	if _, ok := firstOf[themeDetectTimeoutMsg](msgs); ok {
		t.Error("the timeout was armed before the query was sent")
	}
	if c := a.View().BackgroundColor; c != nil {
		t.Errorf("View().BackgroundColor = %s while waiting, want nil (that is what resets it)", bgHex(a))
	}
	if !a.themeDetectionPending() {
		t.Error("themeDetectionPending = false, want true while the query is deferred")
	}
	if posts := footerPosts(msgs); len(posts) != 0 {
		t.Errorf("posted footer messages: %+v", posts)
	}
}

func TestTheDeferredQueryAsksOnceAndArmsTheTimeout(t *testing.T) {
	a := autoRouter(t, autoConfig("github-dark", "", ""))
	testutil.FastTimers(t, &themeDetectTimeout)
	due, ok := queryDue(chooseAutoInSession(t, a))
	if !ok {
		t.Fatal("no themeQueryDueMsg")
	}
	_, cmd := a.Update(due)
	msgs := testutil.Msgs(cmd)
	requests := 0
	for _, m := range msgs {
		if isBackgroundRequest(m) {
			requests++
		}
	}
	if requests != 1 {
		t.Errorf("%d background requests, want 1: %v", requests, msgs)
	}
	if _, ok := firstOf[themeDetectTimeoutMsg](msgs); !ok {
		t.Errorf("no themeDetectTimeoutMsg armed: %v", msgs)
	}
	if c := a.View().BackgroundColor; c != nil {
		t.Errorf("View().BackgroundColor = %s while the answer is awaited, want nil", bgHex(a))
	}
}

func TestTheDeferredQueryThenTheAnswerPicksTheTheme(t *testing.T) {
	for _, tc := range []struct {
		name, theme, bg string
		color           color.Color
	}{
		{"light", "github-light", lightBgOK, color.White},
		{"dark", "default", wantDarkBg, color.Black},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := autoRouter(t, autoConfig("github-dark", "", ""))
			testutil.FastTimers(t, &themeDetectTimeout)
			due, _ := queryDue(chooseAutoInSession(t, a))
			if due == nil {
				t.Fatal("no themeQueryDueMsg")
			}
			a.Update(due)
			posts := footerPosts(answer(t, a, tc.color))
			if len(posts) != 0 {
				t.Errorf("posted footer messages: %+v", posts)
			}
			if got := a.themeResolution().Name; got != tc.theme {
				t.Errorf("theme = %q, want %q", got, tc.theme)
			}
			if got := bgHex(a); got != tc.bg {
				t.Errorf("View background = %s, want %s", got, tc.bg)
			}
		})
	}
}

func TestTheDeferredQueryTimeoutFallsBackToDarkSilently(t *testing.T) {
	a := autoRouter(t, autoConfig("github-dark", "", ""))
	testutil.FastTimers(t, &themeDetectTimeout)
	due, _ := queryDue(chooseAutoInSession(t, a))
	if due == nil {
		t.Fatal("no themeQueryDueMsg")
	}
	_, cmd := a.Update(due)
	timeout, ok := firstOf[themeDetectTimeoutMsg](testutil.Msgs(cmd))
	if !ok {
		t.Fatal("no timeout armed")
	}
	_, cmd = a.Update(timeout)
	if posts := footerPosts(testutil.Msgs(cmd)); len(posts) != 0 {
		t.Errorf("the timeout posted footer messages: %+v", posts)
	}
	if got := a.themeResolution().Name; got != "default" {
		t.Errorf("theme = %q, want the dark default", got)
	}
	if got := bgHex(a); got != wantDarkBg {
		t.Errorf("View background = %s, want %s", got, wantDarkBg)
	}
}

// A named theme chosen while the answer is awaited, before or after the query
// went out, cancels the wait and the deferred query.
func TestANamedThemeChosenWhileWaitingCancelsTheDeferredQuery(t *testing.T) {
	a := autoRouter(t, autoConfig("github-dark", "", ""))
	testutil.FastTimers(t, &themeDetectTimeout)
	due, _ := queryDue(chooseAutoInSession(t, a))
	if due == nil {
		t.Fatal("no themeQueryDueMsg")
	}
	saveThemeFooter(t, a, "default")
	_, cmd := a.Update(due)
	if hasBackgroundRequest(testutil.Msgs(cmd)) {
		t.Error("a cancelled deferred query still asked the terminal")
	}
	if a.themeDetectionPending() {
		t.Error("detection still pending after a named theme was chosen")
	}
	if got := bgHex(a); got != wantDarkBg {
		t.Errorf("View background = %s, want %s", got, wantDarkBg)
	}
}

func TestAStaleDeferredQueryIsIgnored(t *testing.T) {
	a := autoRouter(t, autoConfig("github-dark", "", ""))
	testutil.FastTimers(t, &themeDetectTimeout)
	due, _ := queryDue(chooseAutoInSession(t, a))
	if due == nil {
		t.Fatal("no themeQueryDueMsg")
	}
	_, cmd := a.Update(themeQueryDueMsg{gen: due.(themeQueryDueMsg).gen + 1000})
	if hasBackgroundRequest(testutil.Msgs(cmd)) {
		t.Error("a stale themeQueryDueMsg asked the terminal")
	}
}
