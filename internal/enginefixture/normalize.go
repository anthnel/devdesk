package enginefixture

import "regexp"

// The values that move with the clock in what an engine prints. The recorder fixes them for a
// scenario that declares it (Redaction.FixedTimestamps, FixedDurations); the Comparer of a contract
// (enginecontract.Tolerating) reads the same forms, so a fixture with fixed values is verified
// against a real output of another day.
var (
	// DurationPattern is what the engines print for a duration: a number and a unit, in the
	// singular or the plural, or one of their three phrases. Words, not abbreviations.
	DurationPattern = regexp.MustCompile(`\b(?:Less than a second|About an? (?:second|minute|hour|day|week|month|year)|\d+ (?:second|minute|hour|day|week|month|year)s?)\b`)
	// TimestampPattern is a date and a time of day, as RFC 3339 writes them or as Go's time.Time
	// prints them (a fraction, an offset, a zone name, a monotonic reading).
	TimestampPattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})` +
		`|\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)? [+-]\d{4}(?: (?:[A-Z]{2,5}|[+-]\d{2,4}))?(?: m=[+-]\d+(?:\.\d+)?)?`)
)

// The fixed values: a timestamp keeps its form (RFC 3339 or Go's), a duration becomes one that
// every engine could print.
const (
	fixedGoTimestamp  = "2026-01-01 00:00:00 +0000 UTC"
	fixedRFCTimestamp = "2026-01-01T00:00:00Z"
	fixedDuration     = "5 seconds"
)

// fixTimestamps replaces every timestamp of text by the fixed one of its form.
func fixTimestamps(text []byte) []byte {
	return TimestampPattern.ReplaceAllFunc(text, func(m []byte) []byte {
		if len(m) > 10 && m[10] == 'T' { // "2026-11-27T12:00:00Z": RFC 3339 has a T where Go's form has a blank
			return []byte(fixedRFCTimestamp)
		}
		return []byte(fixedGoTimestamp)
	})
}

// fixDurations replaces every duration of text by the fixed one.
func fixDurations(text []byte) []byte {
	return DurationPattern.ReplaceAll(text, []byte(fixedDuration))
}
