package workspaces

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// minFuzzyQueryLen is the threshold below which the prompt runs no query: a
// subsequence match over a whole directory tree returns too much to be
// useful on one or two characters, and running it on every keystroke below
// that would be wasted work besides.
const minFuzzyQueryLen = 3

// FuzzyFindCancelMsg is sent when the user backs out of the fuzzy-find
// prompt without picking a result.
type FuzzyFindCancelMsg struct{}

// FuzzyFindConfirmMsg carries the absolute path of the directory chosen from
// the fuzzy-find results.
type FuzzyFindConfirmMsg struct {
	Path string
}

// fuzzyRow is one line of the results table.
type fuzzyRow struct {
	Candidate fuzzyCandidate
}

// FuzzyFinder is the "g" prompt: a query, and the whole-tree directories
// that match it, ranked. It owns its own key handling while active — the
// same shape as WorkspaceInput — and a datatable.Model purely for rendering
// and cursor movement over an already-ranked list. The table's own sort and
// "/" filter bar are never reached: only navigation keys are ever forwarded
// to it (see Update), and the order shown is the scorer's, not the table's.
type FuzzyFinder struct {
	query      textinput.Model
	loading    bool
	candidates []fuzzyCandidate
	skipped    int
	results    datatable.Model[fuzzyRow]
	width      int
}

// NewFuzzyFinder creates the prompt, focused, with an empty result set — the
// whole-tree walk behind it is kicked off separately as a Cmd
// (Model.walkWorkspaceDirsCmd), so its I/O never runs inside Update or View
// (Rule 110).
func NewFuzzyFinder() *FuzzyFinder {
	ti := textinput.New()
	ti.Placeholder = "type at least 3 characters"
	ti.Focus()
	ti.CharLimit = 200
	ti.Width = 60
	theme.StyleTextInput(&ti)

	return &FuzzyFinder{
		query:   ti,
		loading: true,
		results: datatable.New(datatable.Config[fuzzyRow]{
			Columns:    fuzzyColumns(),
			SortColumn: -1, // ranked by score, not by the table's own comparator
		}),
	}
}

func fuzzyColumns() []datatable.Column[fuzzyRow] {
	return []datatable.Column[fuzzyRow]{
		{
			Title: "", Sizing: datatable.SizingFixed, MinWidth: datatable.IconColumnWidth,
			Cell:  func(r fuzzyRow) string { return theme.IconDirectory },
			Style: func(r fuzzyRow) lipgloss.Style { return theme.IconStyle(theme.IconRoleDirectory) },
		},
		{
			Title: "Path", Sizing: datatable.SizingContent, TruncateHead: true, MinWidth: 20, Flex: 1,
			Cell: func(r fuzzyRow) string { return r.Candidate.Rel },
		},
	}
}

// SetCandidates records the whole-tree walk's result and reranks the current
// query over it, if one is long enough to run.
func (f *FuzzyFinder) SetCandidates(candidates []fuzzyCandidate, skipped int) {
	f.loading = false
	f.candidates = candidates
	f.skipped = skipped
	f.rematch()
}

// Resize adapts the prompt to the viewport, mirroring updateTableSize: the
// query line and the blank line separating it from the results are outside
// the inner table's own budget.
func (f *FuzzyFinder) Resize(width, height int) {
	f.width = width
	f.query.Width = max(width-4, 10)
	f.results.Resize(width, max(height-2, 1))
}

// Update handles input while the prompt is active.
func (f *FuzzyFinder) Update(msg tea.Msg) (*FuzzyFinder, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			return f, func() tea.Msg { return FuzzyFindCancelMsg{} }
		case "enter":
			if row, ok := f.results.Selected(); ok {
				path := row.Candidate.Abs
				return f, func() tea.Msg { return FuzzyFindConfirmMsg{Path: path} }
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
// 3-character threshold, or before the walk has landed, the results table is
// simply empty — StatusText says why.
func (f *FuzzyFinder) rematch() {
	query := strings.TrimSpace(f.query.Value())
	if f.loading || len(query) < minFuzzyQueryLen {
		f.results.SetItems(nil)
		return
	}

	type scored struct {
		row   fuzzyRow
		score int
	}
	matches := make([]scored, 0, len(f.candidates))
	for _, c := range f.candidates {
		if score, ok := matchFuzzy(c.Rel, query); ok {
			matches = append(matches, scored{row: fuzzyRow{Candidate: c}, score: score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })

	rows := make([]fuzzyRow, len(matches))
	for i, m := range matches {
		rows[i] = m.row
	}
	f.results.SetItems(rows)
	f.results.Remeasure()
	f.results.GotoTop()
}

// StatusText is the footer's one line while the prompt is open.
func (f *FuzzyFinder) StatusText() string {
	query := strings.TrimSpace(f.query.Value())
	switch {
	case f.loading:
		return "Scanning workspace directories..."
	case len(query) < minFuzzyQueryLen:
		return "Type at least 3 characters to search"
	case len(f.results.Items()) == 0:
		return "No matches"
	default:
		return fmt.Sprintf("%d match(es)", len(f.results.Items()))
	}
}

// View renders the prompt full-viewport: the query line, a blank line, then
// the ranked results table. A ranked list needs real vertical space, which
// is why this does not follow ModeAdding/ModeRenaming's centered-overlay
// pattern in this same package — full-viewport is what Rule 112 itself
// asks for.
func (f *FuzzyFinder) View() string {
	label := theme.KeyStyle.Render(theme.IconCircleSmall + " Find " + theme.IconChevronRight + " ")
	queryLine := theme.BgLine(label+f.query.View(), f.width)
	return queryLine + "\n" + theme.EmptyLineBg(f.width) + "\n" + f.results.View()
}
