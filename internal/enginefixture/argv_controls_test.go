package enginefixture_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// An argv word may hold DEL, a C1 control or ESC. encoding/json writes DEL and the
// C1 controls raw, so Format escapes them: the header never holds a raw control
// character, and the round trip stays exact. Parse refuses the raw writing.
func TestArgvControlCharactersAreWrittenEscaped(t *testing.T) {
	tests := []struct {
		name string
		word string
		raw  string // what must not appear in the file
		want string // what appears instead
	}{
		{"DEL", "a\x7fb", "\x7f", `\u007f`},
		{"NEL, U+0085", "a\u0085b", "\u0085", `\u0085`},
		{"CSI, U+009B", "a\u009bb", "\u009b", `\u009b`},
		{"the last C1 control, U+009F", "a\u009fb", "\u009f", `\u009f`},
		{"ESC", "a\x1bb", "\x1b", `\u001b`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := fixtureWith([]byte("o\n"), nil)
			f.Exchange.Argv = []string{"run", tt.word}

			out := enginefixture.Format(f)

			if bytes.Contains(out, []byte(tt.raw)) {
				t.Errorf("Format wrote the control character raw:\n%q", out)
			}
			if !bytes.Contains(out, []byte(tt.want)) {
				t.Errorf("Format did not write %s:\n%q", tt.want, out)
			}
			back, err := enginefixture.Parse("c", fixtureName, out)
			if err != nil {
				t.Fatalf("Parse(Format(f)): %v", err)
			}
			if !slices.Equal(back.Exchange.Argv, f.Exchange.Argv) {
				t.Errorf("argv came back as %q, want %q", back.Exchange.Argv, f.Exchange.Argv)
			}
			if !bytes.Equal(enginefixture.Format(back), out) {
				t.Error("Format(Parse(Format(f))) != Format(f)")
			}

			rawText := strings.Replace(string(out), tt.want, tt.raw, 1)
			if _, err := enginefixture.Parse("c", fixtureName, []byte(rawText)); err == nil {
				t.Errorf("Parse accepted a raw control character in the argv header:\n%q", rawText)
			}
		})
	}
}
