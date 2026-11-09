package indicator

import (
	"strings"
	"testing"
)

const (
	legendTitle  = "Activity indicator"
	motionPhrase = "Turn the blinking off with app.motion: static in the context file or in :cfg."
)

// rowLines returns the body lines with leading blanks removed.
func rowLines(body string) []string {
	var out []string
	for _, l := range strings.Split(body, "\n") {
		out = append(out, strings.TrimLeft(l, " "))
	}
	return out
}

// hasRow reports whether a line starts with glyph and contains label.
func hasRow(body, glyph, label string) bool {
	for _, l := range rowLines(body) {
		if strings.HasPrefix(l, glyph) && strings.Contains(l, label) {
			return true
		}
	}
	return false
}

func TestLegendBaseSection(t *testing.T) {
	s := Legend()
	if s.Title != legendTitle {
		t.Fatalf("Title = %q, want %q", s.Title, legendTitle)
	}
	on, off := Glyph(Running, true), Glyph(Running, false)
	rows := []struct{ glyph, label string }{
		{on + " " + off, "Running (blinks). Pauses to a steady " + on + " when motion is off."},
		{Glyph(Queued, false), "Queued, waiting to start."},
		{Glyph(Failed, false), "Failed. Stays until the next attempt on that row; the footer gives the cause."},
		{"(icon)", "Idle: the row's usual icon, or an empty cell."},
	}
	for _, r := range rows {
		if !hasRow(s.Body, r.glyph, r.label) {
			t.Errorf("no row starting with %q containing %q in:\n%s", r.glyph, r.label, s.Body)
		}
	}
	for _, p := range []string{
		"The first column shows what is happening on a row. The glyph carries the",
		"meaning; the color repeats it. Nothing here relies on color alone.",
		"The footer describes the selected row's state in words.",
		motionPhrase,
	} {
		if !strings.Contains(s.Body, p) {
			t.Errorf("body lacks validated phrase %q", p)
		}
	}
}

func TestLegendExtensions(t *testing.T) {
	cases := []struct {
		name string
		ext  []LegendEntry
		want []LegendEntry
	}{
		{"jobs", JobsLegend, []LegendEntry{{Done, "done"}, {Cancelled, "cancelled"}, {Skipped, "skipped"}}},
		{"agents", AgentsLegend, []LegendEntry{{Suspended, "idle (no activity for 5 min)"}, {Done, "done"}, {Unknown, "unknown"}}},
		{"containers", ContainersLegend, []LegendEntry{
			{Restarting, "restarting"}, {Suspended, "paused"}, {Unknown, "created"},
			{Skipped, "exited (code 0)"}, {Failed, "exited (non-zero code) or dead"},
			{Running, "an action (stop, kill, remove) is running"},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if len(c.ext) != len(c.want) {
				t.Fatalf("extension has %d entries, want %d", len(c.ext), len(c.want))
			}
			for i, e := range c.want {
				if c.ext[i] != e {
					t.Errorf("entry %d = %+v, want %+v", i, c.ext[i], e)
				}
			}
			body := Legend(c.ext...).Body
			for _, e := range c.want {
				if !hasRow(body, Glyph(e.State, true), e.Label) {
					t.Errorf("no row %q %q in:\n%s", Glyph(e.State, true), e.Label, body)
				}
			}
		})
	}
}

func TestContainersLegendHasNoDone(t *testing.T) {
	for _, e := range ContainersLegend {
		if e.State == Done {
			t.Errorf("ContainersLegend has a Done entry %+v: a running container shows its usual icon", e)
		}
	}
	if len(ContainersLegend) == 0 {
		t.Fatal("ContainersLegend is empty")
	}
	body := Legend(ContainersLegend...).Body
	if strings.Contains(body, Glyph(Done, false)) {
		t.Errorf("containers legend shows %q:\n%s", Glyph(Done, false), body)
	}
}

func TestLegendIsDerivedFromTheGlyphTable(t *testing.T) {
	known := map[rune]bool{}
	for _, s := range States() {
		for _, on := range []bool{true, false} {
			for _, r := range Glyph(s, on) {
				known[r] = true
			}
		}
	}
	for name, ext := range map[string][]LegendEntry{
		"base": nil, "jobs": JobsLegend, "agents": AgentsLegend, "containers": ContainersLegend,
	} {
		for _, e := range ext {
			if e.State == Idle {
				t.Errorf("%s: entry %+v uses Idle", name, e)
			}
		}
		for _, r := range Legend(ext...).Body {
			inRange := (r >= 0x2295 && r <= 0x2299) || (r >= 0x25A0 && r <= 0x25FF)
			if inRange && !known[r] {
				t.Errorf("%s: body holds %q which is not the glyph of any state", name, r)
			}
		}
	}
}

func TestLegendExtendsNeverReplaces(t *testing.T) {
	base := Legend()
	if base.Body == "" {
		t.Fatal("base body is empty")
	}
	for name, ext := range map[string][]LegendEntry{
		"jobs": JobsLegend, "agents": AgentsLegend, "containers": ContainersLegend,
	} {
		got := Legend(ext...)
		if got.Title != base.Title {
			t.Errorf("%s: Title = %q, want %q", name, got.Title, base.Title)
		}
		for _, l := range rowLines(base.Body) {
			if l != "" && !strings.Contains(got.Body, l) {
				t.Errorf("%s: base line %q is gone", name, l)
			}
		}
	}
	if again := Legend(); again != base {
		t.Error("Legend() changed after being called with extensions")
	}
}

// DEVDESK-148: the clone list adds Done and Skipped to the base legend.
func TestCloneLegend(t *testing.T) {
	if len(CloneLegend) != 2 {
		t.Fatalf("CloneLegend has %d entries, want 2 (Done, Skipped): %+v", len(CloneLegend), CloneLegend)
	}
	want := []struct {
		state State
		label string
	}{
		{Done, "cloned"},
		{Skipped, "already on disk, or not started (cancelled)"},
	}
	body := Legend(CloneLegend...).Body
	for i, w := range want {
		e := CloneLegend[i]
		if e.State != w.state || !strings.HasPrefix(e.Label, w.label) {
			t.Errorf("entry %d = %+v, want state %v and a label starting %q", i, e, w.state, w.label)
		}
		if !hasRow(body, Glyph(w.state, true), w.label) {
			t.Errorf("no row %q %q in:\n%s", Glyph(w.state, true), w.label, body)
		}
	}
	for _, g := range []string{Glyph(Done, true), Glyph(Skipped, true)} {
		if n := strings.Count(body, g); n != 1 {
			t.Errorf("glyph %q appears %d times in the clone legend, want 1:\n%s", g, n, body)
		}
	}
	if strings.Contains(body, Glyph(Cancelled, false)) {
		t.Errorf("clone legend shows the cancelled glyph %q: a cancelled clone row is skipped", Glyph(Cancelled, false))
	}
}
