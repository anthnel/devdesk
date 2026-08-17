package viewer

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/anthnel/devdesk/internal/viewer"
)

// syntaxStyle is the colour a token class renders in.
//
// The background is set on every style, without exception. lipgloss does not
// inherit one (Rule 115), and a styled run closes with a reset that takes the
// app background with it — so a single uncoloured token would show the
// terminal's own background from there to the end of the line. Setting it
// everywhere is what makes that unexpressible rather than a thing to remember.
//
// This mapping lives here, in the UI package, rather than in theme or in
// internal/viewer: it is the one place that already imports both, so neither of
// them has to learn about the other.
func syntaxStyle(class viewer.TokenClass) lipgloss.Style {
	base := lipgloss.NewStyle().Background(theme.ColorBackground)

	switch class {
	case viewer.ClassKey:
		return base.Foreground(theme.ColorSyntaxKey)
	case viewer.ClassString:
		return base.Foreground(theme.ColorSyntaxString)
	case viewer.ClassNumber:
		return base.Foreground(theme.ColorSyntaxNumber)
	case viewer.ClassLiteral:
		return base.Foreground(theme.ColorSyntaxLiteral)
	case viewer.ClassPunct:
		return base.Foreground(theme.ColorSyntaxPunct)
	case viewer.ClassTag:
		return base.Foreground(theme.ColorSyntaxTag)
	case viewer.ClassAttr:
		return base.Foreground(theme.ColorSyntaxAttr)
	case viewer.ClassComment:
		return base.Foreground(theme.ColorSyntaxComment)
	default:
		return base.Foreground(theme.ColorText)
	}
}

// matchStyle is a run the current search found.
//
// Reverse video, like a selected table row, and it overrides whatever the run
// would otherwise have been — its syntax class or its log level. A search
// occurrence is the most urgent thing on the screen while a search is running,
// and a colour that had to compete with eight others for attention would not be
// findable, which is the whole point of showing it.
func matchStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(theme.ColorSearchMatch).
		Foreground(theme.ColorSearchMatchFg)
}

// levelStyle colours a whole log line by its severity.
//
// A log line is styled in one piece rather than per token: its level is a fact
// about the entry, and picking out an ERROR at a glance is the entire reason
// anyone filters a log. Info is left in the default text colour — it is the
// majority state, and colouring the majority informs no one (Rule 122's colour
// discipline).
func levelStyle(level viewer.Level) lipgloss.Style {
	switch level {
	case viewer.LevelError:
		return theme.StatusErrorStyle.Background(theme.ColorBackground)
	case viewer.LevelWarn:
		return theme.StatusWarningStyle.Background(theme.ColorBackground)
	case viewer.LevelDebug, viewer.LevelTrace:
		return theme.DimStyle.Background(theme.ColorBackground)
	default:
		return lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText)
	}
}

// nodeStyle colours a tree value by what the node holds.
//
// A tree cell carries exactly one class, which is why the tree can be a
// datatable at all: datatable applies one Style per cell and cannot express
// several colours inside one, and it never has to here.
func nodeStyle(kind viewer.NodeKind) lipgloss.Style {
	return syntaxStyle(nodeClass(kind))
}

func nodeClass(kind viewer.NodeKind) viewer.TokenClass {
	switch kind {
	case viewer.NodeString, viewer.NodeText:
		return viewer.ClassString
	case viewer.NodeNumber:
		return viewer.ClassNumber
	case viewer.NodeBool, viewer.NodeNull:
		return viewer.ClassLiteral
	case viewer.NodeObject, viewer.NodeArray:
		return viewer.ClassPunct
	case viewer.NodeComment:
		return viewer.ClassComment
	default:
		return viewer.ClassText
	}
}

// keyClass is how a node's own name is coloured: an XML attribute reads as an
// attribute, an element as a tag, a JSON key as a key.
func keyClass(kind viewer.NodeKind) viewer.TokenClass {
	switch kind {
	case viewer.NodeAttr:
		return viewer.ClassAttr
	case viewer.NodeElement:
		return viewer.ClassTag
	case viewer.NodeComment:
		return viewer.ClassComment
	default:
		return viewer.ClassKey
	}
}
