package theme

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// ── FitTrail / RenderTrail (DEVDESK-769) ─────────────────────────────────────
//
// A breadcrumb is a row of pills, " label " each, one cell apart. A pill costs
// width(label)+2 and a gap 1. FitTrail measures the plain text with
// ansi.StringWidth before any style is applied (Rule 122); these tests state the
// cost themselves, so a drift between the two fails here.

// trailCost is the width of the pills RenderTabs draws for labels.
func trailCost(labels []string) int {
	if len(labels) == 0 {
		return 0
	}
	cost := len(labels) - 1
	for _, l := range labels {
		cost += ansi.StringWidth(l) + 2
	}
	return cost
}

func trailTabs(labels []string) []TabItem {
	tabs := make([]TabItem, len(labels))
	for i, l := range labels {
		tabs[i] = TabItem{Label: l}
	}
	return tabs
}

// oldTrail is the expression the four views carried before RenderTrail.
func oldTrail(labels []string, width int) string {
	return PadWithBg(Bg(" ")+RenderTabs(trailTabs(labels), len(labels)-1), width)
}

// deepTrail is a home pill followed by n numbered folders.
func deepTrail(n int) []string {
	labels := []string{"home"}
	for i := 1; i <= n; i++ {
		labels = append(labels, fmt.Sprintf("folder-%02d", i))
	}
	return labels
}

func TestFitTrailKeepsATrailThatFits(t *testing.T) {
	tests := []struct {
		name   string
		labels []string
	}{
		{"one pill", []string{"home"}},
		{"three pills", []string{"home", "work", "devdesk"}},
		{"deep but short", deepTrail(5)},
		{"wide characters", []string{"home", "日本語", "フォルダ"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := slices.Clone(tt.labels)
			cost := trailCost(tt.labels)
			for _, room := range []int{cost, cost + 1, cost + 50} {
				got := FitTrail(tt.labels, room)
				if !slices.Equal(got, orig) {
					t.Fatalf("FitTrail(room=%d) = %q, want the trail untouched %q", room, got, orig)
				}
				if &got[0] != &tt.labels[0] {
					t.Errorf("FitTrail(room=%d) copied a trail that fits; want the same slice back", room)
				}
				// Rendered, a trail that fits is the old line byte for byte.
				width := room + 1
				if got, want := RenderTrail(trailTabs(tt.labels), width), oldTrail(tt.labels, width); got != want {
					t.Errorf("RenderTrail(width=%d) is not the old expression\n got: %q\nwant: %q", width, got, want)
				}
			}
		})
	}
}

func TestFitTrailDoesNotModifyItsInput(t *testing.T) {
	labels := deepTrail(30)
	orig := slices.Clone(labels)
	for room := 0; room < 120; room++ {
		FitTrail(labels, room)
		if !slices.Equal(labels, orig) {
			t.Fatalf("FitTrail(room=%d) modified its input", room)
		}
	}
}

func TestFitTrailDropsTheHeadAndKeepsTheTail(t *testing.T) {
	labels := deepTrail(30)
	n := len(labels)
	for _, room := range []int{40, 60, 79, 119, 174} {
		t.Run(fmt.Sprintf("room %d", room), func(t *testing.T) {
			got := FitTrail(labels, room)
			if len(got) < 2 || got[0] != TrailEllipsis {
				t.Fatalf("FitTrail = %q, want a %q pill first", got, TrailEllipsis)
			}
			if last := got[len(got)-1]; last != labels[n-1] {
				t.Errorf("current segment = %q, want %q kept whole and last", last, labels[n-1])
			}
			k := len(got) - 1
			if !slices.Equal(got[1:], labels[n-k:]) {
				t.Errorf("tail = %q, want the %d last labels %q", got[1:], k, labels[n-k:])
			}
			if cost := trailCost(got); cost > room {
				t.Errorf("cost = %d, want at most the room %d", cost, room)
			}
			// The largest k that fits: one more segment would overflow.
			wider := append([]string{TrailEllipsis}, labels[n-k-1:]...)
			if cost := trailCost(wider); cost <= room {
				t.Errorf("kept %d segments but %d fit in %d cells (cost %d)", k, k+1, room, cost)
			}
		})
	}
}

func TestFitTrailCutsAnOverlongCurrentSegment(t *testing.T) {
	long := strings.Repeat("x", 200)
	tests := []struct {
		name   string
		labels []string
	}{
		{"workspaces", []string{"home", "work", long}},
		{"jobs", []string{"Runs", "scan 󰅂 " + long}},
		{"registries", []string{"Registries", long}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const room = 60
			got := FitTrail(tt.labels, room)
			if len(got) != 2 || got[0] != TrailEllipsis {
				t.Fatalf("FitTrail = %q, want [%q, cut current segment]", got, TrailEllipsis)
			}
			last := tt.labels[len(tt.labels)-1]
			cut := got[1]
			if !strings.HasSuffix(cut, TrailEllipsis) {
				t.Errorf("cut segment = %q, want it to end with %q", cut, TrailEllipsis)
			}
			if !strings.HasPrefix(last, strings.TrimSuffix(cut, TrailEllipsis)) {
				t.Errorf("cut segment = %q, want the start of %q", cut, last)
			}
			// 3 (the "…" pill) + 1 (gap) + width+2 = room.
			if cost := trailCost(got); cost != room {
				t.Errorf("cost = %d, want the whole room %d", cost, room)
			}
		})
	}
}

func TestFitTrailOnTinyRooms(t *testing.T) {
	labels := []string{"home", "work", strings.Repeat("x", 200)}
	tests := []struct {
		room      int
		wantPills int
		wantCost  int // 0 = no pill at all
	}{
		{-1, 0, 0},
		{0, 0, 0},
		{2, 0, 0},
		{3, 1, 3},   // a lone "…"
		{5, 1, 5},   // 3 cells of label
		{7, 1, 7},   // 5 cells of label, still no head pill
		{8, 2, 8},   // "…" pill, gap, 2 cells of label
		{20, 2, 20}, //
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("room %d", tt.room), func(t *testing.T) {
			got := FitTrail(labels, tt.room)
			if len(got) != tt.wantPills {
				t.Fatalf("FitTrail = %q, want %d pill(s)", got, tt.wantPills)
			}
			if cost := trailCost(got); cost != tt.wantCost {
				t.Errorf("cost = %d, want %d", cost, tt.wantCost)
			}
			if len(got) > 0 && !strings.HasSuffix(got[len(got)-1], TrailEllipsis) {
				t.Errorf("last pill = %q, want it to end with %q (it was cut)", got[len(got)-1], TrailEllipsis)
			}
			if tt.wantPills == 2 && got[0] != TrailEllipsis {
				t.Errorf("first pill = %q, want %q", got[0], TrailEllipsis)
			}
		})
	}
}

// Whatever the room, the result fits, is valid UTF-8, and a cut label is cut on
// a cell boundary: wide characters never overshoot and never waste more than
// the one cell an odd room leaves.
func TestFitTrailCutsOnCellBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		labels []string
	}{
		{"cjk", []string{"home", "日本語", "フォルダ", strings.Repeat("漢", 60)}},
		{"nerd font", []string{"\U000f024b home", "\U000f024b projects", "\U000f024b " + strings.Repeat("é", 100)}},
		{"emoji", []string{"home", "📁 docs", "📁 " + strings.Repeat("🙂", 50)}},
		{"ascii", []string{"home", "work", strings.Repeat("x", 120)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			last := tt.labels[len(tt.labels)-1]
			for room := -2; room <= 150; room++ {
				got := FitTrail(tt.labels, room)
				if cost := trailCost(got); cost > room && (room >= 0 || len(got) != 0) {
					t.Fatalf("room %d: cost %d overflows: %q", room, cost, got)
				}
				for _, l := range got {
					if !utf8.ValidString(l) {
						t.Fatalf("room %d: %q is not valid UTF-8", room, l)
					}
				}
				if len(got) == 0 {
					if room >= 3 {
						t.Fatalf("room %d: nothing rendered although a pill of 1 cell fits", room)
					}
					continue
				}
				cut := got[len(got)-1]
				if cut == last {
					continue
				}
				if !strings.HasSuffix(cut, TrailEllipsis) {
					t.Fatalf("room %d: current segment %q is neither whole nor ending with %q", room, cut, TrailEllipsis)
				}
				if !strings.HasPrefix(last, strings.TrimSuffix(cut, TrailEllipsis)) {
					t.Fatalf("room %d: %q is not the start of %q", room, cut, last)
				}
				if waste := room - trailCost(got); waste > 1 {
					t.Fatalf("room %d: cost %d wastes %d cells; a cut loses at most one", room, trailCost(got), waste)
				}
			}
		})
	}
}

// FitTrail prices a pill at width+2 and a gap at 1. RenderTabs draws them: the
// two must agree, or a trail "that fits" overflows by the difference.
func TestRenderTabsWidthIsTheTrailCost(t *testing.T) {
	tests := []struct {
		name   string
		labels []string
	}{
		{"one", []string{"home"}},
		{"ascii", deepTrail(4)},
		{"cjk", []string{"日本語", "フォルダ"}},
		{"nerd font", []string{IconHome + " home", IconDirectory + " src"}},
		{"ellipsis pill", []string{TrailEllipsis, "work"}},
		{"emoji", []string{"📁 docs", "🙂"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ansi.StringWidth(RenderTabs(trailTabs(tt.labels), len(tt.labels)-1))
			if want := trailCost(tt.labels); got != want {
				t.Errorf("RenderTabs is %d cells wide, the cost formula says %d", got, want)
			}
		})
	}
}

func TestRenderTrailFillsTheWidth(t *testing.T) {
	labels := deepTrail(30)
	for _, width := range []int{80, 120, 175} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			got := RenderTrail(trailTabs(labels), width)
			if w := ansi.StringWidth(got); w != width {
				t.Fatalf("line is %d cells wide, want exactly %d", w, width)
			}
			plain := ansi.Strip(got)
			if !strings.HasPrefix(strings.TrimLeft(plain, " "), TrailEllipsis) {
				t.Errorf("trail = %q, want it to start with %q", plain, TrailEllipsis)
			}
			if !strings.HasSuffix(strings.TrimRight(plain, " "), labels[len(labels)-1]) {
				t.Errorf("trail = %q, want the current segment %q last", plain, labels[len(labels)-1])
			}
			if strings.Contains(plain, "home") {
				t.Errorf("trail = %q, want the head dropped", plain)
			}
			// The line is the one the spec names: the fitted trail, last pill active.
			fit := FitTrail(labels, width-1)
			if want := oldTrail(fit, width); got != want {
				t.Errorf("RenderTrail is not PadWithBg(Bg(\" \")+RenderTabs(FitTrail(labels, width-1), last), width)\n got: %q\nwant: %q", got, want)
			}
		})
	}
}

func TestRenderTrailOneCellShortDropsTheHead(t *testing.T) {
	labels := deepTrail(6)
	cost := trailCost(labels)

	if got, want := RenderTrail(trailTabs(labels), cost+1), oldTrail(labels, cost+1); got != want {
		t.Errorf("the exact fit changed:\n got: %q\nwant: %q", got, want)
	}
	got := RenderTrail(trailTabs(labels), cost)
	if w := ansi.StringWidth(got); w != cost {
		t.Errorf("one cell short: line is %d cells, want %d", w, cost)
	}
	if plain := ansi.Strip(got); !strings.HasPrefix(strings.TrimLeft(plain, " "), TrailEllipsis) {
		t.Errorf("one cell short: %q, want it to start with %q", plain, TrailEllipsis)
	}
}

func TestRenderTrailNeverOverflowsANarrowLine(t *testing.T) {
	long := strings.Repeat("x", 200)
	trails := map[string][]string{
		"deep":  deepTrail(30),
		"long":  {"home", "work", long},
		"cjk":   {"home", strings.Repeat("漢", 40)},
		"nerd":  {IconHome + " home", IconDirectory + " " + long},
		"one":   {long},
		"short": {"a"},
	}
	for name, labels := range trails {
		t.Run(name, func(t *testing.T) {
			for width := 0; width <= 40; width++ {
				got := RenderTrail(trailTabs(labels), width)
				if w := ansi.StringWidth(got); w > width {
					t.Fatalf("width %d: line is %d cells wide: %q", width, w, ansi.Strip(got))
				}
				if width > 0 && ansi.StringWidth(got) != width {
					t.Fatalf("width %d: line is %d cells wide, want it padded to the width", width, ansi.StringWidth(got))
				}
			}
			// No width yet (before the first WindowSizeMsg) is an empty line.
			if got := RenderTrail(trailTabs(labels), 0); got != "" {
				t.Errorf("width 0: got %q, want an empty line", ansi.Strip(got))
			}
		})
	}
}

func TestRenderTrailWithNoSegment(t *testing.T) {
	for _, width := range []int{1, 80} {
		got := RenderTrail(nil, width)
		if w := ansi.StringWidth(got); w != width {
			t.Errorf("width %d: empty trail is %d cells wide, want %d", width, w, width)
		}
	}
}
