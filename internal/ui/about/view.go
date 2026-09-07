package about

import (
	"strconv"
	"strings"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/anthnel/devdesk/internal/version"
)

// labelGap separates the widest label from the value column. Two spaces rather
// than one: a single space reads as part of the label when a value starts with
// a letter, which most of them do.
const labelGap = 2

// bodyIndent is the left margin of a field, one deeper than its section title.
const bodyIndent = "   "

// GetTitle names the view.
func (m Model) GetTitle() string { return theme.IconInfo + " About" }

// GetIcon returns the view icon. The title carries it, as everywhere else.
func (m Model) GetIcon() string { return "" }

// GetHeaderInfo says which build is running.
//
// This is the same value as the body's first line, and that is deliberate:
// the header is what you read without scrolling, and this view is exactly
// the one you open for that answer.
func (m Model) GetHeaderInfo(_ string) []shortcut.HeaderInfo {
	return []shortcut.HeaderInfo{
		{Key: "Version", Value: version.Get().Short(), Style: theme.HeaderValueStyle},
	}
}

// GetShortcuts returns the header's shortcut column (Rules 130, 137, 138).
//
// Scrolling is not advertised — ↑↓, PageUp/PageDown and Home/End are universal
// (Rule 138). What is left is what this screen does not have: there is nothing
// to refresh, so `ctrl+r` is absent rather than greyed. A key that could never
// become available is not a disabled key, it is not a key of this view
// (Rule 130).
func (m Model) GetShortcuts() shortcut.Shortcuts {
	return []shortcut.Shortcut{
		{Key: "ctrl+p", Description: "Open command mode"},
		{Key: "?", Description: "Help"},
	}
}

// GetFooterHeight budgets the footer (Rule 124): the blank line and the info
// line, both of which RenderFooter always renders.
func (m Model) GetFooterHeight() int { return 2 }

// RenderFooter renders the lines below the viewport (Rule 124).
func (m Model) RenderFooter(width int) string {
	return theme.EmptyLineBg(width) + "\n" + m.footer.View(width, m.status())
}

// status says how much is left below. It is derived on every frame rather than
// posted, so it carries no timer (Rule 128) — a message posted when the screen
// opened would expire while the reader is still scrolling.
func (m Model) status() sharedcomponents.Status {
	hidden := len(m.lines()) - m.height - m.offset
	if hidden <= 0 {
		return sharedcomponents.Status{}
	}
	return sharedcomponents.Status{
		Text: strconv.Itoa(hidden) + " more " + sharedcomponents.Plural(hidden, "line", "lines") + " below",
	}
}

// View renders the window of the body that the offset selects.
func (m Model) View() string {
	lines := m.lines()

	var out []string
	for i := m.offset; i < len(lines) && len(out) < m.height; i++ {
		out = append(out, theme.PadWithBg(lines[i], m.width))
	}
	for len(out) < m.height {
		out = append(out, theme.EmptyLineBg(m.width))
	}
	return strings.Join(out, "\n")
}

// lines renders the body once, as rows ready to be padded.
//
// The body is recomputed on every call rather than cached in the model: it
// only depends on `m.sections`, which never changes after New, and a cached
// copy would be a second state to keep consistent for a dozen or so lines
// of text.
//
// It opens on a blank line, which is Rule 131's one-line top padding applied
// to a body that is not a form: content welded to the viewport's border reads
// as clipped. The blank belongs to the body rather than to View, so it scrolls
// away with everything else — a padding rendered outside the window would cost
// a row for the whole read, and the three places that reason about the body's
// height all count `lines()`.
func (m Model) lines() []string {
	width := m.labelWidth()

	out := []string{""}
	for i, sec := range m.sections {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, theme.Bg(" ")+theme.SubTitleStyle.Render(sec.Title))
		for _, f := range sec.Fields {
			out = append(out, renderField(f, width))
		}
	}
	return out
}

// renderField renders one label/value row, the value aligned on the widest
// label of the screen so the two columns read as columns.
//
// The missing value is in `DimStyle` and the others in ordinary text: that
// is Rule 122's color discipline — what is gray is what is not there, and
// nothing else on this screen deserves to stand out without reading it.
func renderField(f field, labelWidth int) string {
	label := f.Label + strings.Repeat(" ", labelWidth-len(f.Label)+labelGap)

	if f.Value == "" || f.Value == version.Unknown {
		return theme.Bg(bodyIndent) + theme.KeyStyle.Render(label) + theme.DimStyle.Render(version.Unknown)
	}
	return theme.Bg(bodyIndent) + theme.KeyStyle.Render(label) + theme.Bg(f.Value)
}

// labelWidth is the widest label across every section, so the value column is
// the same one throughout rather than one per section.
func (m Model) labelWidth() int {
	widest := 0
	for _, sec := range m.sections {
		for _, f := range sec.Fields {
			if len(f.Label) > widest {
				widest = len(f.Label)
			}
		}
	}
	return widest
}

// GetHelpContent returns the help for `?` (Rule 114).
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title: "About",
		Description: "Which build of DevDesk is running, and where it keeps its files. " +
			"Everything on this screen is known at startup and none of it is measured, which is why there is nothing to refresh.",
		KeyBindings: []help.KeyBinding{
			{Key: "↑/↓", Description: "Scroll"},
			{Key: "ctrl+p", Description: "Open command mode"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Build",
				Body: "Version: the release this binary was built from, or 'dev' for a working-tree build. A version is set at build time; nothing at runtime can discover one.\n" +
					"Commit: the revision it was built from, shortened to seven characters. '(modified)' means the tree had uncommitted changes, so the SHA alone does not describe what is running — worth quoting in a bug report.\n" +
					"Built: when, in UTC.\n" +
					"Go, Platform: the toolchain that compiled it, and the OS and architecture it targets.\n\n" +
					"'unknown' is an honest answer: a binary built outside a repository, with no build flags, has no way to know its own revision.",
			},
			{
				Title: "Paths",
				Body: "Config: the directory holding config.yaml and the per-context files.\n" +
					"Themes: where a custom theme file is picked up from.\n" +
					"Cache: scan results and discovered registry members. Deleting it costs a rescan, nothing more.\n" +
					"Log: where the application's log output goes.\n\n" +
					"No secret is written to any of these — tokens and passwords go to the host secret manager instead.",
			},
			{
				Title: "What is not here",
				Body:  "The versions of Trivy, gitleaks, plumber and Docker are on the dashboard, not here. They describe the machine rather than this build, they change without DevDesk being rebuilt, and they have to be gone and fetched — which is the line this screen draws.",
			},
		},
	}
}
