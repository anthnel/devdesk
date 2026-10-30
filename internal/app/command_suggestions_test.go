package app

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-89 step 5 (with step 0 / DEVDESK-101): the command line shows the
// suggestion list of docs/ux/command-suggestions.md instead of the horizontal
// view picker. These tests read the screen (View, stripped of ANSI) and the
// header's shortcuts; they do not touch the router's fields.

const maxListRows = 8

func screen(a *App) []string { return strings.Split(ansi.Strip(a.View()), "\n") }

// filetIndex returns the line of the list's bottom rule, or -1.
func filetIndex(lines []string) int {
	for i, l := range lines {
		if strings.HasPrefix(l, "├─") {
			return i
		}
	}
	return -1
}

// listRows returns the list rows (between the title line and the rule).
func listRows(t *testing.T, a *App) []string {
	t.Helper()
	lines := screen(a)
	end := filetIndex(lines)
	if end < 0 {
		t.Fatalf("no list rule on screen:\n%s", strings.Join(lines, "\n"))
	}
	start := -1
	for i, l := range lines[:end] {
		if strings.HasPrefix(l, "┌") {
			start = i
		}
	}
	if start < 0 {
		t.Fatalf("no title line above the list:\n%s", strings.Join(lines, "\n"))
	}
	return lines[start+1 : end]
}

// filet returns the text of the bottom rule.
func filet(t *testing.T, a *App) string {
	t.Helper()
	lines := screen(a)
	i := filetIndex(lines)
	if i < 0 {
		t.Fatalf("no list rule on screen:\n%s", strings.Join(lines, "\n"))
	}
	return lines[i]
}

func highlighted(rows []string) []string {
	var out []string
	for _, r := range rows {
		if strings.Contains(r, theme.IconCircleSmall) {
			out = append(out, r)
		}
	}
	return out
}

func press(t *testing.T, a *App, keys ...string) tea.Cmd {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		cmd = feedKey(t, a, testutil.Key(k))
	}
	return cmd
}

func nEntries() int { return len(command.Entries()) }

// ── Opening and filtering ────────────────────────────────────────────────────

func TestCtrlPShowsTheFullListWithNothingHighlighted(t *testing.T) {
	a := routerAt(t, &fakeView{}, 80, 24)
	press(t, a, "ctrl+p")

	rows := listRows(t, a)
	if len(rows) != maxListRows {
		t.Errorf("%d rows, want %d", len(rows), maxListRows)
	}
	if h := highlighted(rows); len(h) != 0 {
		t.Errorf("rows highlighted with nothing typed: %q", h)
	}
	want := fmt.Sprintf("%d available · ▼ %d more", nEntries(), nEntries()-maxListRows)
	if got := filet(t, a); !strings.Contains(got, want) {
		t.Errorf("rule %q lacks %q", got, want)
	}
	for _, r := range rows {
		if w := ansi.StringWidth(r); w != 80 {
			t.Errorf("row is %d cells wide, want 80: %q", w, r)
		}
	}
}

func TestTypingHighlightsTheBestRowAndCounts(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "co")

	want := command.NewSuggester().Suggest("co")
	rows := listRows(t, a)
	h := highlighted(rows)
	if len(h) != 1 || !strings.HasPrefix(strings.TrimLeft(strings.TrimPrefix(h[0], "│"), " "), theme.IconCircleSmall) ||
		!strings.Contains(h[0], want.Entries[0].Name) {
		t.Errorf("highlighted rows %q, want only the best match %q", h, want.Entries[0].Name)
	}
	if got, w := filet(t, a), fmt.Sprintf("%d of %d", len(want.Entries), nEntries()); !strings.Contains(got, w) {
		t.Errorf("rule %q lacks %q", got, w)
	}
}

func TestTheCurrentViewIsTaggedInTheList(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "dash")

	rows := listRows(t, a)
	if len(rows) == 0 || !strings.Contains(rows[0], "dashboard") || !strings.Contains(rows[0], "(current)") {
		t.Errorf("rows %q: the current view is not tagged", rows)
	}
}

func TestDownNineTimesKeepsTheHighlightVisibleWithExactCounters(t *testing.T) {
	a := commanding(t, &fakeView{})
	press(t, a, "down", "down", "down", "down", "down", "down", "down", "down", "down")

	entries := command.Entries()
	h := highlighted(listRows(t, a))
	if len(h) != 1 || !strings.Contains(h[0], entries[8].Name) {
		t.Fatalf("highlighted %q, want the ninth entry %q", h, entries[8].Name)
	}
	want := fmt.Sprintf("▲ 1 more · ▼ %d more", len(entries)-1-maxListRows)
	if got := filet(t, a); !strings.Contains(got, want) {
		t.Errorf("rule %q lacks %q", got, want)
	}
}

func TestUpAtTheTopDoesNotWrap(t *testing.T) {
	a := commanding(t, &fakeView{})
	press(t, a, "down", "up", "up")

	entries := command.Entries()
	h := highlighted(listRows(t, a))
	if len(h) != 1 || !strings.Contains(h[0], entries[0].Name) {
		t.Errorf("highlighted %q, want to stay on %q", h, entries[0].Name)
	}
	if got := filet(t, a); strings.Contains(got, "▲") {
		t.Errorf("rule %q claims rows above the first", got)
	}
}

func TestUpWithNothingHighlightedGoesToTheLastVisibleRow(t *testing.T) {
	a := commanding(t, &fakeView{})
	press(t, a, "up")

	entries := command.Entries()
	h := highlighted(listRows(t, a))
	if len(h) != 1 || !strings.Contains(h[0], entries[maxListRows-1].Name) {
		t.Errorf("highlighted %q, want %q", h, entries[maxListRows-1].Name)
	}
}

// ── tab ──────────────────────────────────────────────────────────────────────

func TestTabNoLongerOpensTheBreadcrumbNorShowsAPosition(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "co")
	press(t, a, "tab")

	header := ansi.Strip(a.renderHeader())
	if strings.Contains(header, "[1/") || strings.Contains(header, "[2/") {
		t.Errorf("the header still shows a [i/n] position:\n%s", header)
	}
	if strings.Contains(header, "agent-usage") {
		t.Errorf("the header shows the view breadcrumb:\n%s", header)
	}
}

func TestTabCompletesTheHighlightedRowWithoutRunningIt(t *testing.T) {
	cases := []struct{ typed, want string }{
		{"co", "configuration"},
		{"context", "context "},
	}
	for _, c := range cases {
		t.Run(c.typed, func(t *testing.T) {
			a := commanding(t, &fakeView{})
			typeCommand(t, a, c.typed)
			press(t, a, "tab")

			if got := a.commandInput.Value(); got != c.want {
				t.Errorf("line holds %q, want %q", got, c.want)
			}
			if !a.commandMode || a.currentView != command.ViewDashboard {
				t.Errorf("tab ran something: commandMode=%v view=%s", a.commandMode, a.currentView)
			}
		})
	}
}

func TestTabCompletesTheRowChosenWithTheArrows(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "co")
	press(t, a, "down", "tab")

	if got := a.commandInput.Value(); got != "containers" {
		t.Errorf("line holds %q, want containers", got)
	}
}

func TestTabAndShiftTabDoNothingWithoutAHighlightedRow(t *testing.T) {
	for _, typed := range []string{"", "zz"} {
		a := commanding(t, &fakeView{})
		typeCommand(t, a, typed)
		before := a.View()
		press(t, a, "tab")
		if a.commandInput.Value() != typed || a.View() != before {
			t.Errorf("tab with %q and no highlight changed the screen or the line (%q)", typed, a.commandInput.Value())
		}
	}
}

func TestShiftTabDoesNothing(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "co")
	before := a.View()
	press(t, a, "shift+tab")

	if a.commandInput.Value() != "co" || a.View() != before {
		t.Errorf("shift+tab changed the line (%q) or the screen", a.commandInput.Value())
	}
}

// ── enter ────────────────────────────────────────────────────────────────────

func TestEnterOpensTheBestMatch(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "co")
	press(t, a, "enter")

	if a.currentView != command.ViewConfiguration || a.commandMode {
		t.Errorf("view=%s commandMode=%v, want configuration with the line closed", a.currentView, a.commandMode)
	}
}

func TestEnterOpensTheRowChosenWithTheArrows(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "co")
	press(t, a, "down", "enter")

	if a.currentView != command.ViewContainers {
		t.Errorf("view=%s, want containers", a.currentView)
	}
}

// C10: a line emptied of its text must not run a command nobody chose.
func TestEnterOnAnEmptyLineChangesNothingAndWarns(t *testing.T) {
	setups := map[string]func(*testing.T, *App){
		"opened empty":            func(*testing.T, *App) {},
		"letter typed and erased": func(t *testing.T, a *App) { typeCommand(t, a, "q"); press(t, a, "backspace") },
	}
	for name, setup := range setups {
		t.Run(name, func(t *testing.T) {
			view := &fakeView{}
			a := commanding(t, view)
			setup(t, a)
			before := *view

			cmd := press(t, a, "enter")

			if _, quit := testutil.MsgOf[tea.QuitMsg](cmd); quit {
				t.Fatal("enter on an empty line quit the application")
			}
			if a.currentView != command.ViewDashboard || !a.commandMode {
				t.Errorf("view=%s commandMode=%v: the empty enter acted", a.currentView, a.commandMode)
			}
			if !reflect.DeepEqual(before, *view) {
				t.Error("the empty enter reached the view")
			}
			if got, want := filet(t, a), theme.IconWarning+" Type a name or choose one"; !strings.Contains(got, want) {
				t.Errorf("rule %q lacks %q", got, want)
			}
		})
	}
}

// ── No match ─────────────────────────────────────────────────────────────────

func TestNoMatchShowsAnUnselectableLine(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "zz")

	rows := listRows(t, a)
	if len(rows) == 0 || !strings.Contains(rows[0], `No match for "zz"`) {
		t.Errorf("rows %q lack the no-match line", rows)
	}
	if h := highlighted(rows); len(h) != 0 {
		t.Errorf("the no-match line is highlighted: %q", h)
	}
	if got := filet(t, a); !strings.Contains(got, fmt.Sprintf("0 of %d", nEntries())) {
		t.Errorf("rule %q lacks the 0 count", got)
	}
}

func TestEnterOnNoMatchWarnsAndKeepsTheLineOpen(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "zz")

	cmd := press(t, a, "enter")

	if a.currentView != command.ViewDashboard || !a.commandMode || a.commandInput.Value() != "zz" {
		t.Errorf("view=%s commandMode=%v line=%q", a.currentView, a.commandMode, a.commandInput.Value())
	}
	want := theme.IconWarning + ` "zz" is not a view or a command`
	if got := filet(t, a); !strings.Contains(got, want) {
		t.Errorf("rule %q lacks %q", got, want)
	}
	assertNoTimer(t, cmd)
}

// The warning lasts until the next keystroke, arrows included.
func TestTheWarningGoesAtTheNextKey(t *testing.T) {
	for _, key := range []string{"z", "down", "up", "backspace"} {
		t.Run(key, func(t *testing.T) {
			a := commanding(t, &fakeView{})
			typeCommand(t, a, "zz")
			press(t, a, "enter")
			press(t, a, key)

			if got := filet(t, a); strings.Contains(got, "is not a view or a command") {
				t.Errorf("rule %q still carries the warning after %q", got, key)
			}
		})
	}
}

// assertNoTimer fails when the Cmd blocks, which is what tea.Tick does.
func assertNoTimer(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	done := make(chan struct{})
	go func() { cmd(); close(done) }()
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Error("the warning started a timer; it must last until the next keystroke")
	}
}

// ── esc ──────────────────────────────────────────────────────────────────────

func TestEscClosesTheLineAndTheListInOnePressLeavingTheViewUntouched(t *testing.T) {
	view := &fakeView{body: "body"}
	a := routerAt(t, view, 80, 24)
	before := *view
	beforeView := a.currentView

	press(t, a, "ctrl+p")
	typeCommand(t, a, "co")
	press(t, a, "down")
	press(t, a, "esc")

	if a.commandMode {
		t.Error("esc left the line open")
	}
	if filetIndex(screen(a)) >= 0 {
		t.Error("the list survived esc")
	}
	if a.currentView != beforeView || !reflect.DeepEqual(before, *view) {
		t.Error("the view changed between opening and closing the line")
	}
}

// ── Q3: the argument stage ───────────────────────────────────────────────────

func TestAnArgumentShowsTheSingleContextRow(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "ctx work")

	rows := listRows(t, a)
	h := highlighted(rows)
	if len(rows) < 1 || len(h) != 1 || !strings.Contains(h[0], "context <name>") {
		t.Errorf("rows %q, want the highlighted context <name> row", rows)
	}
	if got := filet(t, a); !strings.Contains(got, fmt.Sprintf("1 of %d", nEntries())) {
		t.Errorf("rule %q lacks the 1 count", got)
	}
}

func TestEnterOnAnArgumentRunsTheTypedTextAsIs(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "ctx phaseq")

	cmd := press(t, a, "enter")

	done, ok := testutil.MsgOf[ContextSwitchCompleteMsg](cmd)
	if !ok || done.ContextName != "phaseq" {
		t.Errorf("enter produced %T (%+v), want a switch to phaseq", testutil.Msg(cmd), done)
	}
}

func TestTabDoesNothingOnAnArgument(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "ctx work")
	press(t, a, "tab")

	if got := a.commandInput.Value(); got != "ctx work" {
		t.Errorf("tab rewrote the argument line to %q", got)
	}
}

// ── Header shortcuts ─────────────────────────────────────────────────────────

func TestTheHeaderOffersTheLineVocabularyWhileTyping(t *testing.T) {
	view := &fakeView{shortcuts: shortcut.Shortcuts{{Key: "Z", Description: "Zap the thing"}}}
	a := commanding(t, view)

	sc := a.commandLineShortcuts()
	want := map[string]string{"↑↓": "Choose", "tab": "Complete", "enter": "Open", "esc": "Close"}
	for key, desc := range want {
		found := false
		for _, s := range sc {
			if s.Key == key && s.Description == desc {
				found = true
			}
		}
		if !found {
			t.Errorf("shortcuts %v lack %s %s", sc, key, desc)
		}
	}
	if header := ansi.Strip(a.renderHeader()); strings.Contains(header, "Zap the thing") || !strings.Contains(header, "Complete") {
		t.Errorf("the header still shows the view's shortcuts instead of the line's:\n%s", header)
	}
	for _, s := range sc {
		if r := []rune(s.Description); len(r) == 0 || !unicode.IsUpper(r[0]) {
			t.Errorf("description %q breaks Rule 137", s.Description)
		}
	}
}

func TestTheShortcutKeysDoNotChangeAcrossTheFourStates(t *testing.T) {
	states := []struct {
		typed       string
		highlighted bool
	}{
		{"", false}, {"co", true}, {"dash", true}, {"zz", false},
	}
	var reference []string
	for _, s := range states {
		a := commanding(t, &fakeView{})
		typeCommand(t, a, s.typed)
		sc := a.commandLineShortcuts()

		keys := testutil.ShortcutKeys(sc)
		if reference == nil {
			reference = keys
		}
		if len(keys) == 0 || !reflect.DeepEqual(keys, reference) {
			t.Errorf("%q: keys %v, want %v in every state", s.typed, keys, reference)
		}
		for _, k := range []string{"tab", "enter"} {
			if got := testutil.ShortcutDisabled(sc, k); got == s.highlighted {
				t.Errorf("%q: %s disabled=%v with highlighted=%v", s.typed, k, got, s.highlighted)
			}
		}
		for _, k := range []string{"↑↓", "esc"} {
			if !testutil.ShortcutEnabled(sc, k) {
				t.Errorf("%q: %s should always be enabled", s.typed, k)
			}
		}
	}
}

// ── Layout ───────────────────────────────────────────────────────────────────

// Hauteur utile < 2 (no list, usable line, no overflow) is left to step 7.
func TestTheLayoutHoldsWithTheLineOpen(t *testing.T) {
	sizes := [][2]int{{80, 24}, {120, 40}, {60, 30}, {40, 16}}
	steps := map[string]func(*testing.T, *App){
		"empty":    func(*testing.T, *App) {},
		"matches":  func(t *testing.T, a *App) { typeCommand(t, a, "co") },
		"no match": func(t *testing.T, a *App) { typeCommand(t, a, "zz") },
		"warning":  func(t *testing.T, a *App) { typeCommand(t, a, "zz"); press(t, a, "enter") },
		"scrolled": func(t *testing.T, a *App) {
			press(t, a, "down", "down", "down", "down", "down", "down", "down", "down", "down")
		},
	}
	for _, sz := range sizes {
		for name, step := range steps {
			t.Run(fmt.Sprintf("%dx%d/%s", sz[0], sz[1], name), func(t *testing.T) {
				a := routerAt(t, &fakeView{body: "body"}, sz[0], sz[1])
				press(t, a, "ctrl+p")
				step(t, a)

				if h := lipgloss.Height(a.View()); h != sz[1] {
					t.Errorf("View is %d lines, want %d", h, sz[1])
				}
				if h := lipgloss.Height(a.renderHeader()); h != 10 {
					t.Errorf("header is %d lines, want 10", h)
				}
				for _, r := range listRows(t, a) {
					if w := ansi.StringWidth(r); w != sz[0] {
						t.Errorf("list row is %d cells wide, want %d: %q", w, sz[0], r)
					}
				}
			})
		}
	}
}

// ── Guards ───────────────────────────────────────────────────────────────────

func TestTabIsADeclaredExceptionOfTheCommandLine(t *testing.T) {
	for _, e := range keymap.DeclaredExceptions() {
		if e.Key == "tab" && strings.Contains(e.Surface, "command-line") && e.Why != "" {
			return
		}
	}
	t.Error(`keymap.DeclaredExceptions() has no "tab" exception for the app/command-line surface (Rule 135 vs Q1-a)`)
}

// The picker and the completion engine are gone, and stay gone.
func TestTheOldPickerAndCompletionCodeIsGone(t *testing.T) {
	banned := []string{"viewPicker", "renderViewPicker", "pickerWindow", "completionEngine", "SetSuggestions", "CompletionEngine"}
	root := filepath.Join("..", "..", "internal")
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		for i, line := range strings.Split(string(src), "\n") {
			for _, word := range banned {
				if strings.Contains(line, word) {
					t.Errorf("%s:%d still mentions %s", path, i+1, word)
				}
			}
		}
		return nil
	})
	if _, err := os.Stat(filepath.Join(root, "command", "completion.go")); err == nil {
		t.Error("internal/command/completion.go still exists")
	}
}
