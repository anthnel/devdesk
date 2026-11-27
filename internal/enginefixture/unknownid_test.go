package enginefixture_test

import (
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-09a3 (DEVDESK-944), security finding B2: a 64-hex word that Redact was not
// told about (the id of a container of another run, a sentinel) must not reach a
// fixture. The recorder asks UnknownEngineIDs about the redacted exchange and ends
// the recording, under the rule RuleUnknownEngineID, on a count above zero. Redact
// itself leaves such a word alone (TestRedactReplacesWhatItKnowsEverywhere).
//
//	func UnknownEngineIDs(redacted Exchange, r Redaction) int
//	Redaction.Public []string // 64-hex ids the contract declares public: the pinned image digest, an image id

func TestUnknownEngineIDsCountsTheWordsNobodyAnnounced(t *testing.T) {
	stranger := sha256Hex("a container of another run")
	other := sha256Hex("the sentinel")
	known := map[string]string{rdID1: "c1"}
	placeholder := enginefixture.PlaceholderID("c1")
	tests := []struct {
		name string
		ex   enginefixture.Exchange
		r    enginefixture.Redaction
		want int
	}{
		{"nothing", enginefixture.Exchange{Argv: []string{"ps"}, Stdout: []byte("none\n")}, enginefixture.Redaction{}, 0},
		{"a stranger in stdout", enginefixture.Exchange{Stdout: []byte("id " + stranger + "\n")}, enginefixture.Redaction{}, 1},
		{"a stranger in stderr", enginefixture.Exchange{Stderr: []byte("no such " + stranger)}, enginefixture.Redaction{}, 1},
		{"a stranger in the argv", enginefixture.Exchange{Argv: []string{"inspect", stranger}}, enginefixture.Redaction{}, 1},
		{"two strangers", enginefixture.Exchange{Stdout: []byte(stranger + " " + other)}, enginefixture.Redaction{}, 2},
		{"the same one twice", enginefixture.Exchange{Stdout: []byte(stranger + " " + stranger)}, enginefixture.Redaction{}, 2},
		{"after a colon, as a digest", enginefixture.Exchange{Stdout: []byte("sha256:" + stranger)}, enginefixture.Redaction{}, 1},
		{"upper case", enginefixture.Exchange{Stdout: []byte("ID " + stranger[:60] + "ABCD")}, enginefixture.Redaction{}, 1},
		{"the placeholder of a role Redact replaced", enginefixture.Exchange{Stdout: []byte(placeholder)}, enginefixture.Redaction{IDs: known}, 0},
		{"the placeholder of a role nobody announced", enginefixture.Exchange{Stdout: []byte(placeholder)}, enginefixture.Redaction{}, 1},
		{"a declared public id", enginefixture.Exchange{Stdout: []byte(stranger)}, enginefixture.Redaction{Public: []string{stranger}}, 0},
		{"a declared public id, in another case", enginefixture.Exchange{Stdout: []byte(stranger)}, enginefixture.Redaction{Public: []string{upper(stranger)}}, 0},
		{"a public id and a stranger", enginefixture.Exchange{Stdout: []byte(stranger + " " + other)}, enginefixture.Redaction{Public: []string{stranger}}, 1},
		{"a word of 12 is the prefix of an id, not an id", enginefixture.Exchange{Stdout: []byte(stranger[:12])}, enginefixture.Redaction{}, 0},
		{"a word of 63", enginefixture.Exchange{Stdout: []byte(stranger[:63])}, enginefixture.Redaction{}, 0},
		{"a word of 65 is not an id", enginefixture.Exchange{Stdout: []byte(stranger + "0")}, enginefixture.Redaction{}, 0},
		{"a sha512", enginefixture.Exchange{Stdout: []byte(stranger + stranger)}, enginefixture.Redaction{}, 0},
		{"a word cut by a non-hex character", enginefixture.Exchange{Stdout: []byte(stranger[:32] + "-" + stranger[32:])}, enginefixture.Redaction{}, 0},
		{"inside a binary stream", enginefixture.Exchange{Stdout: []byte("\xff\x00" + stranger + "\x00")}, enginefixture.Redaction{}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := enginefixture.UnknownEngineIDs(tt.ex, tt.r); got != tt.want {
				t.Errorf("UnknownEngineIDs = %d, want %d", got, tt.want)
			}
		})
	}
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'a' <= c && c <= 'f' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

func TestRuleUnknownEngineIDIsNamed(t *testing.T) {
	if enginefixture.RuleUnknownEngineID != "unknown-engine-id" {
		t.Errorf("RuleUnknownEngineID = %q, want unknown-engine-id", enginefixture.RuleUnknownEngineID)
	}
}
