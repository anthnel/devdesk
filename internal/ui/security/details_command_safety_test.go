package security

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-751: Y copies a command the user pastes into a shell, built from what
// a scan reported. The Run line and Y exist only for a command with nothing a
// shell or a terminal would read as more than a word; anything else is shown as
// text under Do, cleaned, and cannot be copied.

// untrustedCommands are FixCommands that must never be offered under Run.
var untrustedCommands = map[string]string{
	"padded then chained":  "npm install lodash@4.17.21" + strings.Repeat(" ", 200) + "; rm -rf /home",
	"chained":              "npm install lodash@4.17.21; id",
	"and":                  "npm install a && id",
	"pipe":                 "npm install a | sh",
	"background":           "npm install a &",
	"substitution":         "npm install $(id)",
	"backtick":             "npm install `id`",
	"redirect out":         "npm install a > /etc/x",
	"redirect in":          "npm install a < /etc/x",
	"subshell":             "(npm install a)",
	"backslash":            `npm install a\ b`,
	"single quote":         "npm install 'a'",
	"double quote":         `npm install "a"`,
	"brace":                "npm install {a,b}",
	"glob star":            "npm install *",
	"glob question":        "npm install a?",
	"history":              "npm install a!",
	"comment":              "npm install a # b",
	"home directory":       "npm install ~/x",
	"bracket glob":         "npm install a[1]",
	"caret at word head":   "npm install ^a",
	"percent at word head": "npm install %1",
	"equal at word head":   "npm install =a",
	"escape sequence":      "npm install a\x1b[2K\x1b]0;title\x07rm",
	"escape at the end":    "npm install a\x1b",
	"carriage return":      "npm install a\rrm -rf /home",
	"tab inside":           "npm install\ta",
	"vertical tab":         "npm install\va",
	"form feed":            "npm install\fa",
	"no-break space":       "npm install\u00a0a",
	"line separator":       "npm install a\u2028rm",
	"right-to-left switch": "npm install a\u202erm",
	"zero-width space":     "npm install a\u200brm",
	"C1 control":           "npm install a\u009brm",
	"nul":                  "npm install a\x00rm",
	"two lines":            "npm install a\nrm -rf /home",
}

func TestRunCommandRefusesWhatAShellOrATerminalReadsAsMoreThanWords(t *testing.T) {
	for name, command := range untrustedCommands {
		t.Run(name, func(t *testing.T) {
			f := scan.Finding{PkgName: "lodash", FixCommand: command}
			if got, ok := runCommand(f); ok {
				t.Errorf("runCommand(%q) = %q, want it refused", command, got)
			}
		})
	}
}

func TestRunCommandStillAcceptsEveryCommandFixCommandBuilds(t *testing.T) {
	for _, eco := range []string{"gomod", "npm", "yarn", "pnpm", "pip", "poetry", "cargo", "alpine", "debian"} {
		command, ok := scan.FixCommand(eco, "@scope/p-1.x", "1:2.3.4-r0+deb12u1~rc1")
		if !ok {
			t.Fatalf("FixCommand(%q) builds nothing", eco)
		}
		if got, ok := runCommand(scan.Finding{PkgName: "@scope/p-1.x", FixCommand: command}); !ok || got != command {
			t.Errorf("runCommand(%q) = (%q, %v), want the command", command, got, ok)
		}
	}
}

// A refused command is not offered: no Run line, Y greyed and refusing with its
// reason, and nothing reaches the clipboard.
func TestARefusedCommandIsNeitherShownUnderRunNorCopied(t *testing.T) {
	for name, command := range untrustedCommands {
		t.Run(name, func(t *testing.T) {
			testutil.FastTimers(t, &components.FooterMsgDuration)
			written := stubClipboard(t, nil)
			m := openDetail(t, commandFinding(command), 120, 30)

			for _, line := range plainLines(m) {
				if strings.HasPrefix(strings.TrimSpace(line), "Run:") {
					t.Errorf("a Run line is shown for a refused command: %q", line)
				}
			}
			if a := m.canCopyFixCommand(); a.Enabled() || a.Reason != reasonNoCommand {
				t.Errorf("canCopyFixCommand() = %+v, want %q", a, reasonNoCommand)
			}
			if !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Copy) {
				t.Error("Y is not greyed for a refused command")
			}

			m, cmd := step(t, m, testutil.Key(keymap.Copy))
			testutil.Msgs(cmd)
			if len(*written) != 0 {
				t.Errorf("clipboard = %q, want nothing written", *written)
			}
			if m.footer.Text() != reasonNoCommand || m.footer.Level() != components.LevelWarning {
				t.Errorf("footer = %q (level %v), want %q as a warning", m.footer.Text(), m.footer.Level(), reasonNoCommand)
			}
		})
	}
}

// The guidance does not disappear with the Run line: the refused text is under
// Do, and no control character of it reaches the screen.
func TestARefusedCommandIsShownAsTextUnderDoWithoutControlCharacters(t *testing.T) {
	for name, command := range untrustedCommands {
		t.Run(name, func(t *testing.T) {
			m := openDetail(t, commandFinding(command), 120, 40)
			content := ansi.Strip(m.detailsViewport.GetContent())

			if !strings.Contains(content, "Do:") || !strings.Contains(content, "npm install") {
				t.Errorf("the refused instruction is not under Do:\n%s", content)
			}
			for _, r := range strings.ReplaceAll(content, "\n", "") {
				if unicode.IsControl(r) || r == '\u202e' || r == '\u200b' || r == '\u2028' {
					t.Fatalf("the screen holds the control character %U:\n%q", r, content)
				}
			}
			if strings.Contains(m.detailsViewport.GetContent(), "\x1b]0;") {
				t.Error("an operating-system command of the scan reached the screen")
			}
		})
	}
}

func TestAMultiLineCommandKeepsItsLinesUnderDo(t *testing.T) {
	m := openDetail(t, commandFinding("first step\nsecond step"), 120, 30)
	content := ansi.Strip(m.detailsViewport.GetContent())
	if !strings.Contains(content, "first step") || !strings.Contains(content, "second step") {
		t.Errorf("a multi-line instruction lost a line:\n%s", content)
	}
}

func TestFixStepsKeepsARefusedCommandAndNothingElse(t *testing.T) {
	refused := scan.Finding{PkgName: "x", FixCommand: "npm install x; id"}
	if got := fixSteps(refused); len(got) != 1 || got[0] != "npm install x; id" {
		t.Errorf("fixSteps(refused) = %q, want the text as one step", got)
	}
	ok := scan.Finding{PkgName: "x", FixCommand: "npm install x@1.2.3"}
	if got := fixSteps(ok); len(got) != 0 {
		t.Errorf("fixSteps(a command under Run) = %q, want none", got)
	}
	if got := fixSteps(scan.Finding{PkgName: "x", FixCommand: " \t "}); len(got) != 0 {
		t.Errorf("fixSteps(blank) = %q, want none", got)
	}
}
