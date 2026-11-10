package viewer

import (
	"strings"
	"testing"
)

// Order is the one thing a JSON viewer must not get wrong. Unmarshalling into a
// map would answer this test alphabetically, which is a different document to
// the one on disk.
func TestAJSONObjectKeepsTheOrderTheFileGaveIt(t *testing.T) {
	doc := Open("k.json", KindAuto, []byte(`{"zebra":1,"apple":2,"mango":3}`))
	if doc.Kind != KindJSON {
		t.Fatalf("Kind = %q, want json (ParseErr=%v)", doc.Kind, doc.ParseErr)
	}

	var keys []string
	for _, child := range doc.Root.Children {
		keys = append(keys, child.Key)
	}
	want := []string{"zebra", "apple", "mango"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Errorf("keys = %v, want %v — the file's order, not a sorted one", keys, want)
	}
}

func TestAJSONArrayIndexesItsChildren(t *testing.T) {
	doc := Open("a.json", KindAuto, []byte(`[10,20]`))
	if doc.Root.Kind != NodeArray {
		t.Fatalf("root kind = %q, want array", doc.Root.Kind)
	}
	if got := doc.Root.Children[1].Key; got != "[1]" {
		t.Errorf("second child key = %q, want [1]", got)
	}
	if got := doc.Root.Value; got != "[2]" {
		t.Errorf("collapsed array reads %q, want [2] — a count is what a closed container has to say", got)
	}
}

// A value that will not parse is not a reason to refuse the file: it is exactly
// the file the user needs to look at.
func TestAMalformedJSONOpensAsTextAndSaysSo(t *testing.T) {
	doc := Open("broken.json", KindAuto, []byte(`{"a": 1,}`))

	if doc.Kind != KindPlain {
		t.Errorf("Kind = %q, want plain — a broken document still displays", doc.Kind)
	}
	if doc.ParseErr == nil {
		t.Error("ParseErr is nil; the name declared JSON, so the failure is worth reporting")
	}
	if !strings.Contains(doc.Text, `"a": 1`) {
		t.Errorf("Text = %q, want the original content", doc.Text)
	}
}

// The other half of the same rule: nothing *claimed* this was structured, so a
// failed guess must be silent. A file that merely opens with a tag is not a
// broken XML document.
//
// The example is a file with no extension at all, because that is the only thing
// still sniffed. `Dockerfile` used to serve here and no longer can: it is a
// declared kind now, recognised by its name.
func TestASniffedKindThatFailsToParseReportsNothing(t *testing.T) {
	doc := Open("dump", KindAuto, []byte("<not really xml"))

	if doc.Kind != KindPlain {
		t.Errorf("Kind = %q, want plain", doc.Kind)
	}
	if doc.ParseErr != nil {
		t.Errorf("ParseErr = %v, want nil — the content was guessed at, not declared", doc.ParseErr)
	}
}

// A producer that knows what it fetched outranks both the name and the content.
func TestADeclaredKindIsNotSecondGuessed(t *testing.T) {
	doc := Open("inspect", KindJSON, []byte(`{"Id":"abc"}`))
	if doc.Kind != KindJSON {
		t.Fatalf("Kind = %q, want json", doc.Kind)
	}

	// And a log stays a log even when its first line looks like JSON.
	logDoc := Open("logs", KindLog, []byte(`{"level":"warn","msg":"hi"}`))
	if logDoc.Kind != KindLog {
		t.Errorf("Kind = %q, want log — the producer declared it", logDoc.Kind)
	}
	if len(logDoc.Lines) != 1 || logDoc.Lines[0].Level != LevelWarn {
		t.Errorf("lines = %+v, want one warn line", logDoc.Lines)
	}
}

func TestTrailingContentIsNotAValidDocument(t *testing.T) {
	doc := Open("x.json", KindAuto, []byte(`{"a":1} and then some junk`))
	if doc.Kind != KindPlain {
		t.Errorf("Kind = %q, want plain — half a document is not a document", doc.Kind)
	}
}

func TestNormalizationStripsANSIAndCarriageReturns(t *testing.T) {
	raw := []byte("\x1b[31mred\x1b[0m\r\nsecond\rthird\n")
	got := Normalize(raw)
	want := "red\nsecondthird\n"
	if got != want {
		t.Errorf("Normalize = %q, want %q", got, want)
	}
}

func TestNodeCountCountsTheWholeTree(t *testing.T) {
	doc := Open("n.json", KindAuto, []byte(`{"a":{"b":1},"c":2}`))
	// root + a + a.b + c
	if got := doc.NodeCount(); got != 4 {
		t.Errorf("NodeCount = %d, want 4", got)
	}
}
