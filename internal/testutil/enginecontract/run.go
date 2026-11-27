package enginecontract

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"sync"
	"time"
)

// runIDPattern is the alphabet of a run id. It keeps the label value, the resource
// names and the filter a cleanup writes free of anything a shell or the engine
// would read as syntax; it is also what stops an empty id from making
// `label=devdesk.test=` match every run.
var runIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{5,31}$`)

// rolePattern is the alphabet of the role in a resource name.
var rolePattern = regexp.MustCompile(`^[a-z0-9]{1,16}$`)

// ValidRunID reports whether id can be a run id.
func ValidRunID(id string) bool { return runIDPattern.MatchString(id) }

// NewRunID builds an id from the day, in UTC (contract.sh builds its own with
// `date -u`), and four bytes read from random: "20261127-a1b2c3d4".
func NewRunID(random io.Reader, now time.Time) string {
	var b [4]byte
	if _, err := io.ReadFull(random, b[:]); err != nil {
		panic(fmt.Sprintf("enginecontract: no randomness for a run id: %v", err))
	}
	return now.UTC().Format("20060102") + "-" + hex.EncodeToString(b[:])
}

var (
	processOnce sync.Once
	processID   string
)

// ProcessRunID is the run id of this process: RunEnv when it is set (an invalid
// value is an error, never replaced), else one generated id, the same on every
// call, so the resources of a test binary share one run.
func ProcessRunID(getenv func(string) string) (string, error) {
	if v := getenv(RunEnv); v != "" {
		if !ValidRunID(v) {
			return "", fmt.Errorf("%s=%q is not a valid run id (lower case letters, digits and dashes, 6 to 32 characters)", RunEnv, v)
		}
		return v, nil
	}
	processOnce.Do(func() { processID = NewRunID(rand.Reader, time.Now()) })
	return processID, nil
}

// Label is the label a resource of the run carries: "devdesk.test=<runID>".
func Label(runID string) string { return LabelKey + "=" + runID }

// ResourceName is the name of the resource a contract creates for a role:
// "ddc-<runID>-<role>". A role is spelled in the source of a contract, so an
// invalid one panics rather than returning an error nobody reads.
func ResourceName(runID, role string) string {
	if !rolePattern.MatchString(role) {
		panic(fmt.Sprintf("enginecontract: role %q is not 1 to 16 lower case letters or digits", role))
	}
	return NamePrefix + runID + "-" + role
}
