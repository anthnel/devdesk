package enginefixture_test

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/user"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-08b (DEVDESK-942), step 6 of the OC-08 plan: the leak check of the engine
// fixtures. A fixture is a text file anyone can read in review, and it stays in
// the git history for good: nothing of the machine that recorded it may be in
// it (a user name, a host name, a personal path, a private address, a value of a
// sensitive variable, a token).
//
// The API these tests pin down (package enginefixture, on top of fixtureleak):
//
//	type Leak struct{ Fixture string; Line int; Rule string }   // never the matched text
//	func CheckLeaks(fixture string, data []byte, exact []string) []Leak
//	func CheckLeaksAs(fixture string, data []byte, exact, local []string) []Leak
//	func LocalIdentityTerms(names ...string) []string  // the terms worth checking: >= 3 chars, not in the stop list
//	func MachineIdentities() []string                  // LocalIdentityTerms of os.Hostname, user.Current, $USER
//	var LabTerms, AllowedHosts []string
//	var EngineBridgeNets []netip.Prefix
//	var ImageHomes map[string]string                   // /home/<name> kept by an image -> reason; empty at first
//	const PlaceholderUser, PlaceholderHost, PlaceholderHome = "fixture-user", "fixture-host", "/home/fixture-user"
//
// CheckLeaks parses the file. When it is valid, each stream is examined decoded
// (a base64 stream would otherwise hide a leak) and a leak inside a base64 stream
// is reported at the line of the "# stdout:" or "# stderr:" header; the headers,
// the argv included, are examined too, so a pending file is checked. When the
// file is not valid, the raw text is examined line by line. "exact" are the terms
// of the external deny list (rule exact-term), "local" the identities of the
// machine that runs the test (rule local-identity); both, like the lab names, are
// matched at the word: the boundary is any character outside [A-Za-z0-9_.], the
// case is ignored and the JSON-escaped and URL-escaped forms are searched too.
//
// The rules, by the name a Leak carries: secret-token-format, jwt-like-value,
// host-not-allowed, private-or-link-local-address, internal-name, personal-path,
// lab-identity, local-identity, exact-term, sensitive-env-value and, added by the
// review of OC-08a, invisible-character (a bidirectional control or a zero-width
// character, which the text encoding of a stream does let through).
//
// Every leak of these tests is built from invented values. None of them is the
// host name or the user name of the machine that runs them.

const (
	rSecretToken = "secret-token-format"
	rJWT         = "jwt-like-value"
	rHost        = "host-not-allowed"
	rAddress     = "private-or-link-local-address"
	rInternal    = "internal-name"
	rPath        = "personal-path"
	rLab         = "lab-identity"
	rLocal       = "local-identity"
	rExact       = "exact-term"
	rEnv         = "sensitive-env-value"
	rInvisible   = "invisible-character"
)

// slot is the text of the base fixture that a case replaces with its leak.
const slot = "zzslot"

// baseStdout is clean: the replacement names, an engine bridge address, a
// container name that holds "dd" without being the lab user, and an id that holds
// "dd" inside hexadecimal.
const baseStdout = "[\n    {\n" +
	"        \"Id\": \"3f9d2c7add41be\",\n" +
	"        \"Name\": \"/ddm-run\",\n" +
	"        \"Hostname\": \"fixture-host\",\n" +
	"        \"Home\": \"/home/fixture-user\",\n" +
	"        \"IPAddress\": \"172.17.0.2\",\n" +
	"        \"Note\": \"" + slot + "\"\n" +
	"    }\n]\n"

const leakBaseFile = "leak-base--docker-29.10.1-rootful.cli"

func leakBase() enginefixture.Fixture {
	return enginefixture.Fixture{
		Name: "leak-base--docker-29.10.1-rootful", Scenario: "leak-base",
		Meta: enginefixture.Meta{
			Tool: enginefixture.Docker, Version: "29.10.1", Mode: enginefixture.Rootful,
			Recorded: "2026-11-27", Scenario: "leak self-test", Redacted: "home, user, host", RecordedBy: "synthetic",
		},
		Exchange: enginefixture.Exchange{Argv: []string{"inspect", "3f9d2c7add41be"}, Stdout: []byte(baseStdout)},
	}
}

// mutation plants a leak in the base fixture.
type mutation func(f *enginefixture.Fixture)

// inStdout replaces the slot of stdout with s.
func inStdout(s string) mutation {
	return func(f *enginefixture.Fixture) {
		f.Exchange.Stdout = bytes.Replace(f.Exchange.Stdout, []byte(slot), []byte(s), 1)
	}
}

// inStderr makes s the whole of stderr.
func inStderr(s string) mutation {
	return func(f *enginefixture.Fixture) { f.Exchange.Stderr = []byte(s + "\n") }
}

// inBinaryStdout and inBinaryStderr make the stream "\xff\x00\r" followed by s,
// which no text encoding may carry: the file writes it as base64.
func inBinaryStdout(s string) mutation {
	return func(f *enginefixture.Fixture) { f.Exchange.Stdout = []byte("\xff\x00\r" + s) }
}

func inBinaryStderr(s string) mutation {
	return func(f *enginefixture.Fixture) { f.Exchange.Stderr = []byte("\xff\x00\r" + s) }
}

func inArgv(words ...string) mutation {
	return func(f *enginefixture.Fixture) { f.Exchange.Argv = words }
}

func inScenario(s string) mutation {
	return func(f *enginefixture.Fixture) { f.Meta.Scenario = s }
}

// asPending turns the fixture into the file the recorder writes before it has run
// anything: an argv and no answer.
func asPending(inner mutation) mutation {
	return func(f *enginefixture.Fixture) {
		inner(f)
		f.Meta.Record = enginefixture.RecordPending
		f.Exchange.Exit, f.Exchange.Stdout, f.Exchange.Stderr = 0, nil, nil
	}
}

type leakCase struct {
	name    string
	mutate  mutation
	planted string   // the invented text: its line is where the leak must be reported, and it is never echoed
	at      string   // another text to find the line by (the header of a base64 stream); planted when empty
	rules   []string // every one must be reported at that line, and no other (but maybe)
	maybe   []string // rules that may also be reported
	exact   []string
	local   []string
}

func leakLineOf(data, needle string) int {
	i := strings.Index(data, needle)
	if i < 0 {
		return 0
	}
	return strings.Count(data[:i], "\n") + 1
}

// render formats the fixture and checks that the file is valid: a case must go
// through the parsed path, not through the raw text fallback.
func render(t *testing.T, f enginefixture.Fixture) []byte {
	t.Helper()
	data := enginefixture.Format(f)
	if _, err := enginefixture.Parse("leak", leakBaseFile, data); err != nil {
		t.Fatalf("the planted file is not a valid fixture, so the case would test the raw-text fallback: %v", err)
	}
	return data
}

func plantInto(t *testing.T, m mutation) []byte {
	t.Helper()
	f := leakBase()
	m(&f)
	return render(t, f)
}

func assertPlanted(t *testing.T, tc leakCase) {
	t.Helper()
	data := plantInto(t, tc.mutate)
	at := tc.at
	if at == "" {
		at = tc.planted
	}
	line := leakLineOf(string(data), at)
	if line == 0 {
		t.Fatalf("the text %q is not in the file: the case is broken", at)
	}
	const name = "planted--docker"
	leaks := enginefixture.CheckLeaksAs(name, data, tc.exact, tc.local)
	got := map[string]bool{}
	for _, l := range leaks {
		if l.Fixture != name {
			t.Errorf("Leak.Fixture = %q, want the name given to CheckLeaksAs", l.Fixture)
		}
		if l.Line != line {
			t.Errorf("leak %q reported at line %d, want line %d (the only line that changed)", l.Rule, l.Line, line)
		}
		if !slices.Contains(tc.rules, l.Rule) && !slices.Contains(tc.maybe, l.Rule) {
			t.Errorf("unexpected rule %q at line %d, want only %v", l.Rule, l.Line, tc.rules)
		}
		got[l.Rule] = true
	}
	for _, rule := range tc.rules {
		if !got[rule] {
			t.Errorf("no leak with rule %q at line %d, got %v\n%s", rule, line, leaks, data)
		}
	}
	if echoed := fmt.Sprintf("%+v %#v", leaks, leaks); strings.Contains(echoed, tc.planted) {
		t.Errorf("the leaks repeat the planted text: %s", echoed)
	}
}

// The values below look like credentials to the secret-format rules. They are
// assembled at run time so that no literal of this file does.
var (
	fakeGitHubToken = "ghp" + "_" + strings.Repeat("Zz0", 12)
	fakeJWT         = "eyJ" + "hbGciOiJIUzI1NiJ9.zz-payload-5"
	fakePEM         = "-----BEGIN " + "RSA PRIVATE" + " KEY-----"
)

func leakCases() []leakCase {
	one := func(name string, m mutation, planted, rule string) leakCase {
		return leakCase{name: name, mutate: m, planted: planted, rules: []string{rule}}
	}
	return []leakCase{
		// personal-path
		{name: "path/the lab user's home", mutate: inStdout("/home/dd/x"), planted: "/home/dd/x", rules: []string{rPath, rLab}},
		one("path/Linux home", inStdout("/home/zed9/.cache"), "/home/zed9/.cache", rPath),
		one("path/Silverblue home", inStdout("/var/home/zed9/.cache"), "/var/home/zed9/.cache", rPath),
		one("path/macOS home", inStdout("/Users/zed9/Library"), "/Users/zed9/Library", rPath),
		one("path/root home", inStdout("/root/ddmproj"), "/root/ddmproj", rPath),
		one("path/root alone, quoted", inStdout(`"/root"`), `"/root"`, rPath),
		one("path/after =", inStdout("HOME=/home/zed9"), "HOME=/home/zed9", rPath),
		one("path/after :", inStdout("bind:/home/zed9/data:/data"), "bind:/home/zed9/data", rPath),
		one("path/after ,", inStdout("a,/home/zed9/x"), "a,/home/zed9/x", rPath),
		one("path/after |", inStdout("x|/home/zed9/x"), "x|/home/zed9/x", rPath),
		one("path/after (", inStdout("(/home/zed9/x)"), "(/home/zed9/x)", rPath),
		one("path/after [", inStdout("[/home/zed9/x]"), "[/home/zed9/x]", rPath),
		one("path/after a blank", inStdout("cd /home/zed9/x"), "cd /home/zed9/x", rPath),
		one("path/at the start of a line", inStdout("x\n/home/zed9/x"), "/home/zed9/x", rPath),
		one("path/Windows profile, JSON-escaped", inStdout(`C:\\Users\\bob\\AppData`), `C:\\Users\\bob`, rPath),
		one("path/Windows profile, raw", inStdout(`see C:\Users\bob`), `C:\Users\bob`, rPath),
		one("path/Windows profile, lower case", inStdout(`c:\\users\\bob`), `c:\\users\\bob`, rPath),
		one("path/in an argv", inArgv("run", "-v", "/home/zed9/x:/x", "alpine"), "/home/zed9/x:/x", rPath),
		one("path/in a header", inScenario("run in /home/zed9/proj"), "/home/zed9/proj", rPath),

		// lab-identity
		one("lab/dd-podman", inStdout("dd-podman"), "dd-podman", rLab),
		one("lab/dd-docker", inStdout("dd-docker"), "dd-docker", rLab),
		one("lab/jakku", inStdout("host jakku up"), "jakku", rLab),
		one("lab/JAKKU in upper case", inStdout("host JAKKU up"), "JAKKU", rLab),
		one("lab/the user alone", inStdout("user dd logged in"), "user dd logged in", rLab),
		one("lab/the user before an @", inStdout("ssh dd@host"), "ssh dd@host", rLab),
		one("lab/in a header", inScenario("recorded on dd-podman"), "dd-podman", rLab),

		// internal-name
		one("name/home.arpa", inStdout("nas01.home.arpa"), "nas01.home.arpa", rInternal),
		one("name/upper case", inStdout("NAS02.HOME.ARPA"), "NAS02.HOME.ARPA", rInternal),
		one("name/.lan", inStdout("gitlab.corp.lan"), "gitlab.corp.lan", rInternal),
		one("name/.internal", inStdout("vault.zz9.internal"), "vault.zz9.internal", rInternal),
		one("name/.local", inStdout("printer.local"), "printer.local", rInternal),
		one("name/in an argv", inArgv("pull", "registry.corp.lan/x:1"), "registry.corp.lan", rInternal),

		// private-or-link-local-address
		one("address/192.168/16", inStdout("see 192.168.1.10"), "192.168.1.10", rAddress),
		one("address/10/8", inStdout("see 10.0.0.5"), "10.0.0.5", rAddress),
		one("address/172.16/16 is not an engine network", inStdout("see 172.16.0.9"), "172.16.0.9", rAddress),
		one("address/10.87 is next to Podman's", inStdout("see 10.87.255.255"), "10.87.255.255", rAddress),
		one("address/10.90 is next to Podman's", inStdout("see 10.90.0.1"), "10.90.0.1", rAddress),
		one("address/link-local", inStdout("see 169.254.1.2"), "169.254.1.2", rAddress),
		one("address/CGNAT", inStdout("see 100.64.1.1"), "100.64.1.1", rAddress),
		one("address/IPv6 unique-local", inStdout("see fd00::1f"), "fd00::1f", rAddress),
		one("address/IPv6 link-local", inStdout("see fe80::a1b2:c3d4"), "fe80::a1b2:c3d4", rAddress),
		one("address/glued to a letter", inStdout("ip192.168.7.7"), "ip192.168.7.7", rAddress),

		// host-not-allowed
		one("host/another registry", inStdout("pulled from https://registry.example.org/v2/"), "registry.example.org", rHost),
		one("host/public address", inStdout("http://203.0.113.7:5000/"), "203.0.113.7", rHost),
		one("host/credentials in the URL", inStdout("https://u:p@reg.example/"), "u:p@reg.example", rHost),
		one("host/user info on an allowed host", inStdout("https://user@index.docker.io/v1/"), "user@index.docker.io", rHost),
		one("host/JSON-escaped slashes", inStdout(`https:\/\/reg.example\/v2`), "reg.example", rHost),

		// local-identity: the identities are given by the test.
		{name: "local/host name", mutate: inStdout("Hostname: zedbox"), planted: "zedbox", rules: []string{rLocal}, local: []string{"zedbox"}},
		{name: "local/in upper case", mutate: inStdout("Hostname: ZEDBOX"), planted: "ZEDBOX", rules: []string{rLocal}, local: []string{"zedbox"}},
		{name: "local/before a dash", mutate: inStdout("name zedbox-2"), planted: "zedbox-2", rules: []string{rLocal}, local: []string{"zedbox"}},
		{name: "local/in a path", mutate: inStdout("/srv/zedbox/x"), planted: "/srv/zedbox/x", rules: []string{rLocal}, local: []string{"zedbox"}},
		{name: "local/user before an @", mutate: inStdout("zeduser@elsewhere"), planted: "zeduser@", rules: []string{rLocal}, local: []string{"zedbox", "zeduser"}},
		{name: "local/the user's home is also a personal path", mutate: inStdout("/home/zeduser/x"), planted: "/home/zeduser/x", rules: []string{rLocal, rPath}, local: []string{"zeduser"}},
		{name: "local/in an argv", mutate: inArgv("run", "--hostname", "zedbox", "alpine"), planted: "zedbox", rules: []string{rLocal}, local: []string{"zedbox"}},

		// exact-term: the external deny list.
		{name: "exact/the term", mutate: inStdout("see zz-denied-term here"), planted: "zz-denied-term", rules: []string{rExact}, exact: []string{"zz-denied-term"}},
		{name: "exact/another case", mutate: inStdout("see ZZ-Denied-Term here"), planted: "ZZ-Denied-Term", rules: []string{rExact}, exact: []string{"zz-denied-term"}},
		{name: "exact/before a dash", mutate: inStdout("zz-denied-term-2"), planted: "zz-denied-term-2", rules: []string{rExact}, exact: []string{"zz-denied-term"}},
		{name: "exact/JSON-escaped slash", mutate: inStdout(`see zz\/denied here`), planted: `zz\/denied`, rules: []string{rExact}, exact: []string{"zz/denied"}},
		{name: "exact/URL-escaped slash", mutate: inStdout("see zz%2Fdenied here"), planted: "zz%2Fdenied", rules: []string{rExact}, exact: []string{"zz/denied"}},
		{name: "exact/URL-escaped space", mutate: inStdout("see zz+denied+two here"), planted: "zz+denied+two", rules: []string{rExact}, exact: []string{"zz denied two"}},
		{name: "exact/in a header", mutate: inScenario("about zz-denied-term"), planted: "zz-denied-term", rules: []string{rExact}, exact: []string{"zz-denied-term"}},

		// sensitive-env-value
		one("env/SECRETENV in an Env array", inStdout(`"], "Env": ["PATH=/usr/bin", "SECRETENV=x`), "SECRETENV=x", rEnv),
		one("env/password", inStdout("DB_PASSWORD=zzpw9"), "zzpw9", rEnv),
		one("env/lower-case name", inStdout("api_token=zztok9"), "zztok9", rEnv),
		one("env/in an argv", inArgv("run", "-e", "MY_TOKEN=zzv9", "alpine"), "zzv9", rEnv),

		// secret-token-format and jwt-like-value
		one("token/GitHub", inStdout(fakeGitHubToken), fakeGitHubToken, rSecretToken),
		one("token/PEM private key", inStdout(fakePEM), fakePEM, rSecretToken),
		one("token/JWT", inStdout(fakeJWT), fakeJWT, rJWT),

		// A leak in a base64 stream is found and reported at the header of the stream.
		{name: "base64/path in stdout", mutate: inBinaryStdout("see /home/zed9/x"), planted: "/home/zed9/x", at: "# stdout:", rules: []string{rPath}},
		{name: "base64/path in stderr", mutate: inBinaryStderr("see /home/zed9/x"), planted: "/home/zed9/x", at: "# stderr:", rules: []string{rPath}},
		{name: "base64/lab name", mutate: inBinaryStdout("on dd-podman"), planted: "dd-podman", at: "# stdout:", rules: []string{rLab}},
		{name: "base64/token", mutate: inBinaryStdout(fakeGitHubToken), planted: fakeGitHubToken, at: "# stdout:", rules: []string{rSecretToken}},
		{name: "base64/address", mutate: inBinaryStderr("to 192.168.1.10"), planted: "192.168.1.10", at: "# stderr:", rules: []string{rAddress}},
		{name: "base64/local identity", mutate: inBinaryStdout("host zedbox"), planted: "zedbox", at: "# stdout:", rules: []string{rLocal}, local: []string{"zedbox"}},
		{name: "base64/sensitive variable", mutate: inBinaryStdout("API_KEY=zzk9"), planted: "zzk9", at: "# stdout:", rules: []string{rEnv}},
		{name: "base64/invisible character", mutate: inBinaryStdout("a\u202eb"), planted: "a\u202eb", at: "# stdout:", rules: []string{rInvisible}},

		// A pending file has an argv and headers, and nothing else: they are checked.
		{name: "pending/argv", mutate: asPending(inArgv("run", "-v", "/home/zed9/x:/x", "alpine")), planted: "/home/zed9/x:/x", rules: []string{rPath}},
		{name: "pending/header", mutate: asPending(inScenario("on dd-podman")), planted: "dd-podman", rules: []string{rLab}},
		{name: "pending/env in the argv", mutate: asPending(inArgv("run", "-e", "MY_TOKEN=zzv9", "alpine")), planted: "zzv9", rules: []string{rEnv}},

		// invisible-character: the text encoding lets these through (they are not
		// C0 or C1 controls), and a reviewer cannot see them.
		{name: "invisible/right-to-left override in stdout", mutate: inStdout("zzmark\u202e"), planted: "zzmark", rules: []string{rInvisible}},
		{name: "invisible/zero-width space in stderr", mutate: inStderr("zzmark\u200b"), planted: "zzmark", rules: []string{rInvisible}},
		{name: "invisible/isolate in an argv", mutate: inArgv("run", "zzmark\u2066", "alpine"), planted: "zzmark", rules: []string{rInvisible}},
		{name: "invisible/byte order mark in a header", mutate: inScenario("zzmark\ufeff"), planted: "zzmark", rules: []string{rInvisible}},
		{name: "invisible/in a pending file", mutate: asPending(inScenario("zzmark\u202e")), planted: "zzmark", rules: []string{rInvisible}},
		{name: "invisible/splits a lab name", mutate: inStdout("zzmark d\u200bd-podman"), planted: "zzmark", rules: []string{rInvisible}, maybe: []string{rLab}},
	}
}

func TestCheckLeaksEachRuleCatchesItsLeak(t *testing.T) {
	base := render(t, leakBase())
	if leaks := enginefixture.CheckLeaksAs("base--docker", base, nil, nil); len(leaks) != 0 {
		t.Fatalf("the unmodified base is not clean (%v): every case below would be meaningless", leaks)
	}
	expected := map[string]bool{}
	for _, tc := range leakCases() {
		for _, r := range tc.rules {
			expected[r] = true
		}
		t.Run(tc.name, func(t *testing.T) { assertPlanted(t, tc) })
	}
	for _, rule := range []string{rSecretToken, rJWT, rHost, rAddress, rInternal, rPath, rLab, rLocal, rExact, rEnv, rInvisible} {
		if !expected[rule] {
			t.Errorf("no case expects the rule %q: every rule needs a positive case", rule)
		}
	}
}

// cleanCase is a text that merely looks like a leak, and the rule it is the
// negative case of.
type cleanCase struct {
	name  string
	rule  string
	m     mutation
	exact []string
	local []string
}

func cleanCases() []cleanCase {
	in := func(rule, name, text string) cleanCase {
		return cleanCase{name: rule + "/" + name, rule: rule, m: inStdout(text)}
	}
	return []cleanCase{
		// personal-path
		in(rPath, "the replacement home", "/home/fixture-user/x"),
		in(rPath, "an image path that holds /home/", "team/home/api"),
		in(rPath, "an image path that holds /root/", "docker.io/team/root/api"),
		in(rPath, "a request target that holds /home/", "/v2/team/home/api/tags/list"),
		in(rPath, "a directory that ends in home", "/usr/home/x"),
		in(rPath, "a directory that starts with home", "/homework/x"),
		in(rPath, "/rootfs is not /root", "/rootfs/x"),
		in(rPath, "the run directory of a user", "/run/user/2000"),
		in(rPath, "rootless", "rootless"),
		in(rPath, "a Windows system path", `C:\\Windows\\System32`),
		in(rPath, "the macOS Users of an image path", "team/Users/api"),

		// lab-identity: the word, not the letters.
		in(rLab, "a container name that starts with dd", "ddm-run"),
		in(rLab, "an identifier that holds dd", "3f9d2c7add41be5e8f0a"),
		in(rLab, "add", "add and address"),
		in(rLab, "dd followed by a digit", "dd5"),
		in(rLab, "dd followed by an underscore", "dd_backup"),
		in(rLab, "jakku with a tail", "jakkus"),

		// internal-name
		in(rInternal, "a name that starts with lan", "lan.example.com"),
		in(rInternal, "an image reference", "docker.io/library/alpine:3.20"),
		in(rInternal, "a word that ends like a zone", "local-name"),
		in(rInternal, "localhost", "localhost"),

		// private-or-link-local-address: the default networks of the engines.
		in(rAddress, "Docker bridge", "172.17.0.2"),
		in(rAddress, "Docker, last pool", "172.31.255.254"),
		in(rAddress, "Docker, a middle pool", "172.24.0.3"),
		in(rAddress, "Podman default network", "10.88.0.5"),
		in(rAddress, "Podman second network", "10.89.255.254"),
		in(rAddress, "published on every interface", "0.0.0.0:18080->80/tcp"),
		in(rAddress, "loopback", "127.0.0.1:6379->6379/tcp"),
		// A public address is no longer clean: the public-address rule (OC-09a3) owns it, see leaks_public_test.go.
		in(rAddress, "a documentation address", "203.0.113.7"),
		in(rAddress, "a digest that starts like an IPv6 prefix", "sha256:fd00000000000000000000000000000000000000000000000000000000000001"),

		// host-not-allowed
		in(rHost, "Docker Hub index", "https://index.docker.io/v1/"),
		in(rHost, "Docker Hub registry", "https://registry-1.docker.io/v2/"),
		in(rHost, "docker.io", "https://docker.io/library/alpine"),
		in(rHost, "Quay", "https://quay.io/repository/x"),
		in(rHost, "loopback with a port", "http://127.0.0.1:5000/v2/"),
		in(rHost, "localhost", "http://localhost:5000/v2/"),
		in(rHost, "upper case", "HTTPS://INDEX.DOCKER.IO/v1/"),
		in(rHost, "an image reference is not a URL", "docker.io/library/alpine:3.20"),

		// local-identity: the word, and only when the test hands the identity over.
		{name: rLocal + "/not handed over", rule: rLocal, m: inStdout("Hostname: zedbox")},
		{name: rLocal + "/a longer word", rule: rLocal, m: inStdout("zedboxes"), local: []string{"zedbox"}},
		{name: rLocal + "/glued to a word", rule: rLocal, m: inStdout("myzedbox"), local: []string{"zedbox"}},
		{name: rLocal + "/followed by an underscore", rule: rLocal, m: inStdout("zedbox_1"), local: []string{"zedbox"}},
		{name: rLocal + "/the replacement names", rule: rLocal, m: inStdout("fixture-host fixture-user"), local: []string{"zedbox"}},

		// exact-term
		{name: rExact + "/an absent term", rule: rExact, m: inStdout("nothing"), exact: []string{"zz-denied-term"}},
		{name: rExact + "/an empty term matches nothing", rule: rExact, m: inStdout("nothing"), exact: []string{""}},
		{name: rExact + "/glued before", rule: rExact, m: inStdout("xzz-denied-term"), exact: []string{"zz-denied-term"}},
		{name: rExact + "/glued after", rule: rExact, m: inStdout("zz-denied-termx"), exact: []string{"zz-denied-term"}},
		{name: rExact + "/not handed over", rule: rExact, m: inStdout("zz-denied-term")},

		// sensitive-env-value
		in(rEnv, "redacted", "SECRETENV=<redacted>"),
		in(rEnv, "a harmless variable", "PATH=/usr/bin"),
		in(rEnv, "the replacement home", "HOME=/home/fixture-user"),
		in(rEnv, "an empty value", "MY_TOKEN="),
		in(rEnv, "a name alone", "-e MY_TOKEN"),
		{name: rEnv + "/a name alone in an argv", rule: rEnv, m: inArgv("run", "-e", "MY_TOKEN", "alpine")},
		{name: rEnv + "/redacted in an argv", rule: rEnv, m: inArgv("run", "-e", "MY_TOKEN=<redacted>", "alpine")},

		// secret-token-format and jwt-like-value
		in(rSecretToken, "prefixes that are too short", "ghp_short glpat-x AKIA1234"),
		in(rJWT, "the prefix with a short tail", "eyJshort"),

		// invisible-character: text that is not ASCII is not a leak.
		in(rInvisible, "accented letters", "café naïve"),
		in(rInvisible, "CJK", "日本語"),
		in(rInvisible, "arrows and box drawing", "→ ├──┤ ✓"),
		in(rInvisible, "an emoji", "🙂"),
		in(rInvisible, "a tab", "a\tb"),
	}
}

func TestCheckLeaksAcceptsWhatOnlyLooksLikeALeak(t *testing.T) {
	for _, tc := range cleanCases() {
		t.Run(tc.name, func(t *testing.T) {
			f := leakBase()
			tc.m(&f)
			data := render(t, f)
			if leaks := enginefixture.CheckLeaksAs("clean--docker", data, tc.exact, tc.local); len(leaks) != 0 {
				t.Errorf("CheckLeaksAs = %v, want none for:\n%s", leaks, data)
			}
		})
	}
	// Every rule has its negative cases.
	seen := map[string]bool{}
	for _, tc := range cleanCases() {
		seen[tc.rule] = true
	}
	for _, rule := range []string{rSecretToken, rJWT, rHost, rAddress, rInternal, rPath, rLab, rLocal, rExact, rEnv, rInvisible} {
		if !seen[rule] {
			t.Errorf("no negative case for the rule %q", rule)
		}
	}
}

func TestCheckLeaksAcceptsTheWholeSyntheticSet(t *testing.T) {
	for _, name := range syntheticNames(t) {
		t.Run(name, func(t *testing.T) {
			data := syntheticFile(t, name)
			if leaks := enginefixture.CheckLeaks(name, data, nil); len(leaks) != 0 {
				t.Errorf("CheckLeaks = %v, want none", leaks)
			}
		})
	}
}

func TestACleanPendingFileIsClean(t *testing.T) {
	f := leakBase()
	asPending(inArgv("run", "-d", "--name", "ddm-run2", "alpine:3.20", "sleep", "5"))(&f)
	data := render(t, f)
	if leaks := enginefixture.CheckLeaks("pending--docker", data, nil); len(leaks) != 0 {
		t.Errorf("CheckLeaks = %v, want none", leaks)
	}
}

// A user home that an image keeps (for example /home/node of the node image) is a
// known false positive: it is listed in ImageHomes with its reason, never allowed
// by default.
func TestImageHomesAreListedWithTheirReason(t *testing.T) {
	for name, reason := range enginefixture.ImageHomes {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("ImageHomes[%q] has no reason: each entry needs the image that keeps the home", name)
		}
	}

	old := enginefixture.ImageHomes
	t.Cleanup(func() { enginefixture.ImageHomes = old })

	enginefixture.ImageHomes = map[string]string{}
	node := plantInto(t, inStdout("/home/node/app"))
	if leaks := enginefixture.CheckLeaks("node--docker", node, nil); len(leaks) == 0 {
		t.Error("/home/node is reported by no rule while ImageHomes is empty")
	}

	enginefixture.ImageHomes = map[string]string{"node": "the node image runs as the user node"}
	if leaks := enginefixture.CheckLeaks("node--docker", node, nil); len(leaks) != 0 {
		t.Errorf("a listed image home is still reported: %v", leaks)
	}
	other := plantInto(t, inStdout("/home/zed9/app"))
	if leaks := enginefixture.CheckLeaks("other--docker", other, nil); len(leaks) == 0 {
		t.Error("listing one image home let another one through")
	}
}

// Rules of the file, not of the text.

func TestCheckLeaksReportsEachRuleOncePerLine(t *testing.T) {
	data := plantInto(t, inStdout("/home/zed9/a and /home/zed8/b"))
	leaks := enginefixture.CheckLeaks("twice--docker", data, nil)
	if len(leaks) != 1 || leaks[0].Rule != rPath {
		t.Errorf("CheckLeaks = %v, want exactly one %s leak for a line that holds two personal paths", leaks, rPath)
	}
}

func TestCheckLeaksReportsEveryLeakInLineOrder(t *testing.T) {
	data := plantInto(t, func(f *enginefixture.Fixture) {
		inScenario("on dd-podman")(f)
		inStdout("see 192.168.1.10")(f)
		inStderr("/home/zed9/x")(f)
	})
	leaks := enginefixture.CheckLeaks("many--docker", data, nil)
	var lines []int
	for _, l := range leaks {
		lines = append(lines, l.Line)
	}
	want := []int{
		leakLineOf(string(data), "dd-podman"),
		leakLineOf(string(data), "192.168.1.10"),
		leakLineOf(string(data), "/home/zed9/x"),
	}
	if !slices.Equal(lines, want) {
		t.Errorf("leak lines = %v, want %v (one per planted line, ascending)", lines, want)
	}
}

func TestLeakIsNeverTheMatchedText(t *testing.T) {
	var fields []string
	for f := range reflect.TypeFor[enginefixture.Leak]().Fields() {
		fields = append(fields, f.Name)
	}
	slices.Sort(fields)
	if want := []string{"Fixture", "Line", "Rule"}; !slices.Equal(fields, want) {
		t.Errorf("Leak fields = %v, want exactly %v: a field for the matched text would put the secret in a test log", fields, want)
	}
}

func TestCheckLeaksIsCheckLeaksAsWithoutLocalIdentities(t *testing.T) {
	data := plantInto(t, inStdout("see /home/zed9/x and zz-denied-term"))
	exact := []string{"zz-denied-term"}
	a := enginefixture.CheckLeaks("same--docker", data, exact)
	b := enginefixture.CheckLeaksAs("same--docker", data, exact, nil)
	if len(a) == 0 || !reflect.DeepEqual(a, b) {
		t.Errorf("CheckLeaks = %v, CheckLeaksAs(…, nil) = %v, want the same non-empty result", a, b)
	}
}

// When the file is not valid, the raw text is examined: a broken file is not a
// way round the check. (A leak inside a base64 stream cannot be seen then, which
// is why a broken file never passes Parse, Load or Replay.)
func TestAnInvalidFileIsScannedAsRawText(t *testing.T) {
	valid := plantInto(t, inStdout("see /home/zed9/x"))
	declared := regexp.MustCompile(`# stdout: \d+ bytes`)
	tests := []struct {
		name string
		data []byte
		rule string
		line int
	}{
		{"a wrong declared length", declared.ReplaceAll(valid, []byte("# stdout: 999999 bytes")), rPath, leakLineOf(string(valid), "/home/zed9/x")},
		{"a carriage return", bytes.Replace(valid, []byte("see"), []byte("see\r"), 1), rPath, leakLineOf(string(valid), "/home/zed9/x")},
		{"not a fixture at all", []byte("hello\n/home/zed9/x\n"), rPath, 2},
		{"a lab name in a file without headers", []byte("dd-podman\n"), rLab, 1},
		{"a file cut after the headers", valid[:bytes.Index(valid, []byte("--- stdout"))], rPath, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := enginefixture.Parse("leak", leakBaseFile, tt.data); err == nil {
				t.Fatal("the file is valid: the case does not test the raw-text fallback")
			}
			leaks := enginefixture.CheckLeaks("broken--docker", tt.data, nil)
			if tt.line == 0 {
				return // the leak sat after the cut: only "no panic" is asked
			}
			found := false
			for _, l := range leaks {
				found = found || (l.Rule == tt.rule && l.Line == tt.line)
			}
			if !found {
				t.Errorf("no %s leak at line %d, got %v", tt.rule, tt.line, leaks)
			}
		})
	}
	for _, data := range [][]byte{nil, {}, []byte("\n"), []byte("\x00\xff")} {
		if leaks := enginefixture.CheckLeaks("empty--docker", data, nil); len(leaks) != 0 {
			t.Errorf("CheckLeaks(%q) = %v, want none", data, leaks)
		}
	}
}

// Every one of these characters is valid text and invisible to a reviewer; the
// text encoding of a stream accepts them (they are not C0 or C1 controls).
func TestEveryBidirectionalAndZeroWidthCharacterIsReported(t *testing.T) {
	var runes []rune
	for r := '\u200b'; r <= '\u200f'; r++ { // zero-width space, joiners, direction marks
		runes = append(runes, r)
	}
	for r := '\u202a'; r <= '\u202e'; r++ { // embeddings, pop, overrides
		runes = append(runes, r)
	}
	for r := '\u2066'; r <= '\u2069'; r++ { // isolates
		runes = append(runes, r)
	}
	runes = append(runes, '\u2060', '\ufeff') // word joiner, byte order mark
	for _, r := range runes {
		t.Run(fmt.Sprintf("U+%04X", r), func(t *testing.T) {
			data := plantInto(t, inStdout("zzmark"+string(r)))
			line := leakLineOf(string(data), "zzmark")
			for _, l := range enginefixture.CheckLeaks("invisible--docker", data, nil) {
				if l.Rule == rInvisible && l.Line == line {
					return
				}
			}
			t.Errorf("no %s leak at line %d", rInvisible, line)
		})
	}
}

// The constants of the check.

func TestThePlaceholdersAreTheOnesTheRecorderWillWrite(t *testing.T) {
	if enginefixture.PlaceholderUser != "fixture-user" || enginefixture.PlaceholderHost != "fixture-host" ||
		enginefixture.PlaceholderHome != "/home/fixture-user" {
		t.Errorf("placeholders = %q, %q, %q", enginefixture.PlaceholderUser, enginefixture.PlaceholderHost, enginefixture.PlaceholderHome)
	}
	data := plantInto(t, inStdout(enginefixture.PlaceholderUser+" "+enginefixture.PlaceholderHost+" "+enginefixture.PlaceholderHome+"/x"))
	if leaks := enginefixture.CheckLeaks("placeholders--docker", data, nil); len(leaks) != 0 {
		t.Errorf("the placeholders are reported: %v", leaks)
	}
}

func TestLabTermsAreTheNamesAlreadyPublic(t *testing.T) {
	// tools/engine-vms/README.md names the lab; these are the only names the
	// check treats as the lab's.
	want := []string{"dd", "dd-docker", "dd-podman", "jakku"}
	if got := slices.Sorted(slices.Values(enginefixture.LabTerms)); !slices.Equal(got, want) {
		t.Errorf("LabTerms = %v, want %v", got, want)
	}
}

func TestAllowedHostsAreLoopbackAndTheImageRegistries(t *testing.T) {
	want := []string{"docker.io", "index.docker.io", "localhost", "quay.io", "registry-1.docker.io", "127.0.0.1"}
	slices.Sort(want)
	if got := slices.Sorted(slices.Values(enginefixture.AllowedHosts)); !slices.Equal(got, want) {
		t.Errorf("AllowedHosts = %v, want %v", got, want)
	}
}

func TestEngineBridgeNetworksAreTheDefaultOnesOnly(t *testing.T) {
	var got []string
	for _, p := range enginefixture.EngineBridgeNets {
		got = append(got, p.String())
	}
	var want []string
	for second := 17; second <= 31; second++ { // the address pools of Docker
		want = append(want, fmt.Sprintf("172.%d.0.0/16", second))
	}
	want = append(want, "10.88.0.0/16", "10.89.0.0/16") // the networks of Podman
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("EngineBridgeNets = %v, want %v", got, want)
	}
}

// The identities of the machine that runs the test.

func TestLocalIdentityTermsKeepsOnlyWhatIsWorthChecking(t *testing.T) {
	tests := []struct {
		name  string
		names []string
		want  []string
	}{
		{"ordinary names", []string{"zedbox", "zeduser"}, []string{"zedbox", "zeduser"}},
		{"the stop list", []string{"root", "user", "admin", "runner", "ubuntu", "docker", "podman", "ci", "build", "test"}, nil},
		{"the stop list ignores the case", []string{"Runner", "ROOT", "Ubuntu"}, nil},
		{"too short", []string{"", "a", "ab"}, nil},
		{"three characters are enough", []string{"abc"}, []string{"abc"}},
		{"the replacement names", []string{"fixture-user", "fixture-host", "fixture-anything"}, nil},
		{"blanks around a name", []string{"  zedbox\n"}, []string{"zedbox"}},
		{"a mix keeps its order", []string{"root", "zedbox", "ab", "zeduser"}, []string{"zedbox", "zeduser"}},
		{"nothing", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := enginefixture.LocalIdentityTerms(tt.names...); !slices.Equal(got, tt.want) {
				t.Errorf("LocalIdentityTerms(%q) = %q, want %q", tt.names, got, tt.want)
			}
		})
	}
}

// MachineIdentities reads the host name and the user name of the machine; the
// test cannot name them without writing them, so it checks what they must be:
// taken from the places the plan names, filtered like any other identity.
func TestMachineIdentitiesComeFromTheMachineAndAreFiltered(t *testing.T) {
	var sources []string
	if h, err := os.Hostname(); err == nil {
		sources = append(sources, h)
	}
	if u, err := user.Current(); err == nil {
		sources = append(sources, u.Username)
	}
	sources = append(sources, os.Getenv("USER"))
	allowed := enginefixture.LocalIdentityTerms(sources...)

	got := enginefixture.MachineIdentities()
	for i, term := range got {
		if !slices.ContainsFunc(allowed, func(a string) bool { return strings.EqualFold(a, term) }) {
			t.Errorf("identity #%d is not the host name, the user name or $USER, filtered", i) // never the term itself
		}
	}
	if len(allowed) > 0 && len(got) == 0 {
		t.Error("MachineIdentities is empty although the machine has a host name or a user name worth checking")
	}
	if !maps.Equal(setOf(got), setOf(enginefixture.LocalIdentityTerms(got...))) {
		t.Error("MachineIdentities returned a term that LocalIdentityTerms would drop")
	}
}

func setOf(terms []string) map[string]bool {
	m := map[string]bool{}
	for _, t := range terms {
		m[strings.ToLower(t)] = true
	}
	return m
}
