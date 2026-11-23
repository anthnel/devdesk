package cmdsuggest

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// The palette list shows one alias per entry (DEVDESK-747). The expectations
// below are written out by hand: none of them calls command.Entry.ListAlias.

// wantShownAlias is the alias each registry entry shows in the list.
var wantShownAlias = map[string]string{
	"about":         "version",
	"agent-usage":   "au",
	"configuration": "cfg",
	"containers":    "ct",
	"dashboard":     "dash",
	"files":         "fs",
	"git-auth":      "ga",
	"git-explorer":  "ge",
	"jobs":          "j",
	"netdiag":       "net",
	"oci-resources": "oci",
	"security":      "sec",
	"status":        "s",
	"templates":     "tpl",
	"workspaces":    "ws",
	"context":       "ctx",
	"quit":          "q",
}

// columnsOfLine cuts a plain list line (border, margin and marker included)
// into its name, alias and description columns, trimmed.
func columnsOfLine(t *testing.T, line string, width int) (name, alias, desc string) {
	t.Helper()
	l := Columns(width)
	if l.Alias == 0 {
		t.Fatalf("w=%d has no alias column", width)
	}
	cells := []rune(strings.TrimSuffix(strings.TrimPrefix(line, "│"), "│"))
	skip := margin + l.Marker
	if len(cells) < skip+l.Name+l.Alias {
		t.Fatalf("w=%d: line too short: %q", width, line)
	}
	name = strings.TrimSpace(string(cells[skip : skip+l.Name]))
	alias = strings.TrimSpace(string(cells[skip+l.Name : skip+l.Name+l.Alias]))
	desc = strings.TrimSpace(string(cells[skip+l.Name+l.Alias:]))
	return name, alias, desc
}

// listLines renders every registry entry in one list and returns the plain
// line of each, by entry name; the bottom rule is left out.
func listLines(t *testing.T, width int) map[string]string {
	t.Helper()
	in := base(width)
	in.Visible = len(in.Rows)
	lines := plain(render(t, in))
	out := map[string]string{}
	for i, r := range in.Rows {
		out[r.Entry.Name] = lines[i]
	}
	return out
}

func TestNoListLineShowsTheAliasSeparator(t *testing.T) {
	for _, w := range []int{120, 100, 80, 60, 50, 40, 34} {
		in := base(w)
		in.Visible = len(in.Rows)
		lines := plain(render(t, in))
		for i, l := range lines[:len(lines)-1] { // the last line is the rule, "· ▼ n more" is its own
			if strings.Contains(l, "·") {
				t.Errorf("w=%d line %d shows an alias separator: %q", w, i, l)
			}
		}
	}
}

// labelOf returns what the list shows in the name column of a registry entry.
func labelOf(name string) string {
	for _, e := range command.Entries() {
		if e.Name == name {
			return e.Label()
		}
	}
	return name
}

func TestEveryListLineShowsOneAlias(t *testing.T) {
	for _, w := range []int{120, 80, 50, 34} {
		lines := listLines(t, w)
		if len(lines) != len(wantShownAlias) {
			t.Fatalf("w=%d: %d lines, want %d", w, len(lines), len(wantShownAlias))
		}
		for name, want := range wantShownAlias {
			line, ok := lines[name]
			if !ok {
				t.Fatalf("w=%d: no line for %q", w, name)
			}
			gotName, gotAlias, _ := columnsOfLine(t, line, w)
			if wantName := labelOf(name); gotName != wantName {
				t.Errorf("w=%d %s: name column = %q, want %q", w, name, gotName, wantName)
			}
			if gotAlias != want {
				t.Errorf("w=%d %s: alias column = %q, want %q (line %q)", w, name, gotAlias, want, line)
			}
		}
	}
}

// An entry with a single alias renders exactly as it did when every alias was
// listed: the expected line is assembled from the entry's one alias, by hand.
func TestSingleAliasEntriesRenderByteForByte(t *testing.T) {
	for _, w := range []int{120, 80, 50} {
		layout := Columns(w)
		inner := w - 2
		side := lipgloss.NewStyle().Foreground(theme.ColorViewportBorder).Background(theme.ColorBackground).Render("│")
		for _, e := range command.Entries() {
			if len(e.Aliases) != 1 {
				continue
			}
			for _, cursor := range []int{-1, 0} {
				in := Input{Rows: []Row{{Entry: e}}, Total: 17, Cursor: cursor, Width: w, Visible: 1}
				got := render(t, in)[0]
				// The expected line is built from the entry's only alias, never from ListAlias.
				cell := cells{name: e.Label(), alias: e.Aliases[0], desc: e.Description}
				expected := side + row(cell, layout, inner, cursor == 0) + side
				if got != expected {
					t.Errorf("w=%d %s cursor=%d:\n got  %q\n want %q", w, e.Name, cursor, got, expected)
				}
			}
		}
	}
}

// The jobs line, spelled out cell by cell: the shape every single-alias line
// had before this change.
func TestJobsLineIsUnchangedLiteral(t *testing.T) {
	const w = 80
	l := Columns(w)
	e := command.Entry{Name: "jobs", Aliases: []string{"j"}, Description: "Long-running jobs started this session"}
	in := Input{Rows: []Row{{Entry: e}}, Total: 17, Cursor: -1, Width: w, Visible: 1}
	got := plain(render(t, in))[0]
	want := "│" + " " + "  " + pad("jobs", l.Name) + pad("j", l.Alias) + pad(e.Description, l.Desc)
	want += strings.Repeat(" ", w-2-(len([]rune(want))-1)) + "│"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func pad(s string, n int) string {
	return s + strings.Repeat(" ", n-len([]rune(s)))
}

// An entry with two long aliases of the same length shows the first one.
func TestWideEntryShowsOnlyItsFirstAlias(t *testing.T) {
	cjk := command.Entry{Name: "コマンド名前長い", Aliases: []string{"別名一つ", "別名二つ"}, Description: "説明"}
	in := Input{Rows: []Row{{Entry: cjk}}, Total: 1, Cursor: -1, Width: 120, Visible: 1}
	line := plain(render(t, in))[0]
	if !strings.Contains(line, "別名一つ") || strings.Contains(line, "別名二つ") || strings.Contains(line, "·") {
		t.Errorf("want only the first alias, got %q", line)
	}
}

// Typing a hidden alias still lists its entry, and the line shown has a name
// column or an alias column starting with what was typed: the reader can see
// why the line is there.
func TestALineFoundThroughAHiddenAliasExplainsItself(t *testing.T) {
	const w = 80
	s := command.NewSuggester()
	for _, tc := range []struct{ typed, entry string }{
		{"config", "configuration"}, {"conf", "configuration"},
		{"cont", "containers"}, {"con", "containers"},
		{"d", "dashboard"}, {"dash", "dashboard"},
		{"w", "workspaces"}, {"wo", "workspaces"},
		{"c", "context"},
		{"cfg", "configuration"}, {"ct", "containers"}, {"ws", "workspaces"}, {"ctx", "context"},
	} {
		got := s.Suggest(tc.typed)
		var rows []Row
		for _, e := range got.Entries {
			rows = append(rows, Row{Entry: e})
		}
		in := Input{Rows: rows, Total: got.Total, Query: got.Query, Cursor: -1, Width: w, Visible: len(rows)}
		lines := plain(render(t, in))
		found := false
		for i, r := range rows {
			if r.Entry.Name != tc.entry {
				continue
			}
			found = true
			name, alias, _ := columnsOfLine(t, lines[i], w)
			if !strings.HasPrefix(name, tc.typed) && !strings.HasPrefix(alias, tc.typed) {
				t.Errorf("Suggest(%q): the %s line shows name %q alias %q, neither starts with the input", tc.typed, tc.entry, name, alias)
			}
		}
		if !found {
			t.Errorf("Suggest(%q) no longer lists %q", tc.typed, tc.entry)
		}
	}
}

// (current) keeps replacing the alias column when the description is hidden,
// and leads the description otherwise, for an entry that used to list two aliases.
func TestCurrentMarkerOnANarrowedEntryIsUnchanged(t *testing.T) {
	mk := func(w int) string {
		in := base(w)
		in.Visible = len(in.Rows)
		idx := -1
		for i := range in.Rows {
			if in.Rows[i].Entry.Name == "configuration" {
				idx = i
			}
		}
		if idx < 0 {
			t.Fatal("registry has no configuration entry")
		}
		in.Rows[idx].Current = true
		return plain(render(t, in))[idx]
	}
	if l := mk(80); !strings.Contains(l, "(current) Settings") || !strings.Contains(l, "cfg") {
		t.Errorf("w=80: want cfg and (current) leading the description: %q", l)
	}
	if _, alias, desc := columnsOfLine(t, mk(50), 50); alias != "(current)" || desc != "" {
		t.Errorf("w=50: (current) should replace the alias column, got alias %q desc %q", alias, desc)
	}
	if l := mk(30); strings.Contains(l, "(current)") || strings.Contains(l, "cfg") {
		t.Errorf("w=30: no alias column and no (current): %q", l)
	}
}

// The alias column is still dropped below 34 cells: no alias shows there.
func TestAliasColumnStillDisappearsBelowThirtyFourCells(t *testing.T) {
	for name, line := range listLines(t, 33) {
		for _, alias := range wantShownAlias {
			if len(alias) < 2 || alias == name {
				continue
			}
			if strings.Contains(strings.TrimPrefix(line, "│"), " "+alias+" ") {
				t.Errorf("w=33 %s: alias %q shown: %q", name, alias, line)
			}
		}
	}
}
