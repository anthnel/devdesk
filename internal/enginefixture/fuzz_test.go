package enginefixture_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// nameFor builds the file name a text's own headers ask for, so a mutated
// header does not fail on the name check before the rest of Parse is reached.
// The scenario part is taken from the seed name.
func nameFor(data []byte, seedName string) string {
	scenario := "x"
	if i := strings.Index(seedName, "--"); i > 0 {
		scenario = seedName[:i]
	}
	get := func(key string) string {
		for _, line := range strings.Split(string(data), "\n") {
			if rest, ok := strings.CutPrefix(line, "# "+key+": "); ok {
				return rest
			}
		}
		return ""
	}
	return scenario + "--" + get("tool") + "-" + get("version") + "-" + get("mode") + ".cli"
}

// FuzzParse holds the round trip of the plan for any input Parse accepts:
//
//   - Format(Parse(b)) == b: there is exactly one way to write a fixture;
//   - Parse(Format(f)) gives f back: Format never writes what Parse refuses.
//
// Seeds are the synthetic files of testdata and the pending sample; `mise run
// test` runs only the seeds. To explore:
//
//	go test -fuzz=FuzzParse -fuzztime=60s ./internal/enginefixture
func FuzzParse(f *testing.F) {
	for _, name := range syntheticNames(f) {
		f.Add(syntheticFile(f, name), name)
	}
	f.Add([]byte(pendingText), pendingName)
	f.Add([]byte(goodText()), goodName)

	f.Fuzz(func(t *testing.T, data []byte, seedName string) {
		for _, name := range []string{seedName, nameFor(data, seedName)} {
			fx, err := enginefixture.Parse("fuzz", name, data)
			if err != nil {
				continue
			}
			out := enginefixture.Format(fx)
			if !bytes.Equal(out, data) {
				t.Fatalf("Format(Parse(b)) != b\n  in: %q\n out: %q", data, out)
			}
			again, err := enginefixture.Parse("fuzz", name, out)
			if err != nil {
				t.Fatalf("Parse(Format(f)) failed: %v\n%q", err, out)
			}
			if !sameFixture(again, fx) {
				t.Fatalf("Parse(Format(f)) != f\n  got %+v\n want %+v", again, fx)
			}
		}
	})
}

// A fixture built in memory, with any bytes in its streams and any argv words,
// is written so that Parse reads exactly it back.
func FuzzFormatThenParse(f *testing.F) {
	f.Add([]byte("ok\n"), []byte(""), "ps", 0)
	f.Add([]byte{0xff, 0x00, '\r'}, []byte("--- stderr\n"), "rm", 125)
	f.Add([]byte("a\r\n"), []byte("\x1b[31m"), "inspect", 255)

	f.Fuzz(func(t *testing.T, stdout, stderr []byte, word string, exit int) {
		// Outside what a process returns, or an argv word the format refuses by
		// rule (invalid UTF-8, CR, LF) or that the plan leaves open (an empty word),
		// there is nothing to hold.
		if exit < 0 || exit > 255 || word == "" || strings.ContainsAny(word, "\r\n") || !validUTF8(word) {
			t.Skip()
		}
		fx := fixtureWith(stdout, stderr)
		fx.Exchange.Argv = []string{word, "--x"}
		fx.Exchange.Exit = exit

		out := enginefixture.Format(fx)
		back, err := enginefixture.Parse("fuzz", fixtureName, out)
		if err != nil {
			t.Fatalf("Parse(Format(f)): %v\n%q", err, out)
		}
		if !sameExchange(back.Exchange, fx.Exchange) {
			t.Fatalf("exchange changed on the way:\n got %+v\nwant %+v", back.Exchange, fx.Exchange)
		}
		if !bytes.Equal(enginefixture.Format(back), out) {
			t.Fatal("Format is not stable")
		}
	})
}

func validUTF8(s string) bool { return strings.ToValidUTF8(s, "") == s }
