package app

import (
	"math/rand"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// styleBody against the lipgloss call it replaces (DEVDESK-281, task 281.4).
//
// padBody is a fast path: wherever it answers, the string must be the one
// lipgloss.Style.Width(w).Render gives, byte for byte, and wherever it refuses,
// styleBody must still return that string by calling lipgloss.

// slowBody is the call styleBody replaces, as renderBody made it.
func slowBody(content string, width int) string {
	return lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Foreground(theme.ColorText).
		Width(width).
		Render(content)
}

func bodyStyleForTest() lipgloss.Style {
	return lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText)
}

const (
	sgrRed   = "\x1b[38;2;255;0;0m"
	sgrBold  = "\x1b[1m"
	sgrBgRGB = "\x1b[48;2;1;2;3m"
)

func TestPadBodyIsWhatLipglossRenders(t *testing.T) {
	useDefaultTheme(t)
	cases := map[string]string{
		"empty":            "",
		"one plain line":   "plain",
		"trailing newline": "a\nb\n",
		"blank lines":      "\n\n\n",
		"styled":           theme.BgLine("styled", 10) + "\n" + theme.KeyStyle.Render("key") + "\nplain",
		"reset m":          "plain, then " + sgrBold + "bold to the end" + "\x1b[m",
		"reset 0m":         sgrRed + "red to the end" + "\x1b[0m",
		"own background":   sgrBgRGB + "own background to the end\x1b[m",
		"text after reset": "x" + sgrBold + "bold\x1b[m and the rest after a reset",
		"spaces after":     sgrRed + "red\x1b[0m   spaces after",
		"wide":             "世界\n❤️ ✔️\nflag 🇫🇷 family 👨‍👩‍👧\ncombining é",
		"nerd font":        "\ue5ff folder\n\uf058 ok",
		"full width":       strings.Repeat("a", 40) + "\n" + strings.Repeat("世", 20),
		"width minus one":  strings.Repeat("a", 39),
		"cursor movement":  "a\x1b[5Cb\x1b[2Kc",
		"two styles":       sgrBold + "a\x1b[m" + sgrRed + "b\x1b[m",
		"many lines":       strings.Repeat("row "+sgrRed+"x\x1b[m\n", 30),
	}
	for name, content := range cases {
		for _, width := range []int{40, 41, 118} {
			if got, ok := padBody(content, width, bodyStyleForTest()); !ok {
				t.Errorf("%s at %d: padBody refused a line lipgloss leaves as it is", name, width)
			} else if want := slowBody(content, width); got != want {
				t.Errorf("%s at %d:\nwant %q\ngot  %q", name, width, want, got)
			}
			if got, want := styleBody(content, width), slowBody(content, width); got != want {
				t.Errorf("%s at %d: styleBody differs from lipgloss", name, width)
			}
		}
	}
}

// What padBody refuses goes to lipgloss, whose answer styleBody returns.
func TestPadBodyRefusesWhatLipglossMayChange(t *testing.T) {
	useDefaultTheme(t)
	cases := map[string]string{
		"a line wider than the width": strings.Repeat("0123456789", 3),
		"a wide glyph over the edge":  strings.Repeat("a", 19) + "世",
		"a tab":                       "a\tb",
		"a carriage return":           "ab\r\ncd",
		"a bare carriage return":      "ab\rcd",
		"a link":                      "\x1b]8;;https://example.com\x07text\x1b]8;;\x07",
		"an open style at the end":    "plain " + sgrRed + "red",
		"an open style, then text":    sgrBold + "bold\nnext line",
		"a bold reset then bold":      "\x1b[m" + sgrBold + "x",
		"a non CSI escape":            "a\x1b7b",
		"a truncated CSI":             "a\x1b[",
		"a truncated CSI with params": "a\x1b[38;2",
		"a control character":         "a\x07b",
		"DEL":                         "a\x7fb",
		"a form feed":                 "a\fb",
		"a combined reset and style":  "\x1b[0;1mx",
	}
	for name, content := range cases {
		if _, ok := padBody(content, 20, bodyStyleForTest()); ok {
			t.Errorf("%s: padBody answered", name)
		}
		if got, want := styleBody(content, 20), slowBody(content, 20); got != want {
			t.Errorf("%s: styleBody differs from lipgloss:\nwant %q\ngot  %q", name, want, got)
		}
	}
	for _, width := range []int{0, -1, -118} {
		if _, ok := padBody("a", width, bodyStyleForTest()); ok {
			t.Errorf("padBody answered at width %d", width)
		}
		if got, want := styleBody("a", width), slowBody("a", width); got != want {
			t.Errorf("width %d: styleBody differs from lipgloss", width)
		}
	}
}

// A seeded fuzz of lines made of fragments the views really write, and a few
// nasty ones: whenever padBody answers it equals lipgloss, and the width it
// measured is x/ansi's.
func TestPadBodyMatchesLipglossOnGeneratedContent(t *testing.T) {
	useDefaultTheme(t)
	fragments := []string{
		"a", "word", " ", "  ", "-", "世", "é", "❤️", "🇫🇷", "\u200d", "\ue5ff", "\u009b", "\u00a0", "│",
		sgrRed, sgrBold, sgrBgRGB, "\x1b[m", "\x1b[0m", "\x1b[22m", "\x1b[39m", "\x1b[0;1m", "\x1b[5C", "\x1b[2K",
		"\t", "\r", "\x1b7", "\x1b]8;;u\x07", "\x07",
	}
	rng := rand.New(rand.NewSource(281))
	answered, refused := 0, 0
	for n := 0; n < 4000; n++ {
		var lines []string
		for range 1 + rng.Intn(5) {
			var l strings.Builder
			for range rng.Intn(12) {
				l.WriteString(fragments[rng.Intn(len(fragments))])
			}
			lines = append(lines, l.String())
		}
		content := strings.Join(lines, "\n")
		width := 1 + rng.Intn(24)

		if got, ok := padBody(content, width, bodyStyleForTest()); ok {
			answered++
			if want := slowBody(content, width); got != want {
				t.Fatalf("width %d, content %q:\nwant %q\ngot  %q", width, content, want, got)
			}
		} else {
			refused++
		}
		if got, want := styleBody(content, width), slowBody(content, width); got != want {
			t.Fatalf("styleBody, width %d, content %q:\nwant %q\ngot  %q", width, content, want, got)
		}
		for _, l := range lines {
			if w, ok := scanBodyLine(l); ok && w != ansi.StringWidth(l) {
				t.Fatalf("scanBodyLine(%q) = %d, x/ansi says %d", l, w, ansi.StringWidth(l))
			}
		}
	}
	if answered < 200 || refused < 200 {
		t.Errorf("the generator is lopsided: %d answered, %d refused", answered, refused)
	}
}

// The bodies of the cell goldens, which are what the router really draws, take
// the fast path (except the overflow one, which is wrapped), so the goldens
// guard it and not only the fallback.
func TestPadBodyIsTakenOnTheGoldenBodies(t *testing.T) {
	useDefaultTheme(t)
	bodies := map[string]string{
		"framed":          cellsBody(),
		"reset":           resetBody(),
		"reset-then-text": resetThenTextBody(),
	}
	for name, content := range bodies {
		for _, width := range []int{framesWidth - 2, framesWidth} {
			got, ok := padBody(content, width, bodyStyleForTest())
			if !ok {
				t.Errorf("%s at %d: padBody refused", name, width)
				continue
			}
			if want := slowBody(content, width); got != want {
				t.Errorf("%s at %d: differs from lipgloss", name, width)
			}
		}
	}
	if _, ok := padBody(overflowBody(), framesWidth-2, bodyStyleForTest()); ok {
		t.Error("the overflow body is wider than the viewport: padBody must refuse it")
	}
}

// The cost does not grow with the number of lines: the body is one builder, and
// the only other allocations are the two small renders that read the style's
// escape sequences.
func TestPadBodyAllocationsDoNotGrowWithTheLines(t *testing.T) {
	useDefaultTheme(t)
	content := strings.Repeat(theme.BgLine("row", 100)+"\n", 99) + theme.BgLine("last", 100)
	style := bodyStyleForTest()
	var sink string
	n := testing.AllocsPerRun(20, func() { sink, _ = padBody(content, 118, style) })
	if n > 40 {
		t.Errorf("%v allocations for a 100-line body, want a constant handful", n)
	}
	if want := slowBody(content, 118); sink != want {
		t.Error("the body differs from lipgloss")
	}
}
