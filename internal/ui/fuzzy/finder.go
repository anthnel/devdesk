package fuzzy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// MinQueryLen is the threshold below which the prompt runs no query: a
// subsequence match over a whole tree returns too much to be useful on one or
// two characters, and running it on every keystroke below that would be
// wasted work besides.
const MinQueryLen = 3

// CancelMsg is sent when the user backs out of the prompt without picking a
// result.
type CancelMsg struct{}

// ConfirmMsg carries the Key of the candidate chosen from the results.
type ConfirmMsg struct {
	Key string
}

// Candidate is one thing the prompt can match.
//
// Label is what the query is matched against and what the results table
// shows — a path relative to whatever root the view browses, since that is
// what a user types from memory. Key is what ConfirmMsg hands back, and means
// something only to the view: an absolute directory for the workspaces, a
// forge path for the explorer. Icon and Role are the row's glyph and the theme
// role that colours it (Rule 125).
type Candidate struct {
	Key   string
	Label string
	Icon  string
	Role  theme.IconRole
}

// Finder is the "g" prompt: a query, and the candidates that match it,
// ranked. It owns its own key handling while active, and a datatable.Model
// purely for rendering and cursor movement over an already-ranked list. The
// table's own sort and "/" filter bar are never reached: only navigation keys
// are ever forwarded to it (see Update), and the order shown is the scorer's,
// not the table's.
type Finder struct {
	query       textinput.Model
	loading     bool
	loadingText string
	candidates  []Candidate
	skipped     int
	results     datatable.Model[Candidate]
}

// New creates the prompt, focused, with an empty result set. The candidates
// arrive separately, through SetCandidates: finding them is I/O, and it runs
// in a Cmd the view starts, never inside Update or View (Rule 110).
//
// loadingText is the footer's line until they do — the view knows what it is
// waiting for, this component does not.
func New(loadingText string) *Finder {
	ti := textinput.New()
	ti.Placeholder = fmt.Sprintf("type at least %d characters", MinQueryLen)
	ti.Focus()
	ti.CharLimit = 200
	ti.Width = 60
	theme.StyleTextInput(&ti)

	return &Finder{
		query:       ti,
		loading:     true,
		loadingText: loadingText,
		results: datatable.New(datatable.Config[Candidate]{
			Columns:    columns(),
			SortColumn: -1, // ranked by score, not by the table's own comparator
		}),
	}
}

func columns() []datatable.Column[Candidate] {
	return []datatable.Column[Candidate]{
		{
			Title: "", Sizing: datatable.SizingFixed, MinWidth: datatable.IconColumnWidth,
			Cell:  func(c Candidate) string { return c.Icon },
			Style: func(c Candidate) lipgloss.Style { return theme.IconStyle(c.Role) },
		},
		{
			Title: "Path", Sizing: datatable.SizingContent, TruncateHead: true, MinWidth: 20, Flex: 1,
			Cell: func(c Candidate) string { return c.Label },
		},
	}
}

// SetCandidates records what the view found and reranks the current query over
// it, if one is long enough to run. skipped is how many places could not be
// read, so the footer can say the list is incomplete rather than let it pass
// for whole (D59).
//
// It can be called again while the prompt is open — the explorer does when a
// fresher index lands — and the query the user already typed is kept.
func (f *Finder) SetCandidates(candidates []Candidate, skipped int) {
	f.loading = false
	f.candidates = candidates
	f.skipped = skipped
	f.rematch()
}

// Loading reports whether the candidates have not arrived yet.
func (f *Finder) Loading() bool { return f.loading }

// Resize adapts the prompt to the viewport. The query lives in the footer's bar
// slot (RenderBar), so the results table is the whole viewport, the same as
// the table the view normally shows.
func (f *Finder) Resize(width, height int) {
	f.results.Resize(width, max(height-1, 1))
}

// Update handles input while the prompt is active.
func (f *Finder) Update(msg tea.Msg) (*Finder, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			return f, func() tea.Msg { return CancelMsg{} }
		case "enter":
			if c, ok := f.results.Selected(); ok {
				k := c.Key
				return f, func() tea.Msg { return ConfirmMsg{Key: k} }
			}
			return f, nil
		case "up", "down", "pgup", "pgdown", "home", "end":
			f.results.Update(msg)
			return f, nil
		}
	}

	before := f.query.Value()
	var cmd tea.Cmd
	f.query, cmd = f.query.Update(msg)
	if f.query.Value() != before {
		f.rematch()
	}
	return f, cmd
}

// rematch reranks the loaded candidates against the current query. Below the
// threshold, or before the candidates have landed, the results table is simply
// empty — StatusText says why.
func (f *Finder) rematch() {
	query := strings.TrimSpace(f.query.Value())
	if f.loading || len(query) < MinQueryLen {
		f.results.SetItems(nil)
		return
	}

	type scored struct {
		c     Candidate
		score int
	}
	matches := make([]scored, 0, len(f.candidates))
	for _, c := range f.candidates {
		if score, ok := Match(c.Label, query); ok {
			matches = append(matches, scored{c: c, score: score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })

	rows := make([]Candidate, len(matches))
	for i, m := range matches {
		rows[i] = m.c
	}
	f.results.SetItems(rows)
	f.results.Remeasure()
	f.results.GotoTop()
}

// StatusText is the footer's one line while the prompt is open.
func (f *Finder) StatusText() string {
	switch {
	case f.loading:
		return f.loadingText
	case len(strings.TrimSpace(f.query.Value())) < MinQueryLen:
		return fmt.Sprintf("Type at least %d characters to search", MinQueryLen)
	case f.skipped > 0:
		return sharedcomponents.Plural(f.skipped, "place", "places") + " could not be read — results may be incomplete"
	default:
		return ""
	}
}

// MatchHint is the bar's inline count, the same role goto's "1-N" plays.
func (f *Finder) MatchHint() string {
	query := strings.TrimSpace(f.query.Value())
	if f.loading || len(query) < MinQueryLen {
		return ""
	}
	return fmt.Sprintf("%d match(es)", len(f.results.Items()))
}

// RenderBar draws the query in the filter bar's own frame — the same slot and
// the same components.BarFrame the "/" filter and the viewer's own "g" (go to
// line) already use, so the rectangle that closes the viewport is the same one
// whichever occupies it.
func (f *Finder) RenderBar(width int) string {
	inner := theme.Bg(" ") + theme.KeyStyle.Render("Find") +
		theme.Bg(" ") + theme.DimStyle.Render(theme.IconChevronRight) + theme.Bg(" ") +
		f.query.View()
	if hint := f.MatchHint(); hint != "" {
		inner += theme.Bg("  ") + theme.DimStyle.Render(hint)
	}
	return sharedcomponents.BarFrame(width, inner)
}

// RenderFooter is the whole footer while the prompt is open: the bar, the
// blank line, and the info line the view's own footer message renders with
// this prompt's status (Rule 128). Four lines — FooterHeight.
func (f *Finder) RenderFooter(width int, footer *sharedcomponents.FooterMessage) string {
	info := footer.View(width, sharedcomponents.Status{Text: f.StatusText()})
	return f.RenderBar(width) + "\n" + theme.EmptyLineBg(width) + "\n" + info
}

// FooterHeight is what RenderFooter occupies: the bar's two lines, the blank
// line and the info line (Rule 124).
const FooterHeight = 4

// View renders the ranked results — the whole viewport.
func (f *Finder) View() string {
	return f.results.View()
}
