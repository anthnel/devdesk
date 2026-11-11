package jobs

import (
	"path/filepath"
	"slices"
	"time"
)

// MaxFailures caps how many remembered failures the registry keeps.
const MaxFailures = 500

// FailureKey identifies what failed: an operation kind on one target in one
// context. Target is always a TargetKey.
type FailureKey struct {
	Kind    Kind
	Target  string
	Context string
}

// Failure is the last remembered failure for a FailureKey. The registry
// stores it with an already normalized key.
type Failure struct {
	FailureKey
	// Display is the human-readable name of the target.
	Display string
	// Detail is the failure reason.
	Detail string
	At     time.Time
	// Seq orders failures: the registry assigns it on pose, strictly increasing
	// and never reused, even after an eviction or a re-pose of the same key.
	Seq uint64
}

// TargetKey normalizes a target so every view naming the same thing produces
// the same key. Only absolute paths are cleaned; image references, forge
// paths and slugs are kept verbatim, since two spellings of an image
// ("nginx", "nginx:latest") are not provably the same target.
func TargetKey(target string) string {
	if filepath.IsAbs(target) {
		return filepath.Clean(target)
	}
	return target
}

// FailureFor returns the failure remembered for kind on target in
// contextName. The target is normalized with TargetKey.
func FailureFor(failures []Failure, kind Kind, target, contextName string) (Failure, bool) {
	key := FailureKey{Kind: kind, Target: TargetKey(target), Context: contextName}
	for _, f := range failures {
		if f.FailureKey == key {
			return f, true
		}
	}
	return Failure{}, false
}

// FailuresOn returns a copy of the failures on target in contextName, in the
// order of the input slice. With no kinds, every kind is kept. An empty
// target is not special: it matches only failures stored with an empty target.
func FailuresOn(failures []Failure, contextName, target string, kinds ...Kind) []Failure {
	target = TargetKey(target)
	var out []Failure
	for _, f := range failures {
		if f.Context != contextName || f.Target != target {
			continue
		}
		if len(kinds) > 0 && !slices.Contains(kinds, f.Kind) {
			continue
		}
		out = append(out, f)
	}
	return out
}
