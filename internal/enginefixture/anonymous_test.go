package enginefixture_test

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-09b2 (DEVDESK-807): the ids of an engine that no resource of the run owns. Docker's
// refusal of a port names the network endpoint it set up for a container that never ran
// (64 hexadecimal characters, new at every run) and `network rm` of a network in use names
// the endpoint of the container in 12; the contract knows neither. A scenario that has such
// a message declares how many it accepts:
//
//	Redaction.Anonymous int // the most distinct unknown ids this scenario accepts to replace
//
// Redact replaces them, in the order they appear (argv, stdout, stderr), by
// PlaceholderID("anon-1"), ("anon-2"), ...: the same word is the same placeholder, a word of
// 12 is replaced by the first 12 characters of the placeholder (a short id, as ps prints it),
// and a short word that starts a 64-hex one already seen shares its placeholder. A word the
// run knows (a role of IDs, a Public id, a placeholder) is never touched. Beyond the declared
// number a word is left alone and UnknownEngineIDs counts it, so the recording is refused as
// before: the limit is never widened silently. With Anonymous 0, nothing changes.
// UnknownEngineIDs also counts unknown words of 12, only when Anonymous is above 0.

var (
	anonA = sha256Hex("endpoint a")
	anonB = sha256Hex("endpoint b")
	anonC = sha256Hex("endpoint c")
)

func anon(n string) string { return wantPlaceholder("anon-" + n) }

func redactedText(t *testing.T, ex enginefixture.Exchange, r enginefixture.Redaction) enginefixture.Exchange {
	t.Helper()
	got, _ := enginefixture.Redact(ex, r)
	return got
}

func TestRedactReplacesTheAnonymousIDsInTheOrderTheyAppear(t *testing.T) {
	ex := enginefixture.Exchange{
		Argv:   []string{"inspect", anonB},
		Stdout: []byte("out " + anonA + "\n"),
		Stderr: []byte("err " + anonC + " " + anonA + "\n"),
	}

	got := redactedText(t, ex, enginefixture.Redaction{Anonymous: 3})

	if got.Argv[1] != anon("1") || string(got.Stdout) != "out "+anon("2")+"\n" || string(got.Stderr) != "err "+anon("3")+" "+anon("2")+"\n" {
		t.Errorf("redacted = %q %q %q, want anon-1 for the argv, anon-2 and anon-3 after it, the same word the same placeholder",
			got.Argv, got.Stdout, got.Stderr)
	}
	if n := enginefixture.UnknownEngineIDs(got, enginefixture.Redaction{Anonymous: 3}); n != 0 {
		t.Errorf("UnknownEngineIDs after the replacement = %d, want 0", n)
	}
}

// Two recordings of the same scenario give the same fixture whatever the ids were.
func TestRedactAnonymousIsDeterministic(t *testing.T) {
	msg := func(a, b string) enginefixture.Exchange {
		return enginefixture.Exchange{Argv: []string{"run"}, Stderr: []byte("endpoint x (" + a + "): " + b + " " + a)}
	}
	r := enginefixture.Redaction{Anonymous: 2}

	first, second := redactedText(t, msg(anonA, anonB), r), redactedText(t, msg(anonC, sha256Hex("another")), r)

	if string(first.Stderr) != string(second.Stderr) {
		t.Errorf("fixtures differ: %q and %q", first.Stderr, second.Stderr)
	}
}

func TestRedactAnonymousStaysWithinTheDeclaredNumber(t *testing.T) {
	ex := enginefixture.Exchange{Stderr: []byte(anonA + " " + anonB + " " + anonA)}
	r := enginefixture.Redaction{Anonymous: 1}

	got := redactedText(t, ex, r)

	if string(got.Stderr) != anon("1")+" "+anonB+" "+anon("1") {
		t.Errorf("stderr = %q, want the first word replaced, the second one left, the first replaced again", got.Stderr)
	}
	if n := enginefixture.UnknownEngineIDs(got, r); n != 1 {
		t.Errorf("UnknownEngineIDs = %d, want 1: the word over the limit is still refused", n)
	}
}

func TestRedactAnonymousNeverTouchesWhatTheRunKnows(t *testing.T) {
	public := sha256Hex("the pinned image")
	ex := enginefixture.Exchange{
		Argv:   []string{"rmi", rdID1},
		Stdout: []byte(public + " " + wantPlaceholder("c2") + " " + anon("1") + "\n"),
		Stderr: []byte(anonA),
	}
	r := enginefixture.Redaction{IDs: map[string]string{rdID1: "c1", rdID2: "c2"}, Public: []string{public}, Anonymous: 2}

	got := redactedText(t, ex, r)

	if got.Argv[1] != wantPlaceholder("c1") {
		t.Errorf("argv = %q, want the role of the id Redact was told about, not an anonymous one", got.Argv)
	}
	if string(got.Stdout) != public+" "+wantPlaceholder("c2")+" "+anon("1")+"\n" {
		t.Errorf("stdout = %q, want a public id, a role's placeholder and an anonymous placeholder left as they are", got.Stdout)
	}
	if string(got.Stderr) != anon("1") {
		t.Errorf("stderr = %q, want the unknown word to be anon-1 (the placeholders already there are known, and use no slot)", got.Stderr)
	}
}

func TestRedactAnonymousIsOffByDefault(t *testing.T) {
	ex := enginefixture.Exchange{Argv: []string{"x"}, Stderr: []byte(anonA + " " + anonA[:12])}

	got, header := enginefixture.Redact(ex, enginefixture.Redaction{})

	if string(got.Stderr) != string(ex.Stderr) || header != "none" {
		t.Errorf("stderr = %q, header = %q, want nothing replaced without a declared number", got.Stderr, header)
	}
}

func TestRedactAnonymousIsOneOfTheCategoriesOfTheHeader(t *testing.T) {
	_, header := enginefixture.Redact(enginefixture.Exchange{Stderr: []byte(anonA)}, enginefixture.Redaction{Anonymous: 1})

	if header != "ids" {
		t.Errorf("header = %q, want %q: the vocabulary of the header does not grow", header, "ids")
	}
}

func TestRedactAnonymousIsIdempotent(t *testing.T) {
	ex := enginefixture.Exchange{Argv: []string{"run"}, Stderr: []byte("(" + anonA + ") " + anonB[:12] + " " + anonB)}
	r := enginefixture.Redaction{Anonymous: 2}

	once, _ := enginefixture.Redact(ex, r)
	twice, _ := enginefixture.Redact(once, r)

	if string(once.Stderr) != string(twice.Stderr) {
		t.Errorf("a second pass changed %q into %q", once.Stderr, twice.Stderr)
	}
}

func TestRedactAnonymousHandlesUpperCaseAndBinaryStreams(t *testing.T) {
	upperA := upper(anonA)
	ex := enginefixture.Exchange{Stdout: []byte("\xff\x00" + upperA + "\x00" + anonA + "\xfe")}

	got := redactedText(t, ex, enginefixture.Redaction{Anonymous: 1})

	if want := "\xff\x00" + anon("1") + "\x00" + anon("1") + "\xfe"; string(got.Stdout) != want {
		t.Errorf("stdout = %q, want one placeholder for the word in either case, the bytes around it untouched", got.Stdout)
	}
}

func TestRedactAnonymousDoesNotChangeItsInput(t *testing.T) {
	ex := enginefixture.Exchange{Argv: []string{"x", anonA}, Stdout: []byte(anonA), Stderr: []byte(anonA)}

	redactedText(t, ex, enginefixture.Redaction{Anonymous: 1})

	if ex.Argv[1] != anonA || string(ex.Stdout) != anonA || string(ex.Stderr) != anonA {
		t.Errorf("the input changed: %q %q %q", ex.Argv, ex.Stdout, ex.Stderr)
	}
}

// ---- The short ids: the endpoint of `network rm` is 12 characters. ----

func TestRedactAnonymousReplacesAShortIDByThePrefixOfAPlaceholder(t *testing.T) {
	short := anonA[:12]
	ex := enginefixture.Exchange{Stderr: []byte(`active endpoints (name:"x" id:"` + short + `")`)}

	got := redactedText(t, ex, enginefixture.Redaction{Anonymous: 1})

	if want := `active endpoints (name:"x" id:"` + anon("1")[:12] + `")`; string(got.Stderr) != want {
		t.Errorf("stderr = %q, want %q", got.Stderr, want)
	}
}

func TestRedactAnonymousGivesAShortIDAndItsLongFormOnePlaceholder(t *testing.T) {
	r := enginefixture.Redaction{Anonymous: 1}

	for name, in := range map[string]string{
		"the long form first":  anonA + " " + anonA[:12],
		"the short form first": anonA[:12] + " " + anonA,
	} {
		t.Run(name, func(t *testing.T) {
			got := redactedText(t, enginefixture.Exchange{Stderr: []byte(in)}, r)

			parts := strings.Fields(string(got.Stderr))
			if len(parts) != 2 || !strings.HasPrefix(anon("1"), parts[0][:12]) || !strings.HasPrefix(anon("1"), parts[1][:12]) {
				t.Errorf("stderr = %q, want both forms to come from anon-1", got.Stderr)
			}
			if n := enginefixture.UnknownEngineIDs(got, r); n != 0 {
				t.Errorf("UnknownEngineIDs = %d, want 0", n)
			}
		})
	}
}

func TestRedactAnonymousShortIDsDrawOnTheSameNumber(t *testing.T) {
	ex := enginefixture.Exchange{Stderr: []byte(anonA[:12] + " " + anonB[:12])}
	r := enginefixture.Redaction{Anonymous: 1}

	got := redactedText(t, ex, r)

	if string(got.Stderr) != anon("1")[:12]+" "+anonB[:12] {
		t.Errorf("stderr = %q, want the second short id left alone", got.Stderr)
	}
	if n := enginefixture.UnknownEngineIDs(got, r); n != 1 {
		t.Errorf("UnknownEngineIDs = %d, want 1: a short id over the limit is refused when the scenario declared a limit", n)
	}
}

func TestShortWordsAreNotCountedWithoutADeclaredNumber(t *testing.T) {
	ex := enginefixture.Exchange{Stdout: []byte(anonA[:12] + " " + anonA)}

	if n := enginefixture.UnknownEngineIDs(ex, enginefixture.Redaction{}); n != 1 {
		t.Errorf("UnknownEngineIDs = %d, want 1: only the 64-hex word counts, as before", n)
	}
}

func TestShortWordsOfKnownIDsAreNotUnknown(t *testing.T) {
	public := sha256Hex("an image")
	ex := enginefixture.Exchange{Stdout: []byte(public[:12] + " " + wantPlaceholder("c1")[:12])}
	r := enginefixture.Redaction{IDs: map[string]string{rdID1: "c1"}, Public: []string{public}, Anonymous: 1}

	got := redactedText(t, ex, r)

	if string(got.Stdout) != string(ex.Stdout) || enginefixture.UnknownEngineIDs(got, r) != 0 {
		t.Errorf("stdout = %q (unknown %d), want the short forms of a public id and of a role left alone", got.Stdout, enginefixture.UnknownEngineIDs(got, r))
	}
}
