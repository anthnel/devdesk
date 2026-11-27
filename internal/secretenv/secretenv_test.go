package secretenv

import (
	"strings"
	"testing"
)

// The default patterns are case-insensitive substrings of the variable name.
// The list is deliberately blunt: a false positive hides a harmless value, a
// false negative shows a secret (OC-12, S9).
func TestDefaultClassification(t *testing.T) {
	m := Default()
	cases := []struct {
		name string
		want bool
	}{
		// OC-12 criterion 1: sensitive, whatever the case.
		{"API_TOKEN", true},
		{"db_password", true},
		{"AWS_SECRET_ACCESS_KEY", true},
		{"GITHUB_CREDENTIALS", true},
		{"SSH_KEY", true},
		{"Api_Token", true},
		{"secret", true},
		{"MY_PASSWORD_FILE", true}, // a pattern matches anywhere in the name
		// OC-12 criterion 1: not sensitive.
		{"LOG_LEVEL", false},
		{"PATH", false},
		{"HOME", false},
		{"LANG", false},
		{"HOSTNAME", false},
		{"", false},
		// OC-12 criterion 2: accepted and documented false positives.
		{"KEYBOARD_LAYOUT", true},
		{"MONKEY_COUNT", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := m.Sensitive(tc.name); got != tc.want {
				t.Errorf("Default().Sensitive(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// The five default patterns are part of the contract: each one alone must
// classify a name that carries no other pattern.
func TestDefaultPatterns(t *testing.T) {
	want := []string{"TOKEN", "SECRET", "PASSWORD", "KEY", "CREDENTIAL"}
	got := DefaultPatterns()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("DefaultPatterns() = %v, want %v", got, want)
	}
	for _, p := range want {
		if !Default().Sensitive("X_" + p + "_Y") {
			t.Errorf("default pattern %q does not classify X_%s_Y", p, p)
		}
	}
	// The caller gets a copy: editing it must not weaken the defaults.
	got[0] = "NOTHING"
	if !Default().Sensitive("API_TOKEN") {
		t.Error("mutating the slice returned by DefaultPatterns() weakened the defaults")
	}
}

// OC-12 criterion 3: a configured entry is a name or a pattern, matched
// case-insensitively. Proposal under test: every entry is a Go regular
// expression searched in the name, so a bare name is a pattern too.
func TestNewAddsToTheDefaults(t *testing.T) {
	m, err := New([]string{"INTERNAL_URL", "^CORP_", "_dsn$"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cases := []struct {
		name string
		want bool
	}{
		{"INTERNAL_URL", true},
		{"internal_url", true}, // insensitive, as the defaults
		{"CORP_REGION", true},  // anchored pattern
		{"corp_region", true},
		{"MY_CORP_REGION", false}, // the anchor holds
		{"SENTRY_DSN", true},
		{"SENTRY_DSN_URL", false},
		{"PUBLIC_URL", false},
		{"LOG_LEVEL", false},
		// The list adds to the defaults, it never removes from them.
		{"API_TOKEN", true},
		{"KEYBOARD_LAYOUT", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := m.Sensitive(tc.name); got != tc.want {
				t.Errorf("Sensitive(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// Without the extra list, New is the default matcher.
func TestNewWithoutEntries(t *testing.T) {
	for _, extra := range [][]string{nil, {}} {
		m, err := New(extra)
		if err != nil {
			t.Fatalf("New(%v): %v", extra, err)
		}
		if !m.Sensitive("API_TOKEN") || m.Sensitive("LOG_LEVEL") || m.Sensitive("INTERNAL_URL") {
			t.Errorf("New(%v) does not behave as Default()", extra)
		}
	}
}

// OC-12 criterion 4: an invalid entry yields an error that names the entry and
// its position, and a matcher that is still usable: the defaults apply, and so
// do the entries that were valid. Never less masking than before.
func TestNewInvalidEntryKeepsTheDefaults(t *testing.T) {
	cases := []struct {
		name      string
		extra     []string
		validKept bool // the list also carries INTERNAL_URL, which must survive
	}{
		{"unclosed class", []string{"INTERNAL_URL", "["}, true},
		{"unclosed group", []string{"(oops", "INTERNAL_URL"}, true},
		{"empty entry", []string{"", "INTERNAL_URL"}, true}, // would match every name
		{"blank entry", []string{"   ", "INTERNAL_URL"}, true},
		{"alternation with an empty branch", []string{"|", "INTERNAL_URL"}, true}, // matches every name
		{"trailing empty branch", []string{"a|", "INTERNAL_URL"}, true},
		{"empty group", []string{"()", "INTERNAL_URL"}, true},
		{"only an invalid", []string{"["}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := New(tc.extra)
			if err == nil {
				t.Fatalf("New(%q): no error", tc.extra)
			}
			if m == nil {
				t.Fatalf("New(%q) returned a nil matcher next to its error", tc.extra)
			}
			if !m.Sensitive("API_TOKEN") || !m.Sensitive("KEYBOARD_LAYOUT") {
				t.Error("the default patterns no longer apply after an invalid entry")
			}
			if m.Sensitive("LOG_LEVEL") {
				t.Error("an invalid entry made LOG_LEVEL sensitive")
			}
			if tc.validKept && !m.Sensitive("INTERNAL_URL") {
				t.Error("a valid entry next to an invalid one was dropped")
			}
		})
	}
}

// The error quotes the offending entry and its index so the user can find it.
func TestNewErrorNamesTheEntry(t *testing.T) {
	_, err := New([]string{"INTERNAL_URL", "[bad"})
	if err == nil {
		t.Fatal("no error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "[bad") {
		t.Errorf("error does not quote the entry: %q", msg)
	}
	if !strings.Contains(msg, "index 1") {
		t.Errorf("error does not give the 0-based index of the entry: %q", msg)
	}
}

// A matcher that was never built masks like the defaults: the zero value is
// not a way to show everything.
func TestNilMatcherUsesTheDefaults(t *testing.T) {
	var m *Matcher
	if !m.Sensitive("API_TOKEN") {
		t.Error("nil matcher shows API_TOKEN")
	}
	if m.Sensitive("LOG_LEVEL") {
		t.Error("nil matcher masks LOG_LEVEL")
	}
}
