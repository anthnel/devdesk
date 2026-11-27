package enginefixture_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-09a3 (DEVDESK-944), security finding M2: the rule public-address. A public
// IPv4 address or a global IPv6 address (2000::/3) is a leak, except loopback, the
// documentation ranges, the engines' networks, 0.0.0.0 and what the private,
// link-local and CGNAT rule already owns. A dotted quad that is a version number,
// an OID or part of a longer number is not an address.

func publicFixture(stdout []byte) []byte {
	return enginefixture.Format(enginefixture.Fixture{
		Meta: enginefixture.Meta{
			Tool: enginefixture.Docker, Version: "29.10.1", Mode: enginefixture.Rootful, Recorded: "2026-11-27",
			Scenario: "a public address self-test", Redacted: "none", RecordedBy: "synthetic",
		},
		Exchange: enginefixture.Exchange{Argv: []string{"inspect", "x"}, Stdout: stdout},
	})
}

func rulesOf(data []byte) []string {
	var rules []string
	for _, l := range enginefixture.CheckLeaksAs("pub--docker-29.10.1-rootful", data, nil, nil) {
		rules = append(rules, l.Rule)
	}
	return rules
}

func TestPublicAddressIsALeak(t *testing.T) {
	for name, text := range map[string]string{
		"an IPv4 address":                  "8.8.8.8",
		"in a JSON value":                  `{"Gateway":"93.184.216.34"}`,
		"after a colon":                    "dns:1.1.1.1",
		"with a port":                      "52.1.2.3:443",
		"a CIDR":                           "34.120.0.0/16",
		"just below a documentation range": "192.0.3.1",
		"172.15, outside Docker's pools":   "172.15.0.1",
		"a global IPv6 address":            "2a00:1450:4007:80f::200e",
		"a global IPv6 address, upper":     "2A00:1450:4007:80F::200E",
		"a global IPv6 in brackets":        "[2606:4700:4700::1111]:53",
		"an IPv6 that starts with 3":       "3fff::1",
		"an IPv4-mapped IPv6 address":      "::ffff:8.8.8.8",
	} {
		t.Run(name, func(t *testing.T) {
			if rules := rulesOf(publicFixture([]byte(text + "\n"))); !slices.Contains(rules, "public-address") {
				t.Errorf("rules = %v for %q, want public-address", rules, text)
			}
		})
	}
}

func TestPublicAddressInABase64StreamIsALeak(t *testing.T) {
	// A binary stream is written as base64, and the check decodes it.
	data := publicFixture([]byte("\xff\x00\r reached 8.8.4.4 \n"))
	if !strings.Contains(string(data), "base64") {
		t.Fatal("the sample is not written as base64: the case proves nothing")
	}
	if rules := rulesOf(data); !slices.Contains(rules, "public-address") {
		t.Errorf("rules = %v, want public-address for an address inside a base64 stream", rules)
	}
}

func TestPublicAddressInTheArgvAndTheHeadersIsALeak(t *testing.T) {
	f := enginefixture.Fixture{
		Meta: enginefixture.Meta{
			Tool: enginefixture.Docker, Version: "29.10.1", Mode: enginefixture.Rootful, Recorded: "2026-11-27",
			Scenario: "pulled from 8.8.8.8", Redacted: "none", RecordedBy: "synthetic",
		},
		Exchange: enginefixture.Exchange{Argv: []string{"run", "--dns", "9.9.9.9", "img"}},
	}
	leaks := enginefixture.CheckLeaksAs("pub--docker-29.10.1-rootful", enginefixture.Format(f), nil, nil)
	lines := map[int]bool{}
	for _, l := range leaks {
		if l.Rule == "public-address" {
			lines[l.Line] = true
		}
	}
	if len(lines) != 2 {
		t.Errorf("public-address lines = %v, want the scenario header and the argv header", lines)
	}
}

func TestPublicAddressSparesWhatIsNotOne(t *testing.T) {
	for name, text := range map[string]string{
		"loopback":                      "127.0.0.1:6379",
		"another loopback":              "127.1.2.3",
		"0.0.0.0":                       "0.0.0.0:18080->80/tcp",
		"documentation 1":               "192.0.2.10",
		"documentation 2":               "198.51.100.7",
		"documentation 3":               "203.0.113.200",
		"IPv6 documentation":            "2001:db8::1",
		"IPv6 loopback":                 "::1",
		"IPv6 unspecified":              "::",
		"IPv6 link-local":               "fe80::1",
		"Docker bridge":                 "172.17.0.2",
		"Podman network":                "10.88.0.5",
		"a netmask":                     "255.255.255.0",
		"the broadcast address":         "255.255.255.255",
		"multicast":                     "224.0.0.251",
		"an octet above 255":            "999.1.2.3",
		"a version of five fields":      "1.2.3.4.5",
		"an OID":                        "OID 1.3.6.1.4.1",
		"a version after a v":           "v8.8.8.8",
		"a version after the word":      "version 8.8.8.8",
		"a JSON version":                `{"Version":"20.10.7.1"}`,
		"after a dot":                   "pkg.8.8.8.8",
		"after a dash":                  "build-52.1.2.3",
		"a date and time":               "2026-11-27T10:15:30.123456789Z",
		"a MAC address":                 "02:42:ac:11:00:02",
		"a time":                        "10:15:30",
		"a digest":                      "sha256:3f9d2c7add41be5e8f0a3f9d2c7add41be5e8f0a3f9d2c7add41be5e8f0a1234",
		"a sequence of digits":          "1234567890123",
		"a number with three fields":    "29.10.1",
		"a C++ scope":                   "std::vector",
		"a group that is not in 2000/3": "4000::1",
	} {
		t.Run(name, func(t *testing.T) {
			if rules := rulesOf(publicFixture([]byte(text + "\n"))); slices.Contains(rules, "public-address") {
				t.Errorf("rules = %v for %q, want no public-address", rules, text)
			}
		})
	}
}
