package components

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ── The activity dot (DEVDESK-341) ───────────────────────────────────────────

// runningDot is the dot the footer must emit for a blink phase: the shared
// Running glyph, in its indicator colour, on the app background (Rule 115).
func runningDot(on bool) string {
	return indicator.Style(indicator.Running).
		Background(theme.ColorBackground).
		Render(indicator.Glyph(indicator.Running, on))
}

func hasBrailleRune(s string) bool {
	for _, r := range s {
		if r >= 0x2800 && r <= 0x28FF {
			return true
		}
	}
	return false
}

// 1, 2: the dot is steady without a phase call, and follows SetBlinkPhase.
func TestActivityDotFollowsTheBlinkPhase(t *testing.T) {
	st := Status{Text: "1 job running", Activity: true}
	cases := []struct {
		name string
		set  func(f *FooterMessage)
		want string
	}{
		{"no call is steady on", func(*FooterMessage) {}, "● 1 job running"},
		{"phase on", func(f *FooterMessage) { f.SetBlinkPhase(true) }, "● 1 job running"},
		{"phase off", func(f *FooterMessage) { f.SetBlinkPhase(false) }, "○ 1 job running"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var f FooterMessage
			c.set(&f)
			got := ansi.Strip(f.View(60, st))
			if !strings.Contains(got, c.want) {
				t.Errorf("footer line = %q, want it to contain %q", got, c.want)
			}
		})
	}
}

// 3: colour and background come from indicator.Style.
func TestActivityDotIsStyledAsTheRunningIndicator(t *testing.T) {
	for _, on := range []bool{true, false} {
		var f FooterMessage
		f.SetBlinkPhase(on)
		got := f.View(60, Status{Text: "work", Activity: true})
		if want := runningDot(on); !strings.Contains(got, want) {
			t.Errorf("on=%v: the dot is not rendered as indicator.Style(Running) on the app background\n got: %q\nwant it to contain: %q",
				on, got, want)
		}
	}
}

// 4: swapping ● for ○ moves nothing.
func TestActivityDotPhaseDoesNotShiftTheText(t *testing.T) {
	st := Status{Text: "Syncing 3/12", Activity: true}
	var on, off FooterMessage
	on.SetBlinkPhase(true)
	off.SetBlinkPhase(false)
	a, b := on.View(60, st), off.View(60, st)
	if wa, wb := lipgloss.Width(a), lipgloss.Width(b); wa != wb {
		t.Errorf("width differs between phases: %d vs %d", wa, wb)
	}
	col := func(line string) int {
		plain := ansi.Strip(line)
		i := strings.Index(plain, "Syncing")
		if i < 0 {
			return -1
		}
		return ansi.StringWidth(plain[:i])
	}
	if ia, ib := col(a), col(b); ia < 0 || ia != ib {
		t.Errorf("text column differs between phases: %d vs %d", ia, ib)
	}
}

// 5, 6: a detail with a glyph already carries the dot; one without does not.
func TestActivityDotAndRowDetail(t *testing.T) {
	var f FooterMessage
	withGlyph := ansi.Strip(f.View(80, Status{
		Activity: true, Detail: RowDetail{Glyph: "●", Lead: "Scanning ", Target: "x"},
	}))
	if n := strings.Count(withGlyph, "●"); n != 1 {
		t.Errorf("a glyph detail must leave exactly one ●, got %d in %q", n, withGlyph)
	}

	noGlyph := ansi.Strip(f.View(80, Status{
		Activity: true, Detail: RowDetail{Lead: "Scanning ", Target: "x"},
	}))
	if !strings.Contains(noGlyph, "● Scanning") {
		t.Errorf("a detail without a glyph must keep the dot, got %q", noGlyph)
	}
}

// 7: no Activity, no dot, whatever the phase.
func TestNoActivityMeansNoDot(t *testing.T) {
	for _, on := range []bool{true, false} {
		var f FooterMessage
		f.SetBlinkPhase(on)
		got := ansi.Strip(f.View(60, Status{Text: "plain status"}))
		if strings.ContainsAny(got, "●○") {
			t.Errorf("on=%v: a status without Activity shows a dot: %q", on, got)
		}
	}
}

// 8: error > warning > info > status; a timed message hides the dot.
func TestTimedMessageHidesTheActivityDot(t *testing.T) {
	for name, set := range map[string]func(*FooterMessage, string){
		"info":  func(f *FooterMessage, s string) { f.Info(s) },
		"warn":  func(f *FooterMessage, s string) { f.Warn(s) },
		"error": func(f *FooterMessage, s string) { f.Error(s) },
	} {
		t.Run(name, func(t *testing.T) {
			var f FooterMessage
			set(&f, "the message")
			got := ansi.Strip(f.View(60, Status{Text: "1 job running", Activity: true}))
			if !strings.Contains(got, "the message") {
				t.Errorf("message not shown: %q", got)
			}
			if strings.ContainsAny(got, "●○") || strings.Contains(got, "1 job running") {
				t.Errorf("status leaked through a %s message: %q", name, got)
			}
		})
	}
}

// 10: narrow widths never panic nor overflow.
func TestActivityDotAtTinyWidths(t *testing.T) {
	for _, width := range []int{1, 2, 3} {
		for _, on := range []bool{true, false} {
			var f FooterMessage
			f.SetBlinkPhase(on)
			for name, st := range map[string]Status{
				"text":   {Text: "Syncing", Activity: true},
				"detail": {Activity: true, Detail: RowDetail{Lead: "Scanning ", Target: "x"}},
			} {
				got := f.View(width, st)
				if w := lipgloss.Width(got); w != width {
					t.Errorf("%s width=%d on=%v: lipgloss.Width = %d, want %d: %q",
						name, width, on, w, width, ansi.Strip(got))
				}
			}
		}
	}
}

// 11: East Asian ambiguous width (● ○ count 2 for runewidth), both branches.
func TestActivityDotUnderEastAsianWidth(t *testing.T) {
	prev := runewidth.DefaultCondition.EastAsianWidth
	runewidth.DefaultCondition.EastAsianWidth = true
	t.Cleanup(func() { runewidth.DefaultCondition.EastAsianWidth = prev })

	const width = 60
	statuses := map[string]Status{
		"text":   {Text: "Syncing 3/12", Activity: true},
		"detail": {Activity: true, Detail: RowDetail{Lead: "Scanning ", Target: "repo", Tail: "3/12"}},
	}
	for name, st := range statuses {
		for _, on := range []bool{true, false} {
			var f FooterMessage
			f.SetBlinkPhase(on)
			got := f.View(width, st)
			if w := theme.StringWidth(ansi.Strip(got)); w != width {
				t.Errorf("%s on=%v: East Asian width = %d, want %d: %q", name, on, w, width, ansi.Strip(got))
			}
		}
	}
}

// 12 (extra): when loading ends the dot goes with the status.
func TestActivityDotDisappearsWhenTheStatusEnds(t *testing.T) {
	var f FooterMessage
	f.SetBlinkPhase(false)
	if got := ansi.Strip(f.View(60, Status{Text: "Syncing", Activity: true})); !strings.Contains(got, "○") {
		t.Fatalf("precondition: dot expected, got %q", got)
	}
	got := ansi.Strip(f.View(60, Status{}))
	if strings.ContainsAny(got, "●○") || strings.TrimSpace(got) != "" {
		t.Errorf("an empty status must render a blank line, got %q", got)
	}
}

// 3 (independent): the SGR carries the running colour as numbers read from the
// theme alias, and none of the spinner's.
func TestActivityDotSGRCarriesTheRunningColours(t *testing.T) {
	sgr := func(prefix string, c color.Color) string {
		r, g, b, _ := c.RGBA()
		return fmt.Sprintf("%s;2;%d;%d;%d", prefix, r>>8, g>>8, b>>8)
	}
	var f FooterMessage
	got := f.View(60, Status{Text: "work", Activity: true})
	i := strings.Index(got, "●")
	if i < 0 {
		t.Fatalf("no dot in %q", got)
	}
	// Whatever opens the line up to the glyph.
	head := got[:i]
	for _, want := range []string{sgr("38", theme.ColorActivityRunning), sgr("48", theme.ColorBackground)} {
		if !strings.Contains(head, want) {
			t.Errorf("dot prefix %q lacks %q", head, want)
		}
	}
}

// Empty text and zero detail: a blank line, no dot, even with Activity.
func TestActivityWithNothingToSayIsABlankLine(t *testing.T) {
	var f FooterMessage
	got := f.View(40, Status{Activity: true})
	if strings.ContainsAny(ansi.Strip(got), "●○") || strings.TrimSpace(ansi.Strip(got)) != "" {
		t.Errorf("want a blank line, got %q", ansi.Strip(got))
	}
	if w := lipgloss.Width(got); w != 40 {
		t.Errorf("width = %d, want 40", w)
	}
}

// Activity with a glyph detail: the detail's glyph only, no second dot.
func TestActivityWithAGlyphDetailShowsOneDot(t *testing.T) {
	var f FooterMessage
	got := f.View(80, Status{
		Activity: true,
		Detail:   RowDetail{Glyph: "●", Lead: "Scanning ", Target: "x"},
	})
	if n := strings.Count(ansi.Strip(got), "●"); n != 1 {
		t.Errorf("want exactly one ●, got %d in %q", n, ansi.Strip(got))
	}
	if hasBrailleRune(got) {
		t.Errorf("braille shown: %q", ansi.Strip(got))
	}
}

// A long text is truncated to the exact width with the dot, also under East
// Asian width.
func TestActivityDotTruncatesLongTextToTheExactWidth(t *testing.T) {
	prev := runewidth.DefaultCondition.EastAsianWidth
	t.Cleanup(func() { runewidth.DefaultCondition.EastAsianWidth = prev })
	long := strings.Repeat("a long status text ", 10)
	for _, ea := range []bool{false, true} {
		runewidth.DefaultCondition.EastAsianWidth = ea
		for _, on := range []bool{true, false} {
			for _, width := range []int{10, 20, 41} {
				var f FooterMessage
				f.SetBlinkPhase(on)
				got := f.View(width, Status{Text: long, Activity: true})
				plain := ansi.Strip(got)
				if w := theme.StringWidth(plain); w != width {
					t.Errorf("ea=%v on=%v width=%d: StringWidth = %d: %q", ea, on, width, w, plain)
				}
				if w := lipgloss.Width(got); ea == false && w != width {
					t.Errorf("width=%d: lipgloss.Width = %d", width, w)
				}
				if !strings.ContainsAny(plain, "●○") {
					t.Errorf("ea=%v width=%d: dot lost: %q", ea, width, plain)
				}
			}
		}
	}
}
