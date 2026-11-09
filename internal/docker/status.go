package docker

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// StatusFacts is what a container's Status column states, as values. A fact
// that the text does not state stays unset (Has* false): an unknown exit code
// is never reported as 0, which would read as a clean exit.
type StatusFacts struct {
	ExitCode    int
	HasExitCode bool
	// Age is how long the container has been in its current state: up for
	// "Up X", gone for "Exited (n) X ago".
	Age    time.Duration
	HasAge bool
}

var (
	statusRE = regexp.MustCompile(`^(?:(Up) |(Exited|Restarting) \((-?\d+)\) )(.+)$`)
	// agePhraseRE matches the duration phrase once "ago" and any trailing
	// "(healthy)" / "(Paused)" are removed.
	agePhraseRE = regexp.MustCompile(`^(?:(\d+) (second|minute|hour|day|week|month|year)s?|(Less than a second)|About an? (minute|hour))$`)
)

// unitDuration follows docker's humanize: a month is 30 days, a year 365.
var unitDuration = map[string]time.Duration{
	"second": time.Second,
	"minute": time.Minute,
	"hour":   time.Hour,
	"day":    24 * time.Hour,
	"week":   7 * 24 * time.Hour,
	"month":  30 * 24 * time.Hour,
	"year":   365 * 24 * time.Hour,
}

// ParseStatus reads the Docker/Podman Status column ("Up 3 hours (healthy)",
// "Exited (137) 5 minutes ago"). Text it does not recognize yields the zero
// StatusFacts.
func ParseStatus(status string) StatusFacts {
	m := statusRE.FindStringSubmatch(strings.TrimSpace(status))
	if m == nil {
		return StatusFacts{}
	}
	var f StatusFacts
	if m[2] != "" {
		code, err := strconv.Atoi(m[3])
		if err != nil {
			return StatusFacts{}
		}
		f.ExitCode, f.HasExitCode = code, true
	}
	phrase := m[4]
	if i := strings.Index(phrase, " ("); i >= 0 {
		phrase = phrase[:i]
	}
	phrase = strings.TrimSuffix(phrase, " ago")
	a := agePhraseRE.FindStringSubmatch(phrase)
	switch {
	case a == nil:
	case a[3] != "":
		f.HasAge = true
	case a[4] != "":
		f.Age, f.HasAge = unitDuration[a[4]], true
	default:
		if n, err := strconv.Atoi(a[1]); err == nil {
			f.Age, f.HasAge = time.Duration(n)*unitDuration[a[2]], true
		}
	}
	return f
}
