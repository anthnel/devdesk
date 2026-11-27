// Package secretenv decides which environment variable names are sensitive,
// so every display or copy of a container's environment masks the same values
// (OC-12). It classifies names only; masking is the caller's job.
package secretenv

import (
	"fmt"
	"regexp"
	"strings"
)

// defaultPatterns are case-insensitive substrings of a variable name. The list
// is deliberately blunt: a false positive hides a harmless value, a false
// negative shows a secret. KEYBOARD_LAYOUT and MONKEY_COUNT are masked by KEY,
// and that is accepted.
var defaultPatterns = []string{"TOKEN", "SECRET", "PASSWORD", "KEY", "CREDENTIAL"}

// DefaultPatterns returns a copy of the built-in patterns.
func DefaultPatterns() []string {
	return append([]string(nil), defaultPatterns...)
}

// Matcher classifies variable names. A nil *Matcher and the zero value apply the
// default patterns only.
type Matcher struct {
	res []*regexp.Regexp
}

// builtin is built once; a Matcher is immutable, so sharing it is safe.
var builtin, _ = New(nil)

// Default returns a matcher that applies the built-in patterns only.
func Default() *Matcher { return builtin }

// New returns a matcher for the default patterns plus extra. Each entry is a Go
// regular expression, case-insensitive, searched anywhere in the name (anchor
// it with ^ or $ to narrow it). The list only adds, it never removes a default.
//
// An empty or blank entry, one that does not compile, or one that matches the
// empty name (such as "|"), is reported in the returned error with its
// 0-based index and its text. The matcher is never nil: it
// carries the defaults and every valid entry, so a bad entry never lowers the
// masking.
func New(extra []string) (*Matcher, error) {
	m := &Matcher{}
	for _, p := range defaultPatterns {
		m.res = append(m.res, regexp.MustCompile("(?i)"+regexp.QuoteMeta(p)))
	}
	var errs []string
	for i, entry := range extra {
		if strings.TrimSpace(entry) == "" {
			errs = append(errs, fmt.Sprintf("entry at index %d (%q): empty pattern", i, entry))
			continue
		}
		re, err := regexp.Compile("(?i)" + entry)
		if err != nil {
			errs = append(errs, fmt.Sprintf("entry at index %d (%q): %v", i, entry, err))
			continue
		}
		// "|" or "a|" compile and match every name: same as an empty entry.
		if re.MatchString("") {
			errs = append(errs, fmt.Sprintf("entry at index %d (%q): matches an empty name", i, entry))
			continue
		}
		m.res = append(m.res, re)
	}
	if len(errs) > 0 {
		return m, fmt.Errorf("invalid sensitive variable pattern: %s", strings.Join(errs, "; "))
	}
	return m, nil
}

// Sensitive reports whether the value of the variable called name must be
// masked. It is safe on a nil receiver, which behaves as Default().
func (m *Matcher) Sensitive(name string) bool {
	if m == nil || len(m.res) == 0 {
		m = builtin
	}
	for _, re := range m.res {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}
