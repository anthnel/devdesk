package about

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/charmbracelet/x/ansi"
)

// DEVDESK-791: `W` opens the online help in the browser.
//
// This file holds what does not need the opener seam (`openBrowser`), so it
// compiles and runs on its own. The behaviour that goes through the seam is in
// online_help_open_test.go.

// wantHelpURL is spelled out here on purpose: the constant under test must not
// be able to drift without a test noticing, and a user who bookmarked the
// address in the docs expects exactly this one.
const wantHelpURL = "https://anthnel.github.io/devdesk/"

// sizedAt builds the screen for a terminal of the given width and height.
func sizedAt(t *testing.T, width, height int) Model {
	t.Helper()

	updated, _ := New(&config.Config{}).Update(testutil.Resize(width, height))
	return updated.(Model)
}

// pressCmd presses one key and returns the model and the command Update asked
// for. press drops the command; here it is the thing under test.
func pressCmd(t *testing.T, m Model, key string) (Model, tea.Cmd) {
	t.Helper()

	updated, cmd := m.Update(testutil.Key(key))
	return updated.(Model), cmd
}

// ── The header (Rules 130, 137, 138) ─────────────────────────────────────────

func TestTheHeaderOffersTheOnlineHelp(t *testing.T) {
	shortcuts := sizedAt(t, 100, 20).GetShortcuts()

	if !testutil.HasShortcut(shortcuts, keymap.Web) {
		t.Fatalf("GetShortcuts() = %v, want %q advertised", testutil.ShortcutKeys(shortcuts), keymap.Web)
	}
	// Rule 130: the key does not depend on any state, so it is never greyed.
	if !testutil.ShortcutEnabled(shortcuts, keymap.Web) {
		t.Errorf("%q is greyed, but nothing on this screen can make opening the help unavailable", keymap.Web)
	}
	for _, s := range shortcuts {
		if s.Key == keymap.Web && s.Description != "Open online help" {
			t.Errorf("description of %q = %q, want %q", keymap.Web, s.Description, "Open online help")
		}
	}
}

// The new key sits next to Help, so the two ways to get help read together.
func TestTheOnlineHelpIsListedNextToHelp(t *testing.T) {
	keys := testutil.ShortcutKeys(sizedAt(t, 100, 20).GetShortcuts())

	web, help := slices.Index(keys, keymap.Web), slices.Index(keys, "?")
	if web < 0 || help < 0 {
		t.Fatalf("shortcut keys = %v, want both %q and %q", keys, keymap.Web, "?")
	}
	if gap := web - help; gap != 1 && gap != -1 {
		t.Errorf("shortcut keys = %v, want %q right next to %q", keys, keymap.Web, "?")
	}
}

// Rules 137 and 138: ctrl+p is learned once and never repeated in the header,
// and scrolling is not advertised.
func TestTheHeaderStaysFreeOfUniversalKeys(t *testing.T) {
	shortcuts := sizedAt(t, 100, 20).GetShortcuts()

	for _, key := range []string{keymap.CommandMode, "ctrl+r", "up", "down", "↑↓", "pgup", "pgdown", "home", "end"} {
		if testutil.HasShortcut(shortcuts, key) {
			t.Errorf("GetShortcuts() advertises %q, which Rules 137/138 keep out of the header", key)
		}
	}
}

// Rule 130: the set of keys is the same wherever the body is scrolled to.
func TestTheOnlineHelpKeyDoesNotDependOnTheScrollPosition(t *testing.T) {
	m := sizedAt(t, 100, 5)
	before := testutil.ShortcutKeys(m.GetShortcuts())

	m = press(t, m, "end")
	if after := testutil.ShortcutKeys(m.GetShortcuts()); !equal(before, after) {
		t.Errorf("the shortcut keys changed from %v to %v when the screen scrolled", before, after)
	}
	if !slices.Contains(before, keymap.Web) {
		t.Errorf("shortcut keys = %v, want %q among them", before, keymap.Web)
	}
}

// ── The help (Rule 114) ──────────────────────────────────────────────────────

func TestTheHelpListsTheOnlineHelpKeyAndTheAddress(t *testing.T) {
	content := sizedAt(t, 100, 20).GetHelpContent()

	var binding string
	found := false
	for _, kb := range content.KeyBindings {
		if kb.Key == keymap.Web {
			binding, found = kb.Description, true
		}
	}
	if !found {
		t.Fatalf("GetHelpContent() key bindings do not list %q", keymap.Web)
	}

	// The address is in the key's own line, so the reader who cannot open a
	// browser (SSH, no desktop) still reads where to go.
	if !strings.Contains(binding, wantHelpURL) {
		t.Errorf("the %q binding says %q, want it to carry %q", keymap.Web, binding, wantHelpURL)
	}
}

// What the screen already did keeps being documented.
func TestTheHelpStillDocumentsTheOtherKeys(t *testing.T) {
	documented := map[string]bool{}
	for _, kb := range sizedAt(t, 100, 20).GetHelpContent().KeyBindings {
		documented[kb.Key] = true
	}

	for _, key := range []string{"↑/↓", keymap.CommandMode, "?"} {
		if !documented[key] {
			t.Errorf("the help no longer documents %q", key)
		}
	}
}

// ── The Documentation line ───────────────────────────────────────────────────

// The line sits in the Project section, under Source, as plain copyable text:
// the whole address, on one row, at the widths the story is checked at.
func TestTheProjectSectionShowsTheDocumentationAddress(t *testing.T) {
	for _, width := range []int{80, 120} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			rows := strings.Split(ansi.Strip(sizedAt(t, width, 40).Render()), "\n")

			project, source, doc := -1, -1, -1
			for i, row := range rows {
				switch {
				case strings.TrimSpace(row) == "Project":
					project = i
				case strings.Contains(row, "Source"):
					source = i
				case strings.Contains(row, "Documentation"):
					doc = i
				}
			}
			if doc < 0 {
				t.Fatalf("no row says %q; rows:\n%s", "Documentation", strings.Join(rows, "\n"))
			}
			if project < 0 || project >= source || source >= doc {
				t.Errorf("rows: Project=%d Source=%d Documentation=%d, want them in that order, Documentation in the Project section", project, source, doc)
			}

			// Plain text: after its label the row is the address, whole, with
			// nothing around it that a copy would drag along.
			value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rows[doc]), "Documentation"))
			if value != wantHelpURL {
				t.Errorf("Documentation row value = %q, want exactly %q", value, wantHelpURL)
			}
		})
	}
}

// The Source line stays as it was, and is still the repository.
func TestTheSourceLineIsUnchanged(t *testing.T) {
	body := ansi.Strip(strings.Join(sizedAt(t, 100, 40).lines(), "\n"))

	if !strings.Contains(body, "Source") || !strings.Contains(body, SourceURL) {
		t.Errorf("the body lost its Source line (%q)", SourceURL)
	}
	if SourceURL != "https://github.com/anthnel/devdesk" {
		t.Errorf("SourceURL = %q, want it unchanged", SourceURL)
	}
}

// ── Nothing else changes (non-regression, passes before and after) ───────────

// `W` is the only key that opens anything. Every other key, including the
// lowercase `w` and the keys the router owns, asks for no command and puts
// nothing in the footer.
func TestNoOtherKeyAsksForACommand(t *testing.T) {
	for _, key := range []string{
		"up", "down", "pgup", "pgdown", "home", "end",
		"?", keymap.CommandMode, ":", "q",
		"w", "o", "O", "enter", "esc", "tab", "ctrl+r", "ctrl+o",
	} {
		t.Run(key, func(t *testing.T) {
			next, cmd := pressCmd(t, sizedAt(t, 100, 5), key)

			if cmd != nil {
				t.Errorf("%q returned a command; only %q may open anything", key, keymap.Web)
			}
			if next.footer.IsSet() {
				t.Errorf("%q put %q in the footer", key, next.footer.Text())
			}
		})
	}
}

// The scrolling keys keep scrolling exactly as before.
func TestScrollingKeysAreUnchanged(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		want func(m Model) int
	}{
		{"down moves one line", []string{"down"}, func(Model) int { return 1 }},
		{"end goes to the bottom", []string{"end"}, func(m Model) int { return m.clampOffset(len(m.lines())) }},
		{"home comes back to the top", []string{"end", "home"}, func(Model) int { return 0 }},
		{"up stops at the top", []string{"up"}, func(Model) int { return 0 }},
		{"pgdown moves a page", []string{"pgdown"}, func(m Model) int { return m.clampOffset(m.height) }},
		{"pgup after pgdown comes back", []string{"pgdown", "pgup"}, func(Model) int { return 0 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := sizedAt(t, 100, 5)
			for _, key := range tt.keys {
				m = press(t, m, key)
			}
			if want := tt.want(m); m.offset != want {
				t.Errorf("offset = %d after %v, want %d", m.offset, tt.keys, want)
			}
		})
	}
}

// `W` itself does not scroll or edit anything: whatever it triggers happens
// through the command and the message that comes back (Rule 110).
func TestPressingTheOnlineHelpKeyLeavesTheViewportAlone(t *testing.T) {
	m := press(t, sizedAt(t, 100, 5), "down")
	before := m.offset

	next, _ := pressCmd(t, m, keymap.Web)

	if next.offset != before {
		t.Errorf("offset = %d after %q, want %d", next.offset, keymap.Web, before)
	}
	if next.InEditMode() {
		t.Errorf("%q made the screen take the keyboard", keymap.Web)
	}
}

// ── The opener is never the real browser ─────────────────────────────────────

// Nothing in this package may call browser.Open except through the one
// variable the tests replace. A second call site would open a real browser
// from a test, and the guard in online_help_open_test.go would not see it.
// Same idea as TestOnlyRealOSSendsASignal in internal/ports: a source scan
// that names the offender.
func TestOnlyTheOpenerSeamCallsTheRealBrowser(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	var uses []string
	seamed := 0
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}

		// `var openBrowser = browser.Open`, at package level.
		seam := map[ast.Node]bool{}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, ident := range vs.Names {
					if ident.Name == "openBrowser" && i < len(vs.Values) {
						seam[vs.Values[i]] = true
					}
				}
			}
		}

		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Open" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "browser" {
				return true
			}
			if seam[sel] {
				seamed++
				return true
			}
			uses = append(uses, fset.Position(sel.Pos()).String())
			return true
		})
	}

	for _, where := range uses {
		t.Errorf("%s: browser.Open is called outside `var openBrowser = browser.Open`; "+
			"go through openBrowser so a test can replace it", where)
	}
	if seamed != 1 {
		t.Errorf("found %d declarations `var openBrowser = browser.Open` in the package sources, want exactly 1", seamed)
	}
}
