package app

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/security"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-754 (SC1, AC6): the header and the frame at 80, 120 and 175 columns
// with the CVE tab grouped. User decisions pinned here: `g` is the third entry of
// the results column, so it is among the seven that show at 80 columns, and `h`
// moves out of them; the mode is named in the info column. The tab bar is not
// measured: it already passes 80 columns with two-digit counts, a separate bug.

// secGroupCVE is one vulnerability of a directory scan.
func secGroupCVE(id, pkg, installed, fixed string, sev scan.SeverityLevel) scan.Finding {
	return scan.Finding{
		ID: id, Title: id + " title", Source: scan.SourceTrivy, Severity: sev,
		PkgName: pkg, Ecosystem: "alpine", Class: scan.ClassOSPackages, Version: installed, FixedIn: fixed,
	}
}

func secGroupResult(dir string) *scan.Result {
	return &scan.Result{
		Target: dir, TargetType: scan.TargetDirectory,
		Findings: []scan.Finding{
			secGroupCVE("CVE-1", "openssl", "1.1.1k", "1.1.1w", scan.SeverityCritical),
			secGroupCVE("CVE-2", "openssl", "1.1.1k", "1.1.1w", scan.SeverityHigh),
			secGroupCVE("CVE-3", "zlib", "1.2", "", scan.SeverityMedium),
		},
	}
}

// secGroupView is the :sec results at width columns, flat or grouped.
func secGroupView(t *testing.T, width int, grouped bool) security.Model {
	t.Helper()
	// A short target: the frame's title line carries it, and t.TempDir() embeds the
	// test's name, which alone makes the flat title wider than 80 columns.
	view := security.NewWithPreloadedResult(testConfig(), nil, secGroupResult("/srv/app"))
	updated, _ := view.Update(testutil.Resize(width, 30))
	if grouped {
		updated, _ = updated.Update(testutil.Key("g"))
	}
	m, ok := updated.(security.Model)
	if !ok {
		t.Fatalf("Update returned a %T", updated)
	}
	if on := strings.Contains(strings.Join(shortcutWords(m.GetShortcuts()), " "), "List by finding"); on != grouped {
		t.Fatalf("grouped = %v, but the column reads %v", grouped, shortcutWords(m.GetShortcuts()))
	}
	return m
}

func shortcutWords(list shortcut.Shortcuts) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		out = append(out, s.Description)
	}
	return out
}

func secGroupHeader(t *testing.T, width int, grouped bool) (rows []string, plain string) {
	t.Helper()
	m := secGroupView(t, width, grouped)
	rows = strings.Split(renderHeaderContent(m.GetHeaderInfo("default"), m.GetShortcuts(), width), "\n")
	return rows, ansi.Strip(strings.Join(rows, "\n"))
}

func TestGroupKeyIsInTheHeaderAtEveryWidth(t *testing.T) {
	for _, width := range []int{80, 120, 175} {
		for _, grouped := range []bool{false, true} {
			t.Run(fmt.Sprintf("width %d grouped %v", width, grouped), func(t *testing.T) {
				rows, plain := secGroupHeader(t, width, grouped)
				if len(rows) != headerMinHeight {
					t.Fatalf("the header is %d rows, want %d", len(rows), headerMinHeight)
				}
				for i, row := range rows {
					if got := lipgloss.Width(row); got != width {
						t.Errorf("row %d is %d cells wide, want %d", i, got, width)
					}
				}
				word, whole := "Group", "Group by fix"
				if grouped {
					word, whole = "List", "List by finding"
				}
				// At 80 the column is 19 cells and the description is cut by it; the
				// key and the start of its word are what must be there.
				pattern := `<g>\s+` + word
				if width >= 120 {
					pattern = `<g>\s+` + regexp.QuoteMeta(whole) + `(\s|$)`
				}
				if !regexp.MustCompile(pattern).MatchString(plain) {
					t.Errorf("width %d: no %q in the header:\n%s", width, pattern, plain)
				}
				last := strings.TrimRight(ansi.Strip(rows[headerMinHeight-1]), " ")
				if !strings.HasSuffix(last, paletteHint) {
					t.Errorf("width %d: the last row %q lost the palette hint", width, last)
				}
			})
		}
	}
}

// At 80 columns only the first seven entries show: g takes the third place, h
// leaves them, and the keys that were there stay but h.
func TestAt80ColumnsGIsVisibleAndHHasMovedToTheHiddenColumn(t *testing.T) {
	_, plain := secGroupHeader(t, 80, false)
	for _, key := range []string{"tab", "enter", "g", "space", "ctrl+o", "S", "c"} {
		if !strings.Contains(plain, "<"+key+">") {
			t.Errorf("<%s> is not on screen at 80 columns:\n%s", key, plain)
		}
	}
	if strings.Contains(plain, "<h>") {
		t.Errorf("<h> is still among the seven entries at 80 columns:\n%s", plain)
	}
	// At 175 every entry fits.
	_, wide := secGroupHeader(t, 175, false)
	for _, key := range []string{"h", "m", "l", "/", ".", "Y", "ctrl+r", "?"} {
		if !strings.Contains(wide, "<"+key+">") {
			t.Errorf("<%s> is not on screen at 175 columns:\n%s", key, wide)
		}
	}
}

// The info column names the mode, whole where the column has room for it, and
// never when the list is flat.
func TestTheInfoColumnNamesTheGroupedMode(t *testing.T) {
	for _, width := range []int{120, 175} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			info := func(grouped bool) string {
				rows, _ := secGroupHeader(t, width, grouped)
				var col []string
				for _, row := range rows {
					col = append(col, ansi.Strip(ansi.Cut(row, 0, 1+shortcut.HeaderInfoWidth(width))))
				}
				return strings.Join(col, "\n")
			}
			if got := info(true); !strings.Contains(got, "Group by fix") {
				t.Errorf("the grouped info column does not name the mode:\n%s", got)
			}
			if got := info(false); strings.Contains(got, "Group by fix") {
				t.Errorf("the flat info column names the grouped mode:\n%s", got)
			}
		})
	}
}

// The whole frame the router draws, grouped, is never wider than the window: the
// header, the table and the info line.
func TestTheGroupedFrameFitsTheWindowAtEveryWidth(t *testing.T) {
	for _, width := range []int{80, 120, 175} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			isolateHome(t)
			a := newWithSize(testConfig(), width, 40)
			a.setView(command.ViewSecurity, secGroupView(t, width, true))
			a.currentView = command.ViewSecurity
			a.Update(testutil.Resize(width, 40))

			frame := a.Render()
			for i, line := range strings.Split(frame, "\n") {
				if w := lipgloss.Width(line); w > width {
					t.Errorf("line %d is %d cells wide, the window holds %d: %q", i, w, width, ansi.Strip(line))
				}
			}
			plain := ansi.Strip(frame)
			for _, want := range []string{"Package", "Change", "CVEs", "openssl", "1.1.1k -> 1.1.1w", "No fix published"} {
				if !strings.Contains(plain, want) {
					t.Errorf("width %d: the frame does not show %q:\n%s", width, want, plain)
				}
			}
			// The selected group's sentence is in the footer (80 columns hold it whole).
			if !strings.Contains(plain, "2 CVEs closed by one move to 1.1.1w") {
				t.Errorf("width %d: the footer does not say what the selected group does:\n%s", width, plain)
			}
		})
	}
}
