package enginefixture

import "strconv"

// anonymize replaces, in out, the ids that no resource of the run owns by the placeholders
// PlaceholderID("anon-1"), ("anon-2"), ... in the order they appear: the words of the argv, then
// stdout, then stderr. It reports whether it replaced anything. The ids are the hexadecimal words of
// 64 characters (a container, an image or an endpoint id) and of 12 (the short form `ps` and an
// error message print); a short word that starts a long one already met shares its placeholder and
// takes its first 12 characters. A word the run knows is never touched (knownIDWords). One beyond
// r.Anonymous distinct ids is left as it is, so UnknownEngineIDs still refuses it.
func anonymize(out *Exchange, r Redaction) bool {
	known := knownIDWords(r)
	slots := map[string]int{} // lower-case word -> number of its placeholder
	next := 0
	changed := false
	replace := func(b []byte) []byte {
		var res []byte
		last := 0
		for _, sp := range hexSpans(b) {
			size := sp.end - sp.start
			if size != idLength && size != shortIDLen {
				continue
			}
			word := string(asciiLower(b[sp.start:sp.end]))
			if known[word] {
				continue
			}
			n, ok := slots[word]
			if !ok {
				if n, ok = sharedSlot(slots, word); !ok {
					if next >= r.Anonymous {
						continue
					}
					next++
					n = next
				}
				slots[word] = n
			}
			placeholder := PlaceholderID("anon-" + strconv.Itoa(n))
			res = append(res, b[last:sp.start]...)
			res = append(res, placeholder[:size]...)
			last, changed = sp.end, true
		}
		if res == nil {
			return b
		}
		return append(res, b[last:]...)
	}
	for i, word := range out.Argv {
		out.Argv[i] = string(replace([]byte(word)))
	}
	out.Stdout = replace(out.Stdout)
	out.Stderr = replace(out.Stderr)
	return changed
}

// sharedSlot is the placeholder number of a word whose short or long form was already met.
func sharedSlot(slots map[string]int, word string) (int, bool) {
	if len(word) == idLength {
		n, ok := slots[word[:shortIDLen]]
		return n, ok
	}
	for long, n := range slots {
		if len(long) == idLength && long[:shortIDLen] == word {
			return n, true
		}
	}
	return 0, false
}

// knownIDWords are the words, lower case, that are not unknown ids: the placeholder of each role
// of r.IDs, the ids of r.Public and the placeholders of the anonymous slots r.Anonymous allows,
// each in its long form and in its first 12 characters.
func knownIDWords(r Redaction) map[string]bool {
	known := map[string]bool{}
	add := func(id string) {
		id = string(asciiLower([]byte(id)))
		known[id] = true
		if len(id) > shortIDLen {
			known[id[:shortIDLen]] = true
		}
	}
	for _, role := range r.IDs {
		add(PlaceholderID(role))
	}
	for _, id := range r.Public {
		add(id)
	}
	for n := 1; n <= r.Anonymous; n++ {
		add(PlaceholderID("anon-" + strconv.Itoa(n)))
	}
	return known
}

type hexSpan struct{ start, end int }

// hexSpans lists the maximal runs of hexadecimal digits of b.
func hexSpans(b []byte) []hexSpan {
	var spans []hexSpan
	start := -1
	for i := 0; i <= len(b); i++ {
		if i < len(b) && isHexDigit(b[i]) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			spans = append(spans, hexSpan{start, i})
			start = -1
		}
	}
	return spans
}
