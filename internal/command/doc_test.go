package command

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The command vocabulary is already held by two tests that pit the parser
// against completion (TestEverythingThatParsesCanBeCompleted and its
// symmetric counterpart). They are missing the third edge: what the
// documentation claims. These two tests close that gap, on the model of
// internal/ui/keymap, which opposes a declared vocabulary to the sources
// the same way.
//
// This is not theoretical. `.claude/CLAUDE.md` advertised "`containers` or `c`"
// since the initial commit, while `c` has always resolved to `context` —
// D16 was exactly this defect in the other direction (`:netdiag` documented,
// refused by the parser), and it had been found by a test. This one
// survived D16/D17 because the two tests back then only looked at the
// code.

// docPath is the path, from this package, of the file that carries the list.
//
// It used to be `.claude/CLAUDE.md` until the architecture notes moved into
// `docs/architecture/`. The test failed telling us to repoint it, which is
// exactly what it was supposed to do: the file read is a piece of test
// data, not a property of the parser.
func docPath() string {
	return filepath.Join("..", "..", "docs", "architecture", "app-shell.md")
}

// docListIntro is the line that opens the list. The bullets that follow,
// up to the first blank line, are the documented commands.
const docListIntro = "Press `ctrl+p` to enter command mode, then type:"

var backticked = regexp.MustCompile("`([^`]+)`")

// documentedCommand is one bullet of the list: every spelling it claims,
// and the whole line so the failure message is readable.
type documentedCommand struct {
	spellings []string
	line      string
}

// readDocumentedCommands reads the bullets of the list in CLAUDE.md.
func readDocumentedCommands(t *testing.T) []documentedCommand {
	t.Helper()

	raw, err := os.ReadFile(docPath())
	if err != nil {
		t.Fatalf("cannot read %s: %v", docPath(), err)
	}

	lines := strings.Split(string(raw), "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == docListIntro {
			start = i + 1
			break
		}
	}
	if start == -1 {
		t.Fatalf("%s no longer contains the command list intro %q — the tests that read it have to be pointed at wherever it moved", docPath(), docListIntro)
	}

	var out []documentedCommand
	for _, line := range lines[start:] {
		if strings.TrimSpace(line) == "" {
			break
		}
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		var spellings []string
		for _, m := range backticked.FindAllStringSubmatch(line, -1) {
			spellings = append(spellings, m[1])
		}
		if len(spellings) > 0 {
			out = append(out, documentedCommand{spellings: spellings, line: strings.TrimSpace(line)})
		}
	}

	if len(out) == 0 {
		t.Fatalf("%s: the command list under %q is empty", docPath(), docListIntro)
	}
	return out
}

// bare strips the example argument from a documented spelling: `context
// <name>` and `context list` both name the `context` command.
func bare(spelling string) string {
	head, _, _ := strings.Cut(spelling, " ")
	return head
}

// TestEveryDocumentedCommandParsesToWhatItClaims pits CLAUDE.md against the
// parser: every advertised spelling must be accepted, and all the spellings
// under the same bullet must lead to the same place — it is the bullet that
// claims they are synonyms.
func TestEveryDocumentedCommandParsesToWhatItClaims(t *testing.T) {
	for _, doc := range readDocumentedCommands(t) {
		first := ParseCommand(bare(doc.spellings[0]))
		if first.Type == CommandUnknown {
			t.Errorf("%s\n  %q is documented but the parser refuses it", doc.line, doc.spellings[0])
			continue
		}

		for _, spelling := range doc.spellings[1:] {
			got := ParseCommand(bare(spelling))
			if got.Type == CommandUnknown {
				t.Errorf("%s\n  %q is documented but the parser refuses it", doc.line, spelling)
				continue
			}
			if got.Type != first.Type || got.View != first.View {
				t.Errorf("%s\n  %q resolves to %s/%s, %q to %s/%s — the line says they are the same command",
					doc.line, doc.spellings[0], first.Type, first.View, spelling, got.Type, got.View)
			}
		}
	}
}

// TestEveryTypeableViewIsDocumented closes the reverse direction, D16's: a
// view that can be reached from the keyboard and that the documentation
// does not name is a view nobody will find.
//
// Only full names are required. Aliases are an editorial choice — the list
// shows one or two per view and does not have to carry them all — whereas a
// missing full name is a view absent from the documentation.
func TestEveryTypeableViewIsDocumented(t *testing.T) {
	documented := map[string]bool{}
	for _, doc := range readDocumentedCommands(t) {
		for _, spelling := range doc.spellings {
			documented[bare(spelling)] = true
		}
	}

	for _, name := range ViewNames() {
		if !documented[name] {
			t.Errorf("view %q can be typed but %s does not list it", name, docPath())
		}
	}
}
