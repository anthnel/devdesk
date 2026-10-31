package auth

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/components"
)

// The router posts its messages (ctx new, MCP) with components.PostFooter. The
// auth view is where a new context lands, so it has to hold a FooterMessage
// like every other view (Rule 128). The expiry is built by hand instead of
// running the Cmd: tea.Tick really sleeps.

// footerInfoLine returns the last line of RenderFooter, the info line.
func footerInfoLine(m *Model, width int) string {
	lines := strings.Split(m.RenderFooter(width), "\n")
	return lines[len(lines)-1]
}

func TestPostFooterMsgAppearsCenteredInTheFooter(t *testing.T) {
	cases := []struct {
		name  string
		level components.Level
		text  string
	}{
		{"info", components.LevelInfo, `Context "staging" created`},
		{"warning", components.LevelWarning, "Context already active"},
		{"error", components.LevelError, "Could not switch context"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t, testConfig(), newFakeStorage())
			const width = 100

			m = feed(t, m, components.PostFooterMsg{ID: uint64(9000 + i), Level: tc.level, Text: tc.text})

			line := footerInfoLine(m, width)
			if !strings.Contains(line, tc.text) {
				t.Fatalf("info line = %q, want it to carry %q", line, tc.text)
			}
			if got := lipgloss.Width(line); got != width {
				t.Errorf("info line width = %d, want the full %d", got, width)
			}
			plain := ansi.Strip(line)
			lead := len(plain) - len(strings.TrimLeft(plain, " "))
			trail := len(plain) - len(strings.TrimRight(plain, " "))
			if lead == 0 || lead-trail > 1 || trail-lead > 1 {
				t.Errorf("message not centered: %d leading, %d trailing cells", lead, trail)
			}
		})
	}
}

func TestPostedFooterMessageExpires(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	const id = 9100

	m = feed(t, m, components.PostFooterMsg{ID: id, Level: components.LevelInfo, Text: "Context \"x\" created"})
	if !strings.Contains(m.RenderFooter(100), "created") {
		t.Fatal("message not shown before its expiry")
	}

	// A stale expiry (another message's timer) leaves it alone.
	m = feed(t, m, components.ClearFooterMsg{ID: id + 1})
	if !strings.Contains(m.RenderFooter(100), "created") {
		t.Error("a stale ClearFooterMsg removed the message")
	}

	m = feed(t, m, components.ClearFooterMsg{ID: id})
	if strings.Contains(m.RenderFooter(100), "created") {
		t.Error("message still shown after its ClearFooterMsg")
	}
}

func TestFooterHeightIsUnchangedWithAMessage(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	want := m.GetFooterHeight()
	if want != 2 {
		t.Fatalf("GetFooterHeight() = %d, want 2 (blank line + info line)", want)
	}

	m = feed(t, m, components.PostFooterMsg{ID: 9200, Level: components.LevelWarning, Text: "something to read"})

	if got := m.GetFooterHeight(); got != want {
		t.Errorf("GetFooterHeight() = %d with a message, want %d", got, want)
	}
	if got := strings.Count(m.RenderFooter(100), "\n") + 1; got != want {
		t.Errorf("RenderFooter() emitted %d lines with a message, want %d", got, want)
	}
	if !strings.Contains(footerInfoLine(m, 100), "something to read") {
		t.Error("message is not on the info line (the last one)")
	}
}

// With no message posted, the view's own states keep showing and the footer
// stays two blank-looking lines.
func TestAuthStatesStillShowWithoutAFooterMessage(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	m = feed(t, m, AuthResultMsg{Error: errors.New("401 unauthorized")})

	if !strings.Contains(m.View(), "401 unauthorized") {
		t.Error("View() lost the authentication error")
	}
	footer := ansi.Strip(m.RenderFooter(100))
	if strings.TrimSpace(footer) != "" {
		t.Errorf("footer = %q, want no text when nothing was posted", footer)
	}
	if got := strings.Count(m.RenderFooter(100), "\n") + 1; got != m.GetFooterHeight() {
		t.Errorf("RenderFooter() emitted %d lines, GetFooterHeight() = %d", got, m.GetFooterHeight())
	}
}
