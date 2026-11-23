package security

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/scan"
)

// DEVDESK-751: a scanner's text comes from the scanned content. No escape
// sequence and no control character of it may reach the terminal.

// hostile is a title-setting OSC, a colour CSI, a NUL, a C1 control and a
// right-to-left override around a visible word.
const hostile = "\x1b]0;PWNED\x07\x1b[31mword\x00\u009b\u202e!"

func TestNormalizeTextAndOneLineDropEscapeAndControlCharacters(t *testing.T) {
	for name, got := range map[string]string{
		"normalizeText": normalizeText("a "+hostile+"\nsecond\t\r"+hostile, false),
		"dropBlank":     dropBlankLines("a " + hostile + "\n\nsecond"),
		"oneLine":       oneLine("a "+hostile+"\nsecond", 80),
	} {
		t.Run(name, func(t *testing.T) {
			if strings.Contains(got, "PWNED") || !strings.Contains(got, "word") {
				t.Errorf("%q: the sequence must go whole, the visible word stay", got)
			}
			for _, r := range strings.ReplaceAll(got, "\n", "") {
				if unicode.IsControl(r) || r == '\u202e' {
					t.Errorf("%q still holds %U", got, r)
				}
			}
		})
	}
}

func TestNormalizeTextKeepsLineFeedsAndOrdinaryText(t *testing.T) {
	if got, want := normalizeText("a\tb\r\n\n\n  é 日本 👨‍👩‍👧  \nc", false), "a    b\n\n  é 日本 👨‍👩‍👧\nc"; got != want {
		t.Errorf("normalizeText = %q, want %q", got, want)
	}
}

// Every scanner-supplied field of the detail goes through it.
func TestNoScannerFieldReachesTheScreenWithAnEscapeSequence(t *testing.T) {
	f := scan.Finding{
		ID: "AVD-DS-0002" + hostile, Title: "Title " + hostile, Severity: scan.SeverityHigh, Source: scan.SourceTrivy,
		Message: "Message " + hostile, Description: "Description " + hostile, File: "dir/file" + hostile + ".yaml", Line: 3,
		Job: "job" + hostile, ScriptLine: "script " + hostile, Match: "match " + hostile,
		Resolution: "Resolution " + hostile, PkgName: "pkg" + hostile, Version: "1.0" + hostile,
		References: []string{"https://example.com/" + hostile},
	}
	m := openDetail(t, f, 140, 60)
	raw := m.detailsViewport.GetContent()

	if strings.Contains(raw, "\x1b]") {
		t.Errorf("an operating-system command reached the screen:\n%q", raw)
	}
	plain := ansi.Strip(raw)
	if strings.Contains(plain, "PWNED") {
		t.Errorf("the payload of a sequence is visible text:\n%s", plain)
	}
	for _, r := range strings.ReplaceAll(plain, "\n", "") {
		if unicode.IsControl(r) || r == '\u202e' {
			t.Fatalf("the screen holds %U:\n%q", r, plain)
		}
	}
	if !strings.Contains(plain, "word") {
		t.Errorf("the visible text of the fields is gone:\n%s", plain)
	}
}
