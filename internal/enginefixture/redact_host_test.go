package enginefixture_test

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-09a3 (DEVDESK-944), review finding 2: the host is replaced at the boundary
// the local-identity rule reads (a dot, a dash or a slash separate words), and a
// host written as a fully qualified name loses its internal suffix, so that
// "fixture-host.lan" does not stand there for internal-name to refuse.

func TestRedactReplacesAHostThatSitsBesideADotOrADash(t *testing.T) {
	r := enginefixture.Redaction{Host: "alicebox"}
	tests := []struct{ name, in, want string }{
		{"a name with an external domain", "alicebox.example.org", "fixture-host.example.org"},
		{"a dash after it", "alicebox-2", "fixture-host-2"},
		{"a dash before it", "node-alicebox", "node-fixture-host"},
		{"any case", "ALICEBOX", "fixture-host"},
		{"glued to a letter before", "xalicebox", "xalicebox"},
		{"glued to a letter after", "alicebox2", "alicebox2"},
		{"glued to an underscore", "alicebox_1", "alicebox_1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := enginefixture.Redact(enginefixture.Exchange{Argv: []string{"x"}, Stdout: []byte(tt.in)}, r)
			if string(got.Stdout) != tt.want {
				t.Errorf("stdout = %q, want %q", got.Stdout, tt.want)
			}
		})
	}
}

func TestRedactAbsorbsTheInternalSuffixOfAHost(t *testing.T) {
	tests := []struct{ name, host, in, want string }{
		{".lan", "alicebox", "Hostname: alicebox.lan", "Hostname: fixture-host"},
		{".local", "alicebox", "alicebox.local up", "fixture-host up"},
		{".internal", "alicebox", `"alicebox.internal"`, `"fixture-host"`},
		{".home.arpa", "alicebox", "alicebox.home.arpa:22", "fixture-host:22"},
		{"any case", "alicebox", "ALICEBOX.LAN", "fixture-host"},
		{"a longer name under the zone is kept as a whole", "alicebox", "alicebox.lanes", "fixture-host.lanes"},
		{"a sub domain below the zone name", "alicebox", "alicebox.lan.example", "fixture-host.lan.example"},
		{"the suffix at the end of the text", "alicebox", "alicebox.lan", "fixture-host"},
		{"the host is a qualified name itself", "alicebox.home.arpa", "alicebox.home.arpa", "fixture-host"},
		{"the first label of a qualified host, on its own", "alicebox.home.arpa", "short name alicebox here", "short name fixture-host here"},
		{"the first label of a qualified host with the suffix", "alicebox.example.org", "alicebox.lan", "fixture-host"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, header := enginefixture.Redact(enginefixture.Exchange{Argv: []string{"x"}, Stdout: []byte(tt.in)}, enginefixture.Redaction{Host: tt.host})
			if string(got.Stdout) != tt.want || header != "host" {
				t.Errorf("stdout = %q (header %q), want %q (host)", got.Stdout, header, tt.want)
			}
		})
	}
}

// The net: the redacted text of a host written as a name of the internal zone
// passes the leak check, internal-name included.
func TestRedactedQualifiedHostPassesTheLeakCheck(t *testing.T) {
	local := enginefixture.LocalIdentityTerms("alicebox")
	f := enginefixture.Fixture{
		Meta: enginefixture.Meta{
			Tool: enginefixture.Podman, Version: "6.2.1", Mode: enginefixture.Rootless, Recorded: "2026-11-27",
			Scenario: "a host in the internal zone", Redacted: "host", RecordedBy: enginefixture.RecordedByContract,
		},
	}
	raw := enginefixture.Exchange{Argv: []string{"info"}, Stdout: []byte("hostname: alicebox.lan\nalso alicebox.home.arpa and alicebox.local\n")}
	f.Exchange, _ = enginefixture.Redact(raw, enginefixture.Redaction{Host: "alicebox"})

	data := enginefixture.Format(f)
	var rules []string
	for _, l := range enginefixture.CheckLeaksAs("host", data, nil, local) {
		rules = append(rules, l.Rule)
	}
	if len(rules) != 0 {
		t.Errorf("rules = %v, want none: %s", rules, data)
	}
	f.Exchange = raw
	if rules := enginefixture.CheckLeaksAs("raw", enginefixture.Format(f), nil, local); len(rules) == 0 {
		t.Errorf("the raw text passes the leak check: the sample proves nothing")
	}
}

func TestRedactIsStillIdempotentForAQualifiedHost(t *testing.T) {
	r := enginefixture.Redaction{Host: "alicebox.home.arpa"}
	in := enginefixture.Exchange{Argv: []string{"info"}, Stdout: []byte("alicebox.home.arpa alicebox ALICEBOX.lan")}

	once, _ := enginefixture.Redact(in, r)
	twice, header := enginefixture.Redact(once, r)

	if !sameExchange(once, twice) || header != "none" || strings.Contains(string(once.Stdout), "alicebox") {
		t.Errorf("once = %q, twice = %q, header = %q", once.Stdout, twice.Stdout, header)
	}
	if want := "fixture-host fixture-host fixture-host"; string(once.Stdout) != want {
		t.Errorf("stdout = %q, want %q", once.Stdout, want)
	}
}
