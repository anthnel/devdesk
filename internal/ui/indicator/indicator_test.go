package indicator

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

func TestEveryStateHasItsOwnGlyph(t *testing.T) {
	states := States()
	if len(states) != 9 {
		t.Fatalf("States() has %d entries, want 9 (every state but Idle)", len(states))
	}
	seen := map[string]string{}
	add := func(name, g string) {
		if g == "" {
			t.Errorf("%s has an empty glyph", name)
			return
		}
		if prev, dup := seen[g]; dup {
			t.Errorf("glyph %q is shared by %s and %s", g, prev, name)
		}
		seen[g] = name
	}
	for _, s := range states {
		add(s.String(), Glyph(s, true))
	}
	add("running(off)", Glyph(Running, false))
	if len(seen) != 10 {
		t.Errorf("got %d distinct glyphs, want 10", len(seen))
	}
	for _, g := range []string{"●", "○"} {
		if seen[g] != "running" && seen[g] != "running(off)" {
			t.Errorf("%q belongs to %q, want Running only", g, seen[g])
		}
	}
	if seen["◉"] != "done" {
		t.Errorf("◉ belongs to %q, want done only", seen["◉"])
	}
	if Glyph(Failed, true) != "⊗" {
		t.Errorf("Failed glyph = %q, want ⊗", Glyph(Failed, true))
	}
	if Glyph(Failed, true) == Glyph(Suspended, true) || Glyph(Failed, true) == Glyph(Restarting, true) {
		t.Error("Failed shares a glyph with Suspended or Restarting")
	}
	if Glyph(Suspended, true) != "◐" || Glyph(Restarting, true) != "◕" {
		t.Errorf("Suspended/Restarting = %q/%q, want ◐/◕", Glyph(Suspended, true), Glyph(Restarting, true))
	}
}

func TestAGlyphIsOneRuneOfPlainText(t *testing.T) {
	for _, s := range States() {
		for _, on := range []bool{true, false} {
			g := Glyph(s, on)
			if n := utf8.RuneCountInString(g); n != 1 {
				t.Errorf("%s (on=%v): glyph %q has %d runes, want 1", s, on, g, n)
			}
			if strings.ContainsAny(g, " \t\x1b") {
				t.Errorf("%s (on=%v): glyph %q contains a space or escape", s, on, g)
			}
			if w := runewidth.StringWidth(g); w > 2 {
				t.Errorf("%s (on=%v): glyph %q is %d cells wide, want <= 2", s, on, g, w)
			}
		}
	}
}

func TestOnlyRunningBlinks(t *testing.T) {
	for _, s := range States() {
		if got, want := Animated(s), s == Running; got != want {
			t.Errorf("Animated(%s) = %v, want %v", s, got, want)
		}
		if s != Running && Glyph(s, true) != Glyph(s, false) {
			t.Errorf("%s glyph changes with the phase: %q vs %q", s, Glyph(s, true), Glyph(s, false))
		}
	}
	if Glyph(Running, true) == Glyph(Running, false) {
		t.Error("Running glyph does not change with the phase")
	}
	if Animated(Idle) {
		t.Error("Idle must not be animated")
	}
	for _, s := range []State{Queued, Restarting} {
		if Animated(s) {
			t.Errorf("%s must be fixed", s)
		}
	}
}

func TestIdleHasNoGlyph(t *testing.T) {
	if Idle != State(0) {
		t.Error("Idle must be the zero value")
	}
	for _, on := range []bool{true, false} {
		if g := Glyph(Idle, on); g != "" {
			t.Errorf("Glyph(Idle, %v) = %q, want empty", on, g)
		}
	}
	if Glyph(Unknown, true) != "◌" {
		t.Errorf("Unknown glyph = %q, want ◌ (not Idle's)", Glyph(Unknown, true))
	}
}

func TestResolveOrder(t *testing.T) {
	observed := []State{Idle, Suspended, Failed, Done}
	for _, obs := range observed {
		for bits := 0; bits < 8; bits++ {
			a := Activity{Running: bits&1 != 0, Queued: bits&2 != 0, Failed: bits&4 != 0, Observed: obs}
			want := obs
			switch {
			case a.Running:
				want = Running
			case a.Queued:
				want = Queued
			case a.Failed:
				want = Failed
			}
			if got := Resolve(a); got != want {
				t.Errorf("Resolve(%+v) = %s, want %s", a, got, want)
			}
		}
	}
	explicit := []struct {
		name string
		in   Activity
		want State
	}{
		{"all set", Activity{Running: true, Queued: true, Failed: true, Observed: Suspended}, Running},
		{"queued over failed", Activity{Queued: true, Failed: true}, Queued},
		{"failed over observed", Activity{Failed: true, Observed: Done}, Failed},
		{"observed alone", Activity{Observed: Restarting}, Restarting},
		{"nothing", Activity{}, Idle},
	}
	for _, c := range explicit {
		if got := Resolve(c.in); got != c.want {
			t.Errorf("%s: Resolve = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestTwoSourcesOneState(t *testing.T) {
	if a, b := Resolve(Activity{Failed: true}), Resolve(Activity{Observed: Failed}); a != b || a != Failed {
		t.Errorf("remembered failure = %s, observed failure = %s, want both Failed", a, b)
	}
}
