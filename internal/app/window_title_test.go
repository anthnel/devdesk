package app

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
)

// windowTitleCase is one row of the table; it doubles as a fuzz seed.
type windowTitleCase struct {
	name    string
	view    command.ViewType
	context string
	want    string // exact expectation; empty when only the invariants apply
}

var windowTitleCases = []windowTitleCase{
	{"dashboard and prod", command.ViewDashboard, "prod", "dk · dashboard · prod"},
	{"canonical name, not alias", command.ViewSecurity, "prod", "dk · security · prod"},
	{"default context", command.ViewDashboard, "default", "dk · dashboard · default"},
	{"empty context falls back to default", command.ViewDashboard, "", "dk · dashboard · default"},
	{"blank context is empty", command.ViewDashboard, "   ", "dk · dashboard · default"},
	{"empty view has no orphan separator", "", "prod", "dk · prod"},
	{"nothing at all is never empty", "", "", "dk · default"},
	{"escape sequence stripped", command.ViewDashboard, "a\x1b]2;pwn\x07b", ""},
	{"control characters stripped", command.ViewDashboard, "x\x00\x7f\u0085\r\ny", ""},
	{"control characters in the view", command.ViewType("da\x07sh\x1b[31m"), "prod", ""},
	{"long ASCII context", command.ViewDashboard, strings.Repeat("a", 200), ""},
	{"bidi override only", command.ViewDashboard, "\u202e", "dk · dashboard · default"},
	{"bidi isolate stripped", command.ViewDashboard, "a\u2066b", "dk · dashboard · ab"},
	{"zero width space only", command.ViewDashboard, "\u200b", "dk · dashboard · default"},
	{"line separator only", command.ViewDashboard, "\u2028", "dk · dashboard · default"},
	{"paragraph separator and BOM", command.ViewDashboard, "x\u2029\ufeffy", "dk · dashboard · xy"},
	{"wide runes", command.ViewDashboard, strings.Repeat("漢", 40), ""},
}

// checkWindowTitleInvariants holds for every input, table or fuzzed.
func checkWindowTitleInvariants(t *testing.T, view, ctx, got string) {
	t.Helper()
	if got == "" {
		t.Errorf("windowTitle(%q, %q) is empty", view, ctx)
	}
	if !strings.HasPrefix(got, "dk") {
		t.Errorf("windowTitle(%q, %q) = %q, want prefix %q", view, ctx, got, "dk")
	}
	if w := ansi.StringWidth(got); w > maxWindowTitleWidth {
		t.Errorf("windowTitle(%q, %q) is %d cells wide, max %d: %q", view, ctx, w, maxWindowTitleWidth, got)
	}
	if !utf8.ValidString(got) {
		t.Errorf("windowTitle(%q, %q) is not valid UTF-8: %q", view, ctx, got)
	}
	for _, r := range got {
		if unicode.IsControl(r) {
			t.Errorf("windowTitle(%q, %q) contains control rune %U: %q", view, ctx, r, got)
			break
		}
	}
	if strings.Contains(got, "\x1b") {
		t.Errorf("windowTitle(%q, %q) contains ESC: %q", view, ctx, got)
	}
	for _, r := range got {
		if unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			t.Errorf("windowTitle(%q, %q) contains format or separator rune %U: %q", view, ctx, r, got)
			break
		}
	}
	if strings.HasSuffix(got, " ") {
		t.Errorf("windowTitle(%q, %q) ends with a space: %q", view, ctx, got)
	}
	// A segment may itself contain the separator, so the empty-segment check
	// only holds when the inputs do not; assembly alone must never produce one.
	if !strings.Contains(view, "·") && !strings.Contains(ctx, "·") {
		segs := strings.Split(strings.TrimSuffix(got, windowTitleTail), windowTitleSeparator)
		if strings.HasSuffix(got, windowTitleTail) {
			// The cut may land right after a space, in the last segment only.
			segs = segs[:len(segs)-1]
		}
		for _, seg := range segs {
			if seg == "" || seg != strings.TrimSpace(seg) {
				t.Errorf("windowTitle(%q, %q) has an empty or padded segment: %q", view, ctx, got)
				break
			}
		}
	}
}

func TestWindowTitle(t *testing.T) {
	for _, tc := range windowTitleCases {
		t.Run(tc.name, func(t *testing.T) {
			got := windowTitle(tc.view, tc.context)
			checkWindowTitleInvariants(t, string(tc.view), tc.context, got)
			if tc.want != "" && got != tc.want {
				t.Errorf("windowTitle(%q, %q) = %q, want %q", tc.view, tc.context, got, tc.want)
			}
		})
	}
}

func TestWindowTitleInjectionLeavesNoSequence(t *testing.T) {
	got := windowTitle(command.ViewDashboard, "a\x1b]2;pwn\x07b")
	if strings.ContainsAny(got, "\x1b\x07") {
		t.Fatalf("title %q still carries an escape or BEL", got)
	}
	if strings.Contains(got, "pwn") {
		t.Fatalf("title %q kept the payload of the stripped sequence", got)
	}
	if want := "dk · dashboard · ab"; got != want {
		t.Errorf("windowTitle = %q, want %q", got, want)
	}
}

func TestWindowTitleTruncatesOnTheRight(t *testing.T) {
	const prefix = "dk · dashboard · "
	for _, tc := range []struct{ name, ctx string }{
		{"ascii", strings.Repeat("a", 200)},
		{"wide", strings.Repeat("漢", 40)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := windowTitle(command.ViewDashboard, tc.ctx)
			if !strings.HasPrefix(got, prefix) {
				t.Errorf("title %q lost its prefix %q", got, prefix)
			}
			if !strings.HasSuffix(got, "…") {
				t.Errorf("title %q is not marked as truncated", got)
			}
			if w := ansi.StringWidth(got); w > maxWindowTitleWidth {
				t.Errorf("title is %d cells wide, max %d", w, maxWindowTitleWidth)
			}
		})
	}
}

func TestWindowTitleIsPure(t *testing.T) {
	a := windowTitle(command.ViewSecurity, "prod")
	b := windowTitle(command.ViewSecurity, "prod")
	if a != b {
		t.Errorf("same inputs gave %q then %q", a, b)
	}
}

func FuzzWindowTitle(f *testing.F) {
	for _, tc := range windowTitleCases {
		f.Add(string(tc.view), tc.context)
	}
	f.Add("\x1b", "\x1b\\")
	f.Add("\x9d2;x\x9c", " \u200b")
	f.Add(strings.Repeat("\x1b[", 100), strings.Repeat("\x07", 100))
	f.Fuzz(func(t *testing.T, view, ctx string) {
		got := windowTitle(command.ViewType(view), ctx)
		checkWindowTitleInvariants(t, view, ctx, got)
	})
}
