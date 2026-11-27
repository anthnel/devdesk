package enginecontract

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// Tolerance is a kind of value that moves with the clock and that a Comparer may
// leave out of the comparison: what an engine prints for how long ago something
// happened, for when it happened, or for the state of its healthcheck.
type Tolerance uint8

const (
	// Durations: "Up 3 seconds", "Created Less than a second ago", "Up About an hour".
	Durations Tolerance = 1 << iota
	// Timestamps: "2026-11-27 12:00:00 +0000 UTC" (the CreatedAt of a ps row or of a
	// network) and "2026-11-27T12:00:00.5Z" (RFC 3339).
	Timestamps
	// Health: the marker a status carries, "(healthy)", "(unhealthy)", "(starting)" or
	// "(health: starting)". A status with a marker and one without still differ. **It makes
	// "(healthy)" equal to "(unhealthy)": never use it when the health state is what a
	// contract is about** (ps-health does not). It is for an output where the health only
	// happens to be printed and has not settled.
	Health

	allTolerances = Durations | Timestamps | Health
)

var (
	// durationWord is what the engines print for a duration: a number and a unit, in the
	// singular or the plural, or one of their three phrases. Words, not abbreviations.
	durationWord = regexp.MustCompile(`\b(?:Less than a second|About an? (?:second|minute|hour|day|week|month|year)|\d+ (?:second|minute|hour|day|week|month|year)s?)\b`)
	// timestampWord is a date and a time of day, as RFC 3339 writes them or as Go's
	// time.Time prints them (a fraction, an offset, a zone name, a monotonic reading).
	timestampWord = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})` +
		`|\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)? [+-]\d{4}(?: (?:[A-Z]{2,5}|[+-]\d{2,4}))?(?: m=[+-]\d+(?:\.\d+)?)?`)
	// healthWord is the marker of a status, with its parentheses.
	healthWord = regexp.MustCompile(`\((?:health: )?(?:starting|healthy|unhealthy)\)`)
)

// Tolerating returns a Comparer that judges two exchanges equal when their streams
// are equal once every value of the tolerated kinds is replaced by one placeholder, on
// both sides. Nothing else is tolerated: an exit code in a status ("Exited (137)"), a
// number, a port, a name, the word "ago" and a kind not asked for all still count.
// With no argument the streams are compared exactly. The argv and the exit code are
// compared before any Comparer is asked (Session.Verify). It reads its inputs and
// changes nothing in them; the comparison is symmetric. A tolerance it does not know
// panics: it is a mistake of the contract, not of the engine.
func Tolerating(kinds ...Tolerance) Comparer {
	var set Tolerance
	for _, k := range kinds {
		if k == 0 || k&^allTolerances != 0 {
			panic(fmt.Sprintf("enginecontract.Tolerating: unknown tolerance %d", k))
		}
		set |= k
	}
	return func(recorded, got enginefixture.Exchange) error {
		var errs []error
		for _, stream := range []struct {
			name          string
			recorded, got []byte
		}{{"stdout", recorded.Stdout, got.Stdout}, {"stderr", recorded.Stderr, got.Stderr}} {
			want, have := normalized(stream.recorded, set), normalized(stream.got, set)
			if want != have {
				errs = append(errs, errors.New(describeStreams(stream.name, []byte(want), []byte(have))))
			}
		}
		return errors.Join(errs...)
	}
}

// SameUpToDurations is Tolerating(Durations): the streams are equal once every
// duration an engine prints is replaced by one placeholder.
func SameUpToDurations() Comparer { return Tolerating(Durations) }

// SameUpToDurationsAndHealth is Tolerating(Durations, Health).
func SameUpToDurationsAndHealth() Comparer { return Tolerating(Durations, Health) }

// normalized is stream with each tolerated kind replaced by its placeholder. The
// timestamps go first, so the numbers of a date are not read as the start of a duration.
func normalized(stream []byte, set Tolerance) string {
	text := string(stream)
	if set&Timestamps != 0 {
		text = timestampWord.ReplaceAllString(text, "<timestamp>")
	}
	if set&Durations != 0 {
		text = durationWord.ReplaceAllString(text, "<duration>")
	}
	if set&Health != 0 {
		text = healthWord.ReplaceAllString(text, "(<health>)")
	}
	return text
}
