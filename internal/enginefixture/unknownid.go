package enginefixture

import "strings"

// RuleUnknownEngineID is the name of the rule UnknownEngineIDs stands for. It is not
// one of CheckLeaksAs: a 64-hex word is a public digest in one fixture and the id of
// the recording machine's container in another, and only the recorder knows which
// ids a run made.
const RuleUnknownEngineID = "unknown-engine-id"

// idLength is the length of a full container, image or network id.
const idLength = 64

// UnknownEngineIDs counts the words of 64 hexadecimal characters that a redacted
// exchange still holds and that are neither the placeholder of a role of r.IDs (what
// Redact put there) nor one of r.Public, the ids the contract declares public (the
// digest of the pinned image, an image id). Such a word is an id Redact was not told
// about: a container of another run, a sentinel, a stranger's, so it must not reach
// a fixture. The recorder refuses the exchange on a count above zero.
//
// A scenario that declared Redaction.Anonymous also has its unknown words of 12 counted (the
// short form of an id), which Redact replaced up to that number; the placeholders it put there
// are known. Limits: only whole words of exactly 64 characters count (a longer hexadecimal
// word, a sha512, is not an engine id), and a 64-hex word that is not an id of the
// engine (a hash a container prints) is reported as well: declare it in r.Public.
func UnknownEngineIDs(redacted Exchange, r Redaction) int {
	known := knownIDWords(r)
	count := func(text string) int {
		n := 0
		for _, word := range hexWords(text) {
			// A word of 12 is an id only for a scenario that declared it accepts unknown ids.
			if (len(word) == idLength || len(word) == shortIDLen && r.Anonymous > 0) && !known[strings.ToLower(word)] {
				n++
			}
		}
		return n
	}
	total := count(string(redacted.Stdout)) + count(string(redacted.Stderr))
	for _, word := range redacted.Argv {
		total += count(word)
	}
	return total
}

// hexWords lists the maximal runs of hexadecimal digits of text.
func hexWords(text string) []string {
	var words []string
	start := -1
	for i := 0; i <= len(text); i++ {
		if i < len(text) && isHexDigit(text[i]) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			words = append(words, text[start:i])
			start = -1
		}
	}
	return words
}
