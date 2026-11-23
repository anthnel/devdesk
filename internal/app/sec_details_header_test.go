package app

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/security"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
)

// DEVDESK-751 (steps 4 and 6): the :sec detail advertises six keys, and the
// header holds them at the three reference widths. At 80 columns the shortcut
// column is 19 cells wide and `<ctrl+o>` takes nine of them, so a description is
// at most ten cells (plan, "Header measured at 80 columns"). The detail also
// hands a finding's file to the viewer and comes back to the same detail.

// secDetailKeys are the six keys of the detail, in the order of the column.
var secDetailKeys = []string{"esc", "W", "enter", "ctrl+o", "Y", "X"}

var secDetailWords = map[string]string{
	"esc": "Back", "W": "Open ref", "enter": "Open file", "ctrl+o": "Apply fix", "Y": "Copy cmd", "X": "Exclude",
}

// secDetailResult is one vulnerability whose file is a real document, so the
// viewer has something to open.
func secDetailResult(dir string) *scan.Result {
	return &scan.Result{
		Target: dir, TargetType: scan.TargetDirectory,
		Findings: []scan.Finding{{
			ID: "CVE-2026-1111", Title: "A flaw in a dependency", Severity: scan.SeverityHigh, Source: scan.SourceTrivy,
			PkgName: "golang.org/x/net", Version: "0.1.0", FixedIn: "0.7.0", File: "config.json",
			FixCommand: "go get golang.org/x/net@v0.7.0", References: []string{"https://nvd.example/CVE-2026-1111"},
		}},
	}
}

func secDetailModel(t *testing.T, dir string) security.Model {
	t.Helper()
	view := security.NewWithPreloadedResult(testConfig(), nil, secDetailResult(dir))
	updated, _ := view.Update(testutil.Resize(160, 30))
	updated, _ = updated.Update(testutil.Key("enter"))
	m, ok := updated.(security.Model)
	if !ok {
		t.Fatalf("Update returned a %T", updated)
	}
	if keys := testutil.ShortcutKeys(m.GetShortcuts()); keys[0] != "esc" {
		t.Fatalf("enter did not open the detail: the column reads %v", keys)
	}
	return m
}

// secDetailShortcuts is the detail's column, required to hold the six keys: a
// header test over a shorter list would pass on whatever it was given.
func secDetailShortcuts(t *testing.T) shortcut.Shortcuts {
	t.Helper()
	list := secDetailModel(t, t.TempDir()).GetShortcuts()
	if got := testutil.ShortcutKeys(list); !slices.Equal(got, secDetailKeys) {
		t.Fatalf("keys = %v, want %v", got, secDetailKeys)
	}
	return list
}

func TestTheDetailAdvertisesItsSixKeys(t *testing.T) {
	list := secDetailShortcuts(t)
	for _, s := range list {
		if s.Description != secDetailWords[s.Key] {
			t.Errorf("%s reads %q, want %q", s.Key, s.Description, secDetailWords[s.Key])
		}
	}
}

// Every `<key> Description` is on screen whole at 80, 120 and 175 columns: none
// is cut, none pushes the row past the window, and the palette hint keeps the
// last row.
func TestTheDetailHeaderHoldsItsSixKeysAtEveryWidth(t *testing.T) {
	list := secDetailShortcuts(t)
	info := []shortcut.HeaderInfo{{Key: "Context", Value: "default"}}

	for _, width := range []int{80, 120, 175} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			lines := strings.Split(renderHeaderContent(info, list, width), "\n")
			if len(lines) != headerMinHeight {
				t.Fatalf("the header is %d rows, want %d", len(lines), headerMinHeight)
			}
			for i, line := range lines {
				if got := lipgloss.Width(line); got != width {
					t.Errorf("row %d is %d cells wide, want %d", i, got, width)
				}
			}
			plain := ansi.Strip(strings.Join(lines, "\n"))
			for _, s := range list {
				pattern := regexp.MustCompile(`<` + regexp.QuoteMeta(s.Key) + `>\s+` + regexp.QuoteMeta(s.Description) + `(\s|$)`)
				if !pattern.MatchString(plain) {
					t.Errorf("width %d: %q is not on screen whole:\n%s", width, "<"+s.Key+"> "+s.Description, plain)
				}
			}
			last := strings.TrimRight(ansi.Strip(lines[headerMinHeight-1]), " ")
			if !strings.HasSuffix(last, paletteHint) {
				t.Errorf("width %d: the last row %q lost the palette hint", width, last)
			}
		})
	}
}

// At 80 columns a description over ten cells is cut by the column: the words are
// chosen for it.
func TestTheDetailDescriptionsFitTheNarrowestColumn(t *testing.T) {
	for _, s := range secDetailShortcuts(t) {
		if w := ansi.StringWidth(s.Description); w > 10 {
			t.Errorf("%s: %q is %d cells wide, want at most 10", s.Key, s.Description, w)
		}
	}
}

// ── enter hands the file to the viewer, and esc comes back ──────────────────

func secView(t *testing.T, a *App) security.Model {
	t.Helper()
	m, ok := a.views[command.ViewSecurity].(security.Model)
	if !ok {
		t.Fatalf("the router holds a %T for the security view", a.views[command.ViewSecurity])
	}
	return m
}

func TestEnterInTheDetailOpensTheFileAndEscComesBackToTheDetail(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	isolateHome(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"name":"web"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	a := newWithSize(testConfig(), 120, 40)
	a.setView(command.ViewSecurity, security.NewWithPreloadedResult(testConfig(), nil, secDetailResult(dir)))
	a.currentView = command.ViewSecurity
	a.Update(testutil.Resize(120, 40))
	feedKey(t, a, testutil.Key("enter"))
	if got := testutil.ShortcutKeys(secView(t, a).GetShortcuts()); !slices.Equal(got, secDetailKeys) {
		t.Fatalf("the first enter did not open the detail: %v", got)
	}

	// The second enter, now in the detail, asks for the file.
	cmd := feedKey(t, a, testutil.Key("enter"))
	var request uiviewer.OpenRequestMsg
	var found bool
	for _, msg := range testutil.Msgs(cmd) {
		if req, ok := msg.(uiviewer.OpenRequestMsg); ok {
			request, found = req, true
		}
	}
	if !found {
		t.Fatal("enter in the detail asked the router for no document")
	}
	a.Update(request)
	if a.currentView != command.ViewViewer {
		t.Fatalf("currentView = %q, want the viewer", a.currentView)
	}
	viewerView, ok := a.views[command.ViewViewer].(uiviewer.Model)
	if !ok || viewerView.OriginView != command.ViewSecurity {
		t.Fatalf("the viewer's origin = %v, want the security view", viewerView.OriginView)
	}

	// esc in the viewer goes back to :sec, which is still on the detail.
	a.Update(uiviewer.BackToOriginMsg{Origin: command.ViewSecurity})
	if a.currentView != command.ViewSecurity {
		t.Fatalf("currentView = %q, want the security view", a.currentView)
	}
	if got := testutil.ShortcutKeys(secView(t, a).GetShortcuts()); !slices.Equal(got, secDetailKeys) {
		t.Errorf("back from the viewer the column reads %v, want the detail's %v", got, secDetailKeys)
	}
}
