package components

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

const (
	rowProgress = "Scanning — 3/12"
	rowSep      = " · "
)

// isolateRowHome pins HOME so /home/u/... folds to ~/... whatever machine runs
// the test (HOME is shared across a test process, see CLAUDE.md).
func isolateRowHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", "/home/u")
	t.Setenv("USERPROFILE", "/home/u")
}

func runningDetail() RowDetail {
	return RowDetail{Glyph: "●", Lead: "Scanning ", Target: "/home/u/w/app/api"}
}

func failedDetail() RowDetail {
	return RowDetail{
		Glyph:  "⊗",
		Target: "/home/u/w/app/api",
		Tail:   ": last scan failed — check logs · earlier failure: sync (see :jobs)",
	}
}

func refDetail() RowDetail {
	return RowDetail{Glyph: "●", Lead: "Pulling ", Target: "nginx:1.27"}
}

func eastAsian(t *testing.T) {
	t.Helper()
	prev := runewidth.DefaultCondition.EastAsianWidth
	runewidth.DefaultCondition.EastAsianWidth = true
	t.Cleanup(func() { runewidth.DefaultCondition.EastAsianWidth = prev })
}

// ── RowDetail ────────────────────────────────────────────────────────────────

func TestRowDetailZeroAndString(t *testing.T) {
	isolateRowHome(t)
	tests := []struct {
		name     string
		d        RowDetail
		wantZero bool
		want     string
	}{
		{"zero value", RowDetail{}, true, ""},
		{"glyph only is not zero", RowDetail{Glyph: "●"}, false, "● "},
		{"lead only is not zero", RowDetail{Lead: "x"}, false, "x"},
		{"running", runningDetail(), false, "● Scanning ~/w/app/api"},
		{"no glyph has no leading space", RowDetail{Lead: "Running since 3 hr ago"}, false, "Running since 3 hr ago"},
		{"image ref is verbatim", refDetail(), false, "● Pulling nginx:1.27"},
		{"failed with tail", failedDetail(), false,
			"⊗ ~/w/app/api: last scan failed — check logs · earlier failure: sync (see :jobs)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.IsZero(); got != tt.wantZero {
				t.Errorf("IsZero = %v, want %v", got, tt.wantZero)
			}
			if tt.wantZero {
				return
			}
			if got := tt.d.String(); got != tt.want {
				t.Errorf("String = %q, want %q", got, tt.want)
			}
		})
	}
}

// ── Composition and truncation order ─────────────────────────────────────────

func TestRowStatusLineComposition(t *testing.T) {
	isolateRowHome(t)
	tests := []struct {
		name     string
		detail   RowDetail
		progress string
		width    int
		want     string
	}{
		{"detail and progress", runningDetail(), rowProgress, 120,
			"● Scanning ~/w/app/api · Scanning — 3/12"},
		{"exactly full width", runningDetail(), rowProgress, 40,
			"● Scanning ~/w/app/api · Scanning — 3/12"},
		{"detail alone", runningDetail(), "", 120, "● Scanning ~/w/app/api"},
		{"progress alone", RowDetail{}, rowProgress, 120, rowProgress},
		{"nothing", RowDetail{}, "", 120, ""},
		{"path shrinks first, progress intact", runningDetail(), rowProgress, 39,
			"● Scanning ~/w/a/api · Scanning — 3/12"},
		{"no progress, path shrinks before any cut", runningDetail(), "", 20,
			"● Scanning ~/w/a/api"},
		{"image ref is cut, never path-shortened", refDetail(), "", 14,
			theme.TruncateWidth("● Pulling nginx:1.27", 14)},
		{"progress alone below the detail threshold", runningDetail(), rowProgress, 25, rowProgress},
		{"progress truncated when alone and too wide", runningDetail(), rowProgress, 10,
			theme.TruncateWidth(rowProgress, 10)},
		{"zero detail, progress truncated", RowDetail{}, rowProgress, 10,
			theme.TruncateWidth(rowProgress, 10)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RowStatusLine(tt.detail, tt.progress, tt.width)
			if got != tt.want {
				t.Errorf("RowStatusLine(width %d) = %q, want %q", tt.width, got, tt.want)
			}
		})
	}
}

// The tail of the detail gives way once the path cannot shrink enough: the
// progress stays whole and the cut carries an ellipsis.
func TestRowStatusLineCutsTheDetailTailBeforeTheProgress(t *testing.T) {
	isolateRowHome(t)
	suffix := rowSep + rowProgress
	// Path room is 5 here, under the 6-column minimum: no ShortPath, tail cut.
	got := RowStatusLine(runningDetail(), rowProgress, 34)
	if !strings.HasSuffix(got, suffix) {
		t.Errorf("progress not intact: %q", got)
	}
	if !strings.Contains(got, theme.Ellipsis) {
		t.Errorf("detail not cut with an ellipsis: %q", got)
	}
	if !strings.HasPrefix(got, "● Scanning ~/") {
		t.Errorf("detail lost its head: %q", got)
	}
	if w := theme.StringWidth(got); w != 34 {
		t.Errorf("width = %d, want 34 (the cut fills its budget)", w)
	}
}

// Thresholds: 8 columns for the detail, so the progress needs width >= 26 to
// share the line, and below that it stands alone and whole.
func TestRowStatusLineDetailThreshold(t *testing.T) {
	isolateRowHome(t)
	suffix := rowSep + rowProgress // 18 columns
	if got := RowStatusLine(runningDetail(), rowProgress, 26); !strings.HasSuffix(got, suffix) || theme.StringWidth(got) > 26 {
		t.Errorf("width 26 should keep an 8-column detail: %q", got)
	}
	if got := RowStatusLine(runningDetail(), rowProgress, 25); got != rowProgress {
		t.Errorf("width 25 should drop the detail: %q", got)
	}
}

// Invariants over every width, with and without East Asian ambiguous width.
func TestRowStatusLineNeverExceedsTheWidth(t *testing.T) {
	isolateRowHome(t)
	long := strings.Repeat("very long progress ", 6)
	details := map[string]RowDetail{
		"running": runningDetail(),
		"failed":  failedDetail(),
		"ref":     refDetail(),
		"zero":    {},
	}
	progresses := map[string]string{"none": "", "short": rowProgress, "long": long}
	run := func(t *testing.T) {
		for dn, d := range details {
			for pn, p := range progresses {
				for w := 1; w <= 140; w++ {
					got := RowStatusLine(d, p, w)
					if strings.ContainsAny(got, "\n\r") {
						t.Fatalf("%s/%s width %d: not a single line: %q", dn, pn, w, got)
					}
					if rw := runewidth.StringWidth(got); rw > w {
						t.Fatalf("%s/%s width %d: line is %d columns: %q", dn, pn, w, rw, got)
					}
				}
			}
		}
	}
	t.Run("default", run)
	t.Run("east asian", func(t *testing.T) { eastAsian(t); run(t) })
}

// ── Status.Detail through FooterMessage.View ────────────────────────────────

// legacyView is FooterMessage.View exactly as it was before Status.Detail
// existed. A Status with no detail must still render byte for byte like this.
func legacyView(f *FooterMessage, width int, status Status) string {
	if width <= 0 {
		return ""
	}
	if f.text != "" {
		return centerLine(f.level.Style().Render(theme.TruncateWidth(f.text, width)), width)
	}
	if status.Text == "" {
		return theme.EmptyLineBg(width)
	}
	spinner, budget := "", width
	if status.Spinner && f.spinnerFrame != "" {
		spinner = f.spinnerFrame + theme.Bg(" ")
		budget -= lipgloss.Width(f.spinnerFrame) + 1
	}
	if budget <= 0 {
		return theme.EmptyLineBg(width)
	}
	return centerLine(spinner+status.Level.Style().Render(theme.TruncateWidth(status.Text, budget)), width)
}

func TestAStatusWithoutDetailRendersAsBefore(t *testing.T) {
	statuses := map[string]Status{
		"empty":         {},
		"text":          {Text: "Scanning — 3/12"},
		"text spinner":  {Text: "Loading images...", Spinner: true},
		"error level":   {Text: "Docker unreachable", Level: LevelError},
		"warning level": {Text: "Nothing to scan", Level: LevelWarning},
		"long":          {Text: strings.Repeat("long status ", 20)},
	}
	for _, frame := range []string{"", "⠋"} {
		for name, st := range statuses {
			for _, width := range []int{0, 1, 2, 3, 20, 80, 140} {
				t.Run(fmt.Sprintf("%s/frame=%q/%d", name, frame, width), func(t *testing.T) {
					var f FooterMessage
					f.SetSpinnerFrame(frame)
					if got, want := f.View(width, st), legacyView(&f, width, st); got != want {
						t.Errorf("View differs from the reference\n got: %q\nwant: %q", got, want)
					}
				})
			}
		}
	}
}

func TestADetailRendersOneCenteredLineOfTheExactWidth(t *testing.T) {
	isolateRowHome(t)
	long := strings.Repeat("progress ", 20)
	statuses := map[string]Status{
		"long detail":   {Detail: failedDetail()},
		"long progress": {Text: long},
		"both":          {Detail: failedDetail(), Text: long},
		"detail+short":  {Detail: runningDetail(), Text: rowProgress},
	}
	run := func(t *testing.T, measure func(string) int) {
		for name, st := range statuses {
			for w := 1; w <= 140; w++ {
				var f FooterMessage
				got := f.View(w, st)
				if strings.Contains(got, "\n") {
					t.Fatalf("%s width %d: View is not one line", name, w)
				}
				if m := measure(stripANSI(got)); m != w {
					t.Fatalf("%s width %d: View is %d columns: %q", name, w, m, stripANSI(got))
				}
			}
		}
	}
	t.Run("default", func(t *testing.T) { run(t, lipgloss.Width) })
	t.Run("east asian", func(t *testing.T) { eastAsian(t); run(t, runewidth.StringWidth) })
}

func TestTheViewShowsTheDetailThenProgress(t *testing.T) {
	isolateRowHome(t)
	var f FooterMessage
	got := strings.TrimSpace(stripANSI(f.View(120, Status{Detail: runningDetail(), Text: rowProgress})))
	if want := "● Scanning ~/w/app/api · Scanning — 3/12"; got != want {
		t.Errorf("View = %q, want %q", got, want)
	}
}

// Criterion 4: a timed message beats the detail, and the detail comes back by
// itself when the timer expires. The expiry is built, never slept.
func TestATimedMessageBeatsTheDetailAndTheDetailReturns(t *testing.T) {
	isolateRowHome(t)
	status := Status{Detail: runningDetail(), Text: rowProgress}
	const detailLine = "● Scanning ~/w/app/api · Scanning — 3/12"

	tests := []struct {
		name string
		set  func(f *FooterMessage)
		text string
	}{
		{"error", func(f *FooterMessage) { f.Error("Failed to scan ~/w/app/api — check logs") }, "Failed to scan"},
		{"warn", func(f *FooterMessage) { f.Warn("Scan already in progress") }, "Scan already"},
		{"info", func(f *FooterMessage) { f.Info("Synced 4 repositories") }, "Synced 4"},
		{"post", func(f *FooterMessage) {
			f.Handle(PostFooterMsg{ID: 99, Level: LevelError, Text: "MCP server did not start"})
		}, "MCP server"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var f FooterMessage
			tt.set(&f)
			shown := strings.TrimSpace(stripANSI(f.View(120, status)))
			if !strings.HasPrefix(shown, tt.text) || strings.Contains(shown, "Scanning —") {
				t.Errorf("message does not take precedence over the detail: %q", shown)
			}
			if !f.Handle(ClearFooterMsg{ID: f.ID()}) {
				t.Fatal("the expiry was not consumed")
			}
			if back := strings.TrimSpace(stripANSI(f.View(120, status))); back != detailLine {
				t.Errorf("after expiry View = %q, want %q", back, detailLine)
			}
		})
	}
}

// A stale expiry must not bring the detail back over a newer message.
func TestAStaleExpiryDoesNotRevealTheDetail(t *testing.T) {
	isolateRowHome(t)
	var f FooterMessage
	f.Error("first")
	stale := ClearFooterMsg{ID: f.ID()}
	f.Warn("second")
	f.Handle(stale)
	got := strings.TrimSpace(stripANSI(f.View(120, Status{Detail: runningDetail()})))
	if got != "second" {
		t.Errorf("View = %q, want the newer message", got)
	}
}

// A glyph means the row's dot already moves: no spinner frame beside it. A
// detail with no glyph keeps the spinner the view asked for.
func TestAGlyphDetailDropsTheSpinner(t *testing.T) {
	isolateRowHome(t)
	const frame = "⠋"
	tests := []struct {
		name      string
		detail    RowDetail
		wantFrame bool
	}{
		{"glyph", runningDetail(), false},
		{"no glyph", RowDetail{Lead: "Running since 3 hr ago"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var f FooterMessage
			f.SetSpinnerFrame(frame)
			got := stripANSI(f.View(80, Status{Detail: tt.detail, Spinner: true}))
			if has := strings.Contains(got, frame); has != tt.wantFrame {
				t.Errorf("spinner frame present = %v, want %v: %q", has, tt.wantFrame, got)
			}
			if w := lipgloss.Width(got); w != 80 {
				t.Errorf("width = %d, want 80", w)
			}
		})
	}
}

// With a long tail and less than minPathCols left for the path, the path is
// first reduced to minPathCols, and only then is the end of the detail cut.
func TestRowStatusLineShrinksThePathToItsMinimumBeforeCuttingTheTail(t *testing.T) {
	isolateRowHome(t)
	const room = 40
	d := failedDetail()
	got := RowStatusLine(d, "", room)
	shortHead := d.head() + theme.ShortPath(d.Target, minPathCols)
	if !strings.HasPrefix(got, shortHead) {
		t.Errorf("RowStatusLine(width %d) = %q, want it to start with %q", room, got, shortHead)
	}
	if strings.HasPrefix(got, d.head()+theme.FoldHome(d.Target)) {
		t.Errorf("path kept at full length although the tail had to be cut: %q", got)
	}
	if !strings.HasSuffix(got, theme.Ellipsis) {
		t.Errorf("tail not cut with an ellipsis: %q", got)
	}
	if w := theme.StringWidth(got); w > room {
		t.Errorf("width = %d, want <= %d", w, room)
	}
}

// A short detail that fits whole in what is left is kept: the drop threshold
// is min(minDetailCols, the detail's own width), not minDetailCols alone.
func TestRowStatusLineKeepsAShortDetailThatFits(t *testing.T) {
	short := RowDetail{Glyph: "●", Lead: "Done"} // 6 columns, under minDetailCols
	if w := theme.StringWidth(short.String()); w != 6 {
		t.Fatalf("fixture width = %d, want 6", w)
	}
	tests := []struct {
		name     string
		width    int
		progress string
		want     string
	}{
		{"fits with room to spare", 13, "abc", "● Done · abc"},
		{"fits in exactly its width", 12, "abc", "● Done · abc"},
		{"one column short, progress alone", 11, "abc", "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RowStatusLine(short, tt.progress, tt.width); got != tt.want {
				t.Errorf("RowStatusLine(width %d) = %q, want %q", tt.width, got, tt.want)
			}
		})
	}
}
