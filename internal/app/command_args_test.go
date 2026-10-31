package app

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-88 step 6 (DEVDESK-115): the argument stage of the command line for
// :ctx — docs/ux/context-switch.md. Executing a switch or a creation is step 7;
// here a switch is observed through switchingTo and the overlay, and the Cmd
// is never run.

// argRouter builds a router over an isolated HOME holding default, web, work;
// "default" is current. No snapshot is loaded yet.
func argRouter(t *testing.T, names ...string) *App {
	t.Helper()
	isolateHome(t)
	if len(names) == 0 {
		names = []string{"default", "web", "work"}
	}
	for _, n := range names {
		if err := config.SaveContext(testConfig(), n); err != nil {
			t.Fatalf("SaveContext(%q): %v", n, err)
		}
	}
	a := newWithSize(testConfig(), 120, 40)
	a.currentContext = names[0]
	// A plain view keeps the viewport height predictable (the dashboard adds tabs).
	a.views[command.ViewDashboard] = &fakeView{}
	a.resize(120, 40)
	return a
}

// ctxOpen opens the command line, applies the context load, and types text.
func ctxOpen(t *testing.T, a *App, text string) {
	t.Helper()
	loadContexts(t, a)
	typeCommand(t, a, text)
}

func hasRowWith(rows []string, sub string) bool {
	for _, r := range rows {
		if strings.Contains(r, sub) {
			return true
		}
	}
	return false
}

// footerOf presses key and returns the footer post it produced, if any.
func footerOf(t *testing.T, a *App, key string) (components.PostFooterMsg, bool) {
	t.Helper()
	testutil.FastTimers(t, &components.FooterMsgDuration)
	return testutil.MsgOf[components.PostFooterMsg](press(t, a, key))
}

// ── Display ──────────────────────────────────────────────────────────────────

func TestCtxArgumentListShowsContextsThenReservedWords(t *testing.T) {
	a := argRouter(t)
	ctxOpen(t, a, "ctx ")

	rows := listRows(t, a)
	if len(rows) != 5 {
		t.Fatalf("%d rows, want 5 (3 contexts, -, new): %q", len(rows), rows)
	}
	for i, want := range []string{"default (current)", "web", "work", "-", "new"} {
		if !strings.Contains(rows[i], want) {
			t.Errorf("row %d = %q, want it to contain %q", i, rows[i], want)
		}
	}
	if got := filet(t, a); !strings.Contains(got, "Contexts · 3 of 3") {
		t.Errorf("rule %q lacks the heading %q", got, "Contexts · 3 of 3")
	}
}

func TestCtxArgumentFilterNarrowsRowsAndHeading(t *testing.T) {
	a := argRouter(t)
	ctxOpen(t, a, "ctx w")

	rows := listRows(t, a)
	if len(rows) != 2 || !strings.Contains(rows[0], "web") || !strings.Contains(rows[1], "work") {
		t.Errorf("rows %q, want web and work", rows)
	}
	if got := filet(t, a); !strings.Contains(got, "Contexts · 2 of 3") {
		t.Errorf("rule %q lacks %q", got, "Contexts · 2 of 3")
	}
}

func TestCtxArgumentListScrollsInAWindowOfEight(t *testing.T) {
	a := argRouter(t, "a01", "a02", "a03", "a04", "a05", "a06", "a07", "a08", "a09", "a10")
	ctxOpen(t, a, "ctx ")

	if rows := listRows(t, a); len(rows) != maxListRows {
		t.Errorf("%d rows, want %d", len(rows), maxListRows)
	}
	if got := filet(t, a); !strings.Contains(got, "▼ 4 more") {
		t.Errorf("rule %q lacks %q (12 rows, 8 shown)", got, "▼ 4 more")
	}
	press(t, a, "tab") // highlights the first row, offset stays
	for i := 0; i < 10; i++ {
		press(t, a, "down")
	}
	if got := filet(t, a); !strings.Contains(got, "▲") {
		t.Errorf("rule %q shows no scrolled-out rows after moving down", got)
	}
}

func TestCtxArgumentListIsLoadingUntilTheSnapshotArrives(t *testing.T) {
	a := argRouter(t)
	a.enterCommandMode() // the load Cmd is not applied
	typeCommand(t, a, "ctx ")

	if rows := listRows(t, a); !hasRowWith(rows, "Loading contexts") {
		t.Errorf("rows %q, want the loading message", rows)
	}
}

func TestCtxArgumentWithNoMatchSaysSo(t *testing.T) {
	a := argRouter(t)
	ctxOpen(t, a, "ctx wrok")

	rows := listRows(t, a)
	if !hasRowWith(rows, `No context starts with "wrok"`) {
		t.Errorf("rows %q, want the empty message", rows)
	}
	if h := highlighted(rows); len(h) != 0 {
		t.Errorf("highlighted %q on an empty list", h)
	}
}

// ── Default highlight ────────────────────────────────────────────────────────

func TestCtxArgumentDefaultHighlight(t *testing.T) {
	cases := []struct {
		line string
		want string // substring of the single highlighted row, "" for none
	}{
		{"ctx work", "work"}, // exact match
		{"ctx web", "web"},
		{"ctx wo", "work"}, // the only match, valid input
		{"ctx w", ""},      // two matches: nothing chosen (criterion 1)
		{"ctx ", ""},       // everything matches
		{"ctx Wo", ""},     // single match but invalid input
		{"ctx wrok", ""},
	}
	for _, c := range cases {
		t.Run(c.line, func(t *testing.T) {
			a := argRouter(t)
			ctxOpen(t, a, c.line)
			h := highlighted(listRows(t, a))
			switch {
			case c.want == "" && len(h) != 0:
				t.Errorf("highlighted %q, want none", h)
			case c.want != "" && (len(h) != 1 || !strings.Contains(h[0], c.want)):
				t.Errorf("highlighted %q, want only %q", h, c.want)
			}
		})
	}
}

// ── tab ──────────────────────────────────────────────────────────────────────

func TestCtxTabHighlightsFirstRowThenCompletes(t *testing.T) {
	a := argRouter(t)
	ctxOpen(t, a, "ctx w")

	press(t, a, "tab")
	if got := a.commandInput.Value(); got != "ctx w" {
		t.Errorf("first tab rewrote the line to %q", got)
	}
	if h := highlighted(listRows(t, a)); len(h) != 1 || !strings.Contains(h[0], "web") {
		t.Errorf("first tab highlighted %q, want web", h)
	}

	press(t, a, "tab")
	if got := a.commandInput.Value(); got != "ctx web" {
		t.Errorf("second tab wrote %q, want %q", got, "ctx web")
	}
}

func TestCtxTabWithAHighlightedRowCompletesAtOnce(t *testing.T) {
	a := argRouter(t)
	ctxOpen(t, a, "ctx wo")

	press(t, a, "tab")
	if got := a.commandInput.Value(); got != "ctx work" {
		t.Errorf("tab wrote %q, want %q", got, "ctx work")
	}
}

func TestCtxTabOnNewLeavesASpaceForTheName(t *testing.T) {
	a := argRouter(t)
	ctxOpen(t, a, "ctx n")

	press(t, a, "tab")
	if got := a.commandInput.Value(); got != "ctx new " {
		t.Errorf("tab wrote %q, want %q", got, "ctx new ")
	}
}

func TestCtxTabWithNoRowsDoesNothing(t *testing.T) {
	a := argRouter(t)
	ctxOpen(t, a, "ctx wrok")

	press(t, a, "tab")
	press(t, a, "tab")
	if got := a.commandInput.Value(); got != "ctx wrok" {
		t.Errorf("tab rewrote the line to %q", got)
	}
}

func TestTabStillCompletesTheCommandWord(t *testing.T) {
	a := argRouter(t)
	ctxOpen(t, a, "ctx")

	press(t, a, "tab")
	if got := a.commandInput.Value(); got != "ctx " && got != "context " {
		t.Errorf("tab on the command stage wrote %q", got)
	}
}

// ── enter: switching ─────────────────────────────────────────────────────────

func requireSwitchTo(t *testing.T, a *App, name string) {
	t.Helper()
	if a.commandMode {
		t.Error("the command line stayed open on a switch")
	}
	if a.switchingTo != name {
		t.Errorf("switchingTo = %q, want %q", a.switchingTo, name)
	}
	if want := "Switching to context " + name + "..."; !strings.Contains(a.View(), want) {
		t.Errorf("the overlay %q is not on screen", want)
	}
}

// Criterion 2: ctx wo, tab, enter switches to work.
func TestCtxPrefixTabEnterSwitches(t *testing.T) {
	a := argRouter(t)
	ctxOpen(t, a, "ctx wo")
	press(t, a, "tab")

	press(t, a, "enter")
	requireSwitchTo(t, a, "work")
}

func TestCtxExactNameEnterSwitches(t *testing.T) {
	a := argRouter(t)
	ctxOpen(t, a, "ctx web")

	press(t, a, "enter")
	requireSwitchTo(t, a, "web")
}

func TestCtxSingleMatchEnterSwitchesWithoutTab(t *testing.T) {
	a := argRouter(t)
	ctxOpen(t, a, "ctx wo")

	press(t, a, "enter")
	requireSwitchTo(t, a, "work")
}

// Any highlighted row at the argument stage is a choice, arrows included.
func TestCtxArrowChoiceCountsAsAChoice(t *testing.T) {
	a := argRouter(t)
	ctxOpen(t, a, "ctx w")
	press(t, a, "down")

	press(t, a, "enter")
	requireSwitchTo(t, a, "web")
}

func TestCtxDashSwitchesToThePreviousContext(t *testing.T) {
	a := argRouter(t)
	a.previousContext = "work"
	ctxOpen(t, a, "ctx -")

	press(t, a, "enter")
	requireSwitchTo(t, a, "work")
}

// ── enter: staying open ──────────────────────────────────────────────────────

// Criterion 3: an ambiguous prefix with nothing chosen switches nothing.
func TestCtxAmbiguousPrefixKeepsTheLineOpenWithANotice(t *testing.T) {
	a := argRouter(t)
	ctxOpen(t, a, "ctx w")

	cmd := press(t, a, "enter")

	if a.switchingTo != "" {
		t.Errorf("switched to %q on an ambiguous prefix", a.switchingTo)
	}
	if !a.commandMode {
		t.Fatal("the command line closed")
	}
	if got := filet(t, a); !strings.Contains(got, `"w" matches web, work — type more letters or press tab`) {
		t.Errorf("rule %q lacks the ambiguity notice", got)
	}
	testutil.FastTimers(t, &components.FooterMsgDuration)
	if _, posted := testutil.MsgOf[components.PostFooterMsg](cmd); posted {
		t.Error("a footer message was posted; the notice belongs to the list's rule")
	}
}

func TestCtxNewWithoutANameKeepsTheLineOpenWithUsage(t *testing.T) {
	for _, line := range []string{"ctx new", "ctx new "} {
		t.Run(line, func(t *testing.T) {
			a := argRouter(t)
			ctxOpen(t, a, line)

			press(t, a, "enter")

			if !a.commandMode {
				t.Fatal("the command line closed")
			}
			if got := filet(t, a); !strings.Contains(got, "Usage: :ctx new <name>") {
				t.Errorf("rule %q lacks the usage notice", got)
			}
		})
	}
}

func TestCtxEnterBeforeTheSnapshotArrivesKeepsTheLineOpen(t *testing.T) {
	a := argRouter(t)
	a.enterCommandMode() // load not applied
	typeCommand(t, a, "ctx work")

	press(t, a, "enter")

	if !a.commandMode || a.switchingTo != "" {
		t.Errorf("commandMode=%v switchingTo=%q, want an open line and no switch", a.commandMode, a.switchingTo)
	}
	if got := filet(t, a); !strings.Contains(got, "Contexts are still loading — try again") {
		t.Errorf("rule %q lacks the loading notice", got)
	}
}

// ── enter: refusals ──────────────────────────────────────────────────────────

func TestCtxRefusalsCloseTheLineAndPostTheFooter(t *testing.T) {
	cases := []struct {
		name, line string
		level      components.Level
		text       string
	}{
		{"E1b near miss", "ctx wrok", components.LevelError,
			`Context "wrok" not found — did you mean "work"? Use :ctx new wrok to create it`},
		{"E1 unknown", "ctx zzz", components.LevelError,
			`Context "zzz" not found — use :ctx new zzz to create it`},
		{"E4 invalid", "ctx Work", components.LevelError,
			`Invalid context name "Work" — use lowercase letters, digits and hyphens`},
		{"E4b reserved", "ctx new new", components.LevelError,
			`"new" is reserved — choose another name`},
		{"E5 exists", "ctx new work", components.LevelWarning,
			`Context "work" already exists — use :ctx work to switch`},
		{"E3 no previous", "ctx -", components.LevelWarning,
			"No previous context in this session"},
		{"E7 already", "ctx default", components.LevelInfo,
			`Already on context "default"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := argRouter(t)
			ctxOpen(t, a, c.line)

			post, ok := footerOf(t, a, "enter")

			if !ok {
				t.Fatal("no footer message posted")
			}
			if post.Level != c.level || post.Text != c.text {
				t.Errorf("footer = level %v %q, want level %v %q", post.Level, post.Text, c.level, c.text)
			}
			if a.commandMode {
				t.Error("the command line stayed open on a refusal")
			}
			if a.switchingTo != "" {
				t.Errorf("a refusal started a switch to %q", a.switchingTo)
			}
		})
	}
}

// Criterion 3 of the story: a typo creates nothing.
func TestCtxTypoCreatesNoContext(t *testing.T) {
	a := argRouter(t)
	ctxOpen(t, a, "ctx wrok")
	footerOf(t, a, "enter")

	for _, name := range []string{"wrok", "-"} {
		if exists, _ := config.ContextExists(name); exists {
			t.Errorf("context %q was created", name)
		}
	}
}

// ── enter: list, creation, other commands ────────────────────────────────────

func TestCtxWithNoArgumentOpensTheListOverlay(t *testing.T) {
	for _, line := range []string{"ctx", "ctx "} {
		t.Run(line, func(t *testing.T) {
			a := argRouter(t)
			ctxOpen(t, a, line)

			cmd := press(t, a, "enter")

			list, ok := testutil.MsgOf[ContextListMsg](cmd)
			if !ok {
				t.Fatalf("enter produced %T, want the context list", testutil.Msg(cmd))
			}
			if list.Current == "" {
				t.Error("the list does not say which context is current")
			}
			if a.commandMode {
				t.Error("the command line stayed open")
			}
		})
	}
}

func TestCtxNewWithANameClosesTheLineWithoutRefusal(t *testing.T) {
	a := argRouter(t)
	ctxOpen(t, a, "ctx new staging")

	post, posted := footerOf(t, a, "enter")

	if posted {
		t.Errorf("a valid creation was refused: %q", post.Text)
	}
	if a.commandMode {
		t.Error("the command line stayed open")
	}
}

// ── esc (A7) ─────────────────────────────────────────────────────────────────

func TestEscWhileTypingACtxArgumentHasNoSideEffect(t *testing.T) {
	a := argRouter(t)
	ctxOpen(t, a, "ctx wo")
	press(t, a, "tab")

	cmd := press(t, a, "esc")

	if a.commandMode {
		t.Error("esc left the line open")
	}
	if a.switchingTo != "" {
		t.Errorf("esc started a switch to %q", a.switchingTo)
	}
	testutil.FastTimers(t, &components.FooterMsgDuration)
	for _, m := range testutil.Msgs(cmd) {
		switch m.(type) {
		case ContextSwitchCompleteMsg, ContextSwitchErrorMsg, components.PostFooterMsg:
			t.Errorf("esc produced %T", m)
		}
	}
	names, _ := config.ListContexts()
	if len(names) != 3 {
		t.Errorf("contexts on disk = %v, want the 3 that existed", names)
	}
}
