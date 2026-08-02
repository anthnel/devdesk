package netdiag

import (
	"strings"
	"testing"
)

// digAnswer is a full dig output with one answer, matching what the DNS test
// returns for a resolvable name.
const digAnswer = `
; <<>> DiG 9.18.24 <<>> google.com
;; global options: +cmd
;; Got answer:
;; ->>HEADER<<- opcode: QUERY, status: NOERROR, id: 41521
;; flags: qr rd ra; QUERY: 1, ANSWER: 2, AUTHORITY: 0, ADDITIONAL: 1

;; OPT PSEUDOSECTION:
; EDNS: version: 0, flags:; udp: 512
;; QUESTION SECTION:
;google.com.			IN	A

;; ANSWER SECTION:
google.com.		217	IN	A	142.250.75.238
google.com.		217	IN	A	142.250.75.174

;; Query time: 2 msec
;; SERVER: 8.8.8.8#53(8.8.8.8) (UDP)
;; WHEN: Sat Aug 02 12:00:00 UTC 2026
;; MSG SIZE  rcvd: 55
`

// digNXDomain is the shape returned for a name that does not exist: a SOA in an
// AUTHORITY section rather than an ANSWER one.
const digNXDomain = `
;; ->>HEADER<<- opcode: QUERY, status: NXDOMAIN, id: 9110
;; QUESTION SECTION:
;nope.example.			IN	A

;; AUTHORITY SECTION:
example.		900	IN	SOA	ns.example. hostmaster.example. 1 900 900 1800 900

;; Query time: 14 msec
;; SERVER: 1.1.1.1#53(1.1.1.1) (UDP)
`

// digNoAnswer resolves without error but returns nothing — a real case for
// AAAA lookups on IPv4-only hosts.
const digNoAnswer = `
;; ->>HEADER<<- opcode: QUERY, status: NOERROR, id: 3
;; QUESTION SECTION:
;example.com.			IN	AAAA

;; Query time: 5 msec
;; SERVER: 8.8.8.8#53(8.8.8.8) (UDP)
`

// ── Parsing ──────────────────────────────────────────────────────────────────

func TestParseDNSOutputExtractsEveryField(t *testing.T) {
	res := parseDNSOutput(digAnswer)

	if res.status != "NOERROR" {
		t.Errorf("status = %q, want NOERROR", res.status)
	}
	if res.question != "google.com  A" {
		t.Errorf("question = %q, want \"google.com  A\"", res.question)
	}
	if res.queryTime != "2 ms" {
		t.Errorf("queryTime = %q, want \"2 ms\"", res.queryTime)
	}
	// The parenthesised repeat of the address is noise and must be dropped.
	if res.server != "8.8.8.8#53" {
		t.Errorf("server = %q, want \"8.8.8.8#53\" without the trailing parenthesis", res.server)
	}
	if len(res.answers) != 2 {
		t.Fatalf("%d answers parsed, want 2", len(res.answers))
	}

	first := res.answers[0]
	if first.name != "google.com" {
		t.Errorf("answer name = %q, want the trailing dot stripped", first.name)
	}
	if first.ttl != "217" || first.rtype != "A" || first.value != "142.250.75.238" {
		t.Errorf("answer = %+v, want ttl 217, type A, value 142.250.75.238", first)
	}
}

// Records outside the ANSWER section must not be collected: an NXDOMAIN's SOA
// would otherwise be shown as if it were an answer.
func TestParseDNSOutputIgnoresOtherSections(t *testing.T) {
	res := parseDNSOutput(digNXDomain)

	if res.status != "NXDOMAIN" {
		t.Errorf("status = %q, want NXDOMAIN", res.status)
	}
	if len(res.answers) != 0 {
		t.Errorf("%d answers parsed from an NXDOMAIN reply, want none: %+v", len(res.answers), res.answers)
	}
	if res.question != "nope.example  A" {
		t.Errorf("question = %q, want it parsed even without answers", res.question)
	}
}

func TestParseDNSOutputHandlesJunk(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output string
	}{
		{"empty", ""},
		{"not dig output at all", "connection timed out; no servers could be reached"},
		{"truncated mid-section", ";; ANSWER SECTION:\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := parseDNSOutput(tc.output)

			if len(res.answers) != 0 {
				t.Errorf("answers = %+v, want none", res.answers)
			}
		})
	}
}

// ── Formatting ───────────────────────────────────────────────────────────────

func TestFormatDNSOutputShowsAnswers(t *testing.T) {
	out := strings.Join(formatDNSOutput(digAnswer, 80), "\n")

	for _, want := range []string{"NOERROR", "google.com", "142.250.75.238", "TTL 217s", "2 ms", "8.8.8.8#53"} {
		if !strings.Contains(out, want) {
			t.Errorf("the formatted output is missing %q", want)
		}
	}
	if !strings.Contains(out, "Answers") {
		t.Error("the answers section is not labelled")
	}
}

// A successful lookup with nothing to show has to say so, rather than render an
// empty section the user cannot distinguish from a rendering bug.
func TestFormatDNSOutputMarksAnEmptyAnswerSection(t *testing.T) {
	out := strings.Join(formatDNSOutput(digNoAnswer, 80), "\n")

	if !strings.Contains(out, "(none)") {
		t.Errorf("a NOERROR reply with no answers does not say so:\n%s", out)
	}
}

// A failed lookup shows the status, and must not claim an empty answer section
// — there is nothing to answer.
func TestFormatDNSOutputOnFailure(t *testing.T) {
	out := strings.Join(formatDNSOutput(digNXDomain, 80), "\n")

	if !strings.Contains(out, "NXDOMAIN") {
		t.Error("the failure status is not shown")
	}
	if strings.Contains(out, "(none)") {
		t.Error("an NXDOMAIN reply rendered an empty answers section")
	}
}

func TestFormatDNSOutputOnJunk(t *testing.T) {
	out := strings.Join(formatDNSOutput("connection timed out", 80), "\n")

	if !strings.Contains(out, "unknown") {
		t.Errorf("output with no parsable status does not fall back to \"unknown\":\n%s", out)
	}
}

// Every line is padded to the viewport width, so the details background is
// uniform (Rule 115).
func TestFormatDNSOutputPadsEveryLine(t *testing.T) {
	const width = 60

	for i, line := range formatDNSOutput(digAnswer, width) {
		if got := len([]rune(stripANSI(line))); got != width {
			t.Errorf("line %d is %d runes wide, want %d", i, got, width)
		}
	}
}

// The icon has to distinguish the three outcomes: a resolved name, a name that
// resolved to nothing, and a failure.
func TestDNSStatusIconDistinguishesOutcomes(t *testing.T) {
	resolved := dnsStatusIcon("NOERROR", 2)
	empty := dnsStatusIcon("NOERROR", 0)
	failed := dnsStatusIcon("NXDOMAIN", 0)

	if resolved == empty {
		t.Error("a resolved name and an empty answer share an icon")
	}
	if empty == failed {
		t.Error("an empty answer and a failure share an icon")
	}
	for _, icon := range []string{resolved, empty, failed} {
		if icon == "" {
			t.Error("dnsStatusIcon returned nothing")
		}
	}
}

// stripANSI removes escape sequences so widths can be measured. Under `go test`
// lipgloss emits none, but the helper keeps the assertion honest if that ever
// changes.
func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEscape = true
		case inEscape && r == 'm':
			inEscape = false
		case !inEscape:
			b.WriteRune(r)
		}
	}
	return b.String()
}
